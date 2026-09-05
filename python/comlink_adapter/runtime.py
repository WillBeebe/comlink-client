import asyncio
from dataclasses import dataclass, field
import inspect
import re
from .client import ComlinkError, Event, TERMINAL

@dataclass(frozen=True)
class Action:
    """A model may propose only an action on the current circuit."""
    name: str
    text: str = ''

@dataclass
class _Call:
    queue: asyncio.Queue = field(default_factory=lambda: asyncio.Queue(maxsize=8))
    history: list = field(default_factory=list)
    task: object = None
    timer: object = None

class Agent:
    """Bounded event-to-async-callback adapter. Owner supplies exact peer allowlist.

    handler(event, context) returns up to four Actions. The callback must cooperate
    with cancellation, not log/store text, and enforce its own provider budget.
    """
    def __init__(self, link, handler, *, allowed_peers, max_turns=16,
                 max_seconds=300, max_context_bytes=32768, model_concurrency=4):
        if not inspect.iscoroutinefunction(handler):
            raise ValueError('handler must be an async function')
        peers = frozenset(allowed_peers)
        if len(peers)>128 or any(not re.fullmatch('[0-9a-z]{128}', p) for p in peers):
            raise ValueError('exact peer numbers required')
        if not 1<=max_turns<=16 or not 0<max_seconds<=300 or not 1<=max_context_bytes<=65536 or not 1<=model_concurrency<=16:
            raise ValueError('invalid runtime limits')
        self.link, self.handler, self.allowed_peers = link, handler, peers
        self.max_turns, self.max_seconds, self.max_context_bytes = max_turns, max_seconds, max_context_bytes
        self.semaphore = asyncio.Semaphore(model_concurrency)
        self.calls, self.ended, self.tasks = {}, set(), set()
        self.total_calls = 0
        self.dialing = set()
        self.running = False
        self.failure = None

    def _terminal(self, event):
        keys = list(self.calls) if event.type == 'disconnected' else [event.call]
        for call in keys:
            self.ended.add(call)
            state = self.calls.pop(call, None)
            if state:
                state.timer.cancel()
                state.history.clear()
                while not state.queue.empty():
                    state.queue.get_nowait()
                if state.task is not asyncio.current_task():
                    state.task.cancel()
        if len(self.ended)>128:
            self.link._stop()

    def _open(self, call):
        if call in self.ended:
            return None
        if call not in self.calls:
            if len(self.calls)>=8 or self.total_calls>=100:
                raise ComlinkError('runtime call capacity exceeded')
            self.total_calls += 1
            state = self.calls[call] = _Call()
            state.task = asyncio.create_task(self._work(call, state))
            self.tasks.add(state.task)
            state.task.add_done_callback(self.tasks.discard)
            state.timer = asyncio.get_running_loop().call_later(self.max_seconds, self._expire, call)
        return self.calls[call]

    def _expire(self, call):
        self._terminal(Event('closed', call))
        # Fail closed: disconnect the handset, rather than leave a live unattended call.
        self.link._stop()

    async def dial(self, number):
        if number not in self.allowed_peers:
            raise ComlinkError('peer not authorized locally')
        self.dialing.add(number)
        try:
            result = await self.link.tool('dial', to=number)
        finally:
            self.dialing.discard(number)
        call = result.get('call')
        if not isinstance(call,str) or not re.fullmatch('[0-9a-f]{64}',call):
            raise ComlinkError('invalid call result')
        self._open(call)
        return call

    async def _work(self, call, state):
        try:
            for _ in range(self.max_turns):
                event = await state.queue.get()
                state.history.append(event)
                if sum(len(e.text.encode()) for e in state.history)>self.max_context_bytes:
                    raise ComlinkError('call context limit exceeded')
                async with self.semaphore:
                    actions = await self.handler(event, tuple(state.history))
                if self.calls.get(call) is not state:
                    return
                if not isinstance(actions, (list,tuple)) or len(actions)>4:
                    raise ComlinkError('handler must return at most four Actions')
                for action in actions:
                    if not isinstance(action,Action) or action.name not in {'answer','reject','say','hangup'}:
                        raise ComlinkError('unsupported model action')
                    if action.name in {'answer','reject'} and event.type!='ring':
                        raise ComlinkError('answer/reject requires an incoming ring')
                    if action.name!='say' and action.text:
                        raise ComlinkError('text is only valid for say')
                    if len(action.text.encode())>16384:
                        raise ComlinkError('outgoing text limit exceeded')
                    if self.calls.get(call) is not state:
                        return
                    args={'call':call}
                    if action.name=='say':
                        args['text']=action.text
                    result = await self.link.tool(action.name, **args)
                    if result.get('sent' if action.name=='say' else 'ok') is not True:
                        raise ComlinkError('action did not succeed')
                    if action.name=='say':
                        state.history.append(Event('sent',call,self.link.number or '',action.text))
                        if sum(len(e.text.encode()) for e in state.history)>self.max_context_bytes:
                            raise ComlinkError('call context limit exceeded')
                    if action.name in {'reject','hangup'}:
                        self._terminal(Event(action.name,call))
                        return
            self._expire(call)
        except asyncio.CancelledError:
            raise
        except Exception:
            # Do not log model exceptions: they can contain decrypted input.
            self.failure = ComlinkError('agent callback/action failed; connection closed')
            self.link._stop()
        finally:
            state.history.clear()
            while not state.queue.empty():
                state.queue.get_nowait()

    async def run(self):
        if self.running or self.link.on_terminal:
            raise ComlinkError('one event consumer is allowed per handset')
        self.running = True
        self.link.on_terminal = self._terminal
        try:
            if not self.link.number:
                await self.link.register()
            while True:
                event = await self.link.events.get()
                if event.type=='disconnected':
                    if self.failure:
                        raise self.failure
                    return
                if event.type in TERMINAL or event.call in self.ended:
                    continue
                if event.type=='ring' and event.sender not in self.allowed_peers:
                    await self.link.tool('reject',call=event.call)
                    self._terminal(Event('reject',event.call))
                    continue
                if event.type=='ring' or (event.type=='answer' and event.sender in self.dialing):
                    self._open(event.call)
                state = self.calls.get(event.call)
                if state is None:
                    raise ComlinkError('event for unknown circuit')
                state.queue.put_nowait(event)
        finally:
            self._terminal(Event('disconnected'))
            await self.link.close()
            if self.tasks:
                await asyncio.gather(*self.tasks,return_exceptions=True)
            self.link.on_terminal = None
            self.running = False
