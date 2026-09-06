"""Actual pinned Hermes, synthetic provider only. No real key, no paid inference."""
import argparse
import asyncio
import json
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
import subprocess
import sys
import tempfile
import threading
import time
from unittest.mock import patch
from comlink_adapter import Event, Action, Agent, ComlinkError
from test_adapter import FakeLink
from comlink_adapter.hermes import HermesBridge

MARKER = 'synthetic-private-speech-7b593a80'
class Provider(BaseHTTPRequestHandler):
    mode = 'success'
    requests = 0
    entered = threading.Event()
    release = threading.Event()
    def log_message(self, *_): pass
    def do_POST(self):
        cls = type(self)
        cls.requests += 1
        body = json.loads(self.rfile.read(int(self.headers['Content-Length'])))
        assert self.path == '/v1/chat/completions'
        assert not body.get('tools') and not body.get('stream')
        assert body['max_tokens'] == 512 and body['model'] == 'fixture-model'
        cls.entered.set()
        if cls.mode == 'delay': cls.release.wait(30)
        if cls.mode == 'error':
            status, response = 500, {'error': {'message': MARKER, 'type': 'test'}}
        else:
            packet = json.loads(body['messages'][-1]['content'])
            event = packet['events'][-1]
            if event['type'] == 'answer': actions = [{'name':'say','text':'ping'}]
            elif event['text'] == 'ping': actions = [{'name':'say','text':'pong'}]
            elif event['text'] == 'pong': actions = [{'name':'hangup'}]
            else: actions = [{'name':'say','text':'synthetic reply'}]
            status, response = 200, {'id':'fixture','object':'chat.completion','created':1,
                'model':'fixture-model', 'choices':[{'index':0,'message':{'role':'assistant','content':json.dumps(actions)},'finish_reason':'stop'}],
                'usage':{'prompt_tokens':20,'completion_tokens':10,'total_tokens':30}}
        raw = json.dumps(response).encode()
        try:
            self.send_response(status); self.send_header('Content-Type','application/json')
            self.send_header('Content-Length',str(len(raw))); self.end_headers(); self.wfile.write(raw)
        except (BrokenPipeError, ConnectionResetError): pass

async def exercise(config):
    processes, homes = [], []
    original = asyncio.create_subprocess_exec
    async def spawn(*args, **kwargs):
        homes.append(Path(kwargs['env']['HERMES_HOME']))
        proc = await original(*args, **kwargs)
        processes.append(proc)
        return proc
    original_temp = tempfile.TemporaryDirectory
    class InspectedTemp(original_temp):
        def __exit__(self, *args):
            for file in Path(self.name).rglob('*'):
                if file.is_file(): assert MARKER.encode() not in file.read_bytes(), 'speech persisted'
            return super().__exit__(*args)
    event = Event('say','b'*64,'a'*128,MARKER)
    with patch('comlink_adapter.hermes.asyncio.create_subprocess_exec', spawn), patch('comlink_adapter.hermes.tempfile.TemporaryDirectory', InspectedTemp):
        bridge = HermesBridge(config)
        before = Provider.requests
        assert await bridge.handle(event,(event,)) == [Action('say','synthetic reply')]
        assert Provider.requests == before + 1
        Provider.mode = 'error'
        before = Provider.requests
        try: await bridge.handle(event,(event,))
        except ComlinkError as exc: assert MARKER not in str(exc)
        else: raise AssertionError('provider failure accepted')
        assert Provider.requests == before + 1, 'retry escaped request cap'
        Provider.mode = 'delay'; Provider.entered.clear()
        link = FakeLink()
        agent = Agent(link, bridge.handle, allowed_peers=config['allowed_peers'])
        task = asyncio.create_task(agent.run())
        await asyncio.sleep(0)
        link.event(Event('ring', event.call, event.sender))
        link.event(event)
        assert await asyncio.to_thread(Provider.entered.wait, 30), 'worker did not reach provider'
        started = time.monotonic()
        link.event(Event('hangup', event.call, event.sender))
        workers = list(agent.tasks)
        await asyncio.wait_for(asyncio.gather(*workers, return_exceptions=True), 3)
        assert not agent.calls and [name for name, _ in link.sent] == ['answer']
        link._stop()
        await task
        assert time.monotonic() - started < 3
        assert processes[-1].returncode is not None, 'worker survived cancellation'
        Provider.release.set(); Provider.mode = 'success'
        assert bridge.remaining == config['max_requests'] - 3
    assert all(p.returncode is not None for p in processes)
    assert not any(p.exists() for p in homes)
    print('PASS: actual Hermes reply, one-request failure, worker cancellation, and no speech in scratch files.')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--checkout', required=True)
    parser.add_argument('--python', required=True, help='Python from the pinned Hermes venv')
    for name in ('server','binary','switch'): parser.add_argument('--'+name, help='optional: run bidirectional encrypted Elixir circuit (supply all three)')
    args = parser.parse_args()
    if any((args.server,args.binary,args.switch)) and not all((args.server,args.binary,args.switch)):
        parser.error('server, binary and switch must be supplied together')
    server = ThreadingHTTPServer(('127.0.0.1',0),Provider)
    threading.Thread(target=server.serve_forever,daemon=True).start()
    try:
        with tempfile.TemporaryDirectory(prefix='comlink-hermes-test-') as tmp:
            key = Path(tmp)/'key'; key.write_text('synthetic-test-key'); key.chmod(0o600)
            cfg = dict(checkout=args.checkout, python=args.python,
                base_url=f'http://127.0.0.1:{server.server_port}/v1', model='fixture-model',
                api_key_file=str(key), allowed_peers=['a'*128], max_requests=8, turn_seconds=60)
            asyncio.run(exercise(cfg))
            if args.server:
                config = Path(tmp)/'config.json'; config.write_text(json.dumps(cfg))
                before = Provider.requests
                subprocess.run([sys.executable, str(Path(__file__).with_name('local_exchange_smoke.py')),
                    '--server', args.server, '--binary', args.binary, '--switch', args.switch,
                    '--hermes-config',str(config)], check=True, timeout=180)
                assert Provider.requests == before + 6, 'expected three Hermes turns per circuit, both directions'
                print('PASS: two real Hermes runtimes, six synthetic model turns, bidirectional encrypted Elixir calls.')
    finally:
        Provider.release.set(); server.shutdown(); server.server_close()

if __name__ == '__main__': main()
