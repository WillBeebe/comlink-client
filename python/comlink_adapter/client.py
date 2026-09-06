import asyncio
from dataclasses import dataclass
import json
from pathlib import Path
import re

TOOLS = frozenset({'register', 'whois', 'dial', 'answer', 'reject', 'say', 'hangup'})
# Recognize optional owner-managed address-book tools without granting callbacks access.
CONTACT_TOOLS = frozenset({'my_number', 'contacts_list', 'contacts_save', 'contacts_remove'})

def validate_tool_list(listed):
    names = [t['name'] for t in listed['tools']]
    if len(names) != len(set(names)) or not TOOLS <= set(names) <= TOOLS | CONTACT_TOOLS:
        raise ComlinkError('unexpected handset tools')

TERMINAL = frozenset({'reject', 'hangup', 'closed', 'disconnected'})

class ComlinkError(Exception):
    """Payload-free local transport or MCP failure."""

@dataclass(frozen=True)
class Event:
    type: str
    call: str = ''
    sender: str = ''
    text: str = ''

class Comlink:
    """One handset process and independent reader; never automatically reconnects.

    Entering the async context negotiates MCP but does not enroll. Call register
    explicitly, or run Agent, to register/reuse the configured private profile.
    """
    def __init__(self, *, binary=None, state=None, endpoint='https://api.comlink.fyi/mcp',
                 roots=None, ca=None, timeout=30):
        self.binary = str(binary or Path.home()/'.local/bin/comlink')
        self.state = Path(state or Path.home()/'.local/share/comlink').absolute()
        self.endpoint, self.roots, self.ca = endpoint, str(roots or self.state/'roots.json'), ca
        if not 0 < timeout <= 60:
            raise ValueError('timeout must be between 0 and 60 seconds')
        self.timeout = timeout
        self.process = self.reader = None
        self.pending = {}
        self.abandoned = set()
        self.sequence = self.event_sequence = 0
        self.events = asyncio.Queue(maxsize=64)
        self.lock = asyncio.Lock()
        self.closed = False
        self.number = None
        self.on_terminal = None

    async def __aenter__(self):
        if self.process is not None or self.closed:
            raise ComlinkError('create a fresh adapter for a new connection')
        args = [self.binary, 'mcp', '--enroll', '--file', str(self.state/'handset.json'),
                '--roots', self.roots, '--endpoint', self.endpoint]
        if self.ca:
            args += ['--ca', str(self.ca)]
        self.process = await asyncio.create_subprocess_exec(*args, stdin=asyncio.subprocess.PIPE,
            stdout=asyncio.subprocess.PIPE, stderr=asyncio.subprocess.DEVNULL, limit=262144)
        self.reader = asyncio.create_task(self._read())
        try:
            result = await self.request('initialize', {'protocolVersion': '2025-06-18',
                'capabilities': {'experimental': {'comlink.fyi/events': {'version': 1}}},
                'clientInfo': {'name': 'comlink-python-adapter', 'version': '0.1.0b1'}})
            if result.get('capabilities', {}).get('experimental', {}).get('comlink.fyi/events', {}).get('version') != 1:
                raise ComlinkError('handset lacks live-event support')
            await self._send({'jsonrpc': '2.0', 'method': 'notifications/initialized'})
            listed = await self.request('tools/list', {})
            validate_tool_list(listed)
            return self
        except BaseException:
            await self.close()
            raise

    async def __aexit__(self, *_):
        await self.close()

    async def _send(self, message):
        raw = json.dumps(message, ensure_ascii=True).encode() + b'\n'
        if len(raw) > 262144:
            raise ComlinkError('request too large')
        async with self.lock:
            if self.closed:
                raise ComlinkError('handset disconnected')
            self.process.stdin.write(raw)
            await asyncio.wait_for(self.process.stdin.drain(), self.timeout)

    async def request(self, method, params):
        if self.closed or len(self.pending) >= 32:
            raise ComlinkError('adapter unavailable or busy')
        self.sequence += 1
        ident = self.sequence
        future = asyncio.get_running_loop().create_future()
        self.pending[ident] = future
        try:
            await self._send({'jsonrpc': '2.0', 'id': ident, 'method': method, 'params': params})
            return await asyncio.wait_for(future, self.timeout)
        except asyncio.CancelledError:
            self.abandoned.add(ident)
            if len(self.abandoned)>64:
                self._stop()
            raise
        except (TimeoutError, OSError):
            # Acceptance is uncertain: close, never retry speech automatically.
            self._stop()
            raise ComlinkError('request failed; acceptance may be unknown') from None
        finally:
            self.pending.pop(ident, None)

    def _stop(self):
        if self.closed:
            return
        self.closed = True
        if self.on_terminal:
            self.on_terminal(Event('disconnected'))
        for future in self.pending.values():
            if not future.done():
                future.set_exception(ComlinkError('handset disconnected'))
        while not self.events.empty():
            self.events.get_nowait()
        self.events.put_nowait(Event('disconnected'))
        if self.process and self.process.returncode is None:
            try:
                self.process.terminate()
            except ProcessLookupError:
                pass

    def _event(self, params):
        if not isinstance(params, dict) or type(params.get('seq')) is not int or params.get('seq') != self.event_sequence+1 or params.get('version') != 1:
            raise ComlinkError('invalid event sequence')
        kind = params.get('type')
        if kind not in TERMINAL | {'ring', 'answer', 'say'}:
            raise ComlinkError('unknown event')
        call, sender, text = (params.get(k, '') for k in ('call', 'from', 'text'))
        if not all(isinstance(v, str) for v in (call, sender, text)) or len(text.encode()) > 65536:
            raise ComlinkError('invalid event payload')
        if kind != 'disconnected' and not re.fullmatch('[0-9a-f]{64}', call):
            raise ComlinkError('invalid call identifier')
        if kind != 'say' and text:
            raise ComlinkError('unexpected plaintext event')
        self.event_sequence += 1
        event = Event(kind, call, sender, text)
        if kind in TERMINAL and self.on_terminal:
            # Cancel model work immediately, even while a tool response is pending.
            self.on_terminal(event)
        if kind == 'disconnected':
            self._stop()
        else:
            self.events.put_nowait(event)

    async def _read(self):
        try:
            while not self.closed:
                raw = await self.process.stdout.readline()
                if not raw or len(raw) > 262144 or not raw.endswith(b'\n'):
                    break
                message = json.loads(raw)
                if not isinstance(message, dict) or message.get('jsonrpc') != '2.0':
                    break
                method = message.get('method')
                if method == 'notifications/comlink.fyi/event':
                    self._event(message.get('params'))
                elif method == 'ping' and 'id' in message:
                    await self._send({'jsonrpc': '2.0', 'id': message['id'], 'result': {}})
                elif method == 'notifications/tools/list_changed':
                    continue
                elif method:
                    break
                else:
                    if message.get('id') in self.abandoned:
                        self.abandoned.discard(message['id'])
                        continue
                    future = self.pending.get(message.get('id'))
                    if future is None:
                        break
                    if future.done():
                        continue
                    if 'error' in message:
                        future.set_exception(ComlinkError('MCP request refused'))
                    elif 'result' in message:
                        future.set_result(message['result'])
                    else:
                        break
        except (ValueError, OSError, asyncio.QueueFull, ComlinkError):
            pass
        finally:
            self._stop()

    async def tool(self, name, **arguments):
        if name not in TOOLS:
            raise ValueError('only Comlink tools are supported')
        result = await self.request('tools/call', {'name': name, 'arguments': arguments})
        if result.get('isError'):
            raise ComlinkError('Comlink tool refused')
        value = result.get('structuredContent')
        if value is None:
            try:
                value = json.loads(result['content'][0]['text'])
            except (KeyError, IndexError, TypeError, ValueError):
                raise ComlinkError('invalid tool response') from None
        return value

    async def register(self):
        result = await self.tool('register')
        number = result.get('number')
        if not isinstance(number, str) or not re.fullmatch('[0-9a-z]{128}', number):
            raise ComlinkError('invalid public number')
        self.number = number
        return number

    async def close(self):
        self._stop()
        if self.reader and self.reader is not asyncio.current_task():
            self.reader.cancel()
            await asyncio.gather(self.reader, return_exceptions=True)
        if self.process:
            try:
                await asyncio.wait_for(self.process.wait(), 5)
            except TimeoutError:
                self.process.kill()
                await self.process.wait()
        self.pending.clear()
        while not self.events.empty():
            self.events.get_nowait()
