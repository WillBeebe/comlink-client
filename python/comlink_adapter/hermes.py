"""Opt-in Hermes receiving bridge. Hermes stays an external, pinned dependency."""
import argparse
import asyncio
from dataclasses import asdict
import json
import os
from pathlib import Path
import subprocess
import tempfile
from urllib.parse import urlsplit

from . import Action, Agent, Comlink, ComlinkError

HERMES_REVISION = '245e48008fa814b3251f50755eb656bd9fb86cb1'


def validate_config(config):
    required = {'checkout', 'python', 'base_url', 'model', 'api_key_file', 'allowed_peers', 'max_requests'}
    optional = {'binary', 'state', 'endpoint', 'roots', 'ca', 'max_tokens', 'turn_seconds', 'role', 'dial'}
    if not isinstance(config, dict) or not required <= config.keys() or config.keys() - required - optional:
        raise ValueError('missing or unknown Hermes configuration fields')
    for field in ('checkout', 'python', 'api_key_file'):
        if not isinstance(config[field], str) or not Path(config[field]).is_absolute():
            raise ValueError('Hermes paths must be absolute')
    url = urlsplit(config['base_url'])
    if (url.scheme != 'https' and not (url.scheme == 'http' and url.hostname in {'127.0.0.1', '::1', 'localhost'})) or not url.hostname or url.username or url.password or url.query or url.fragment:
        raise ValueError('provider must use HTTPS or local loopback HTTP')
    for name, default, maximum in [('max_requests', 0, 100), ('max_tokens', 512, 4096), ('turn_seconds', 45, 120)]:
        value = config.get(name, default)
        if type(value) is not int or not 1 <= value <= maximum:
            raise ValueError('invalid Hermes request/token/time limit')
    if not isinstance(config['model'], str) or not config['model'] or len(config['model']) > 200:
        raise ValueError('explicit model required')
    if not isinstance(config.get('role', ''), str) or len(config.get('role', '').encode()) > 4096:
        raise ValueError('role must fit in 4096 bytes')
    if not isinstance(config['allowed_peers'], list):
        raise ValueError('exact peer allowlist required')
    # Reuse the runtime policy validation without starting a handset.
    async def unused(*_): return []
    Agent(None, unused, allowed_peers=config['allowed_peers'])
    if config.get('dial') and config['dial'] not in config['allowed_peers']:
        raise ValueError('outgoing peer is not authorized')
    checkout = Path(config['checkout'])
    def git(*args):
        return subprocess.check_output(['git', '-C', str(checkout), *args], stderr=subprocess.DEVNULL).decode().strip()
    if git('rev-parse', 'HEAD') != HERMES_REVISION or git('status', '--porcelain', '--untracked-files=no'):
        raise ValueError('Hermes requires the documented clean, pinned revision')
    key = Path(config['api_key_file'])
    if not key.is_file() or key.stat().st_mode & 0o077 or key.stat().st_size > 8192:
        raise ValueError('provider key file must be private (chmod 600), at most 8192 bytes')
    return dict(config)


def parse_actions(raw, event_type):
    value = json.loads(raw)
    if not isinstance(value, list) or len(value) > 4:
        raise ValueError('expected up to four actions')
    result = []
    terminal = False
    for item in value:
        if not isinstance(item, dict) or set(item) - {'name', 'text'} or terminal:
            raise ValueError('invalid action shape/order')
        name, text = item.get('name'), item.get('text', '')
        if name not in {'answer', 'reject', 'say', 'hangup'} or not isinstance(text, str) or len(text.encode()) > 16384:
            raise ValueError('invalid action')
        if (name != 'say' and text) or (name in {'answer', 'reject'} and event_type != 'ring'):
            raise ValueError('invalid action for event')
        terminal = name in {'reject', 'hangup'}
        result.append(Action(name, text))
    return result


class HermesBridge:
    """Fresh killable worker per event; call context is owned only by Agent RAM."""
    def __init__(self, config):
        self.config = validate_config(config)
        self.remaining = config['max_requests']

    async def handle(self, event, context):
        # Local peer policy already admitted this ring; no model spend to answer it.
        if event.type == 'ring':
            return [Action('answer')]
        if self.remaining <= 0:
            return [Action('hangup')]
        self.remaining -= 1  # Reservations are never refunded after errors/cancellation.
        cfg = self.config
        payload = json.dumps({'events': [asdict(e) for e in context]}, ensure_ascii=True).encode()
        if len(payload) > 524288:
            raise ComlinkError('Hermes context too large')
        with tempfile.TemporaryDirectory(prefix='comlink-hermes-') as scratch:
            env = {k: os.environ[k] for k in ('PATH', 'LANG', 'SSL_CERT_FILE', 'SSL_CERT_DIR', 'SYSTEMROOT') if k in os.environ}
            env.update(HERMES_HOME=scratch, PYTHONDONTWRITEBYTECODE='1', PYTHONNOUSERSITE='1')
            proc = await asyncio.create_subprocess_exec(cfg['python'], '-I', str(Path(__file__).with_name('hermes_worker.py')),
                cfg['checkout'], stdin=asyncio.subprocess.PIPE, stdout=asyncio.subprocess.PIPE,
                stderr=asyncio.subprocess.DEVNULL, cwd=scratch, env=env, limit=131072)
            try:
                async with asyncio.timeout(cfg.get('turn_seconds', 45)):
                    private = {k: cfg[k] for k in ('base_url', 'model')}
                    private.update(api_key=Path(cfg['api_key_file']).read_text().strip(),
                        max_tokens=cfg.get('max_tokens', 512))
                    proc.stdin.write(json.dumps(private).encode() + b'\n')
                    await proc.stdin.drain()
                    if await proc.stdout.readline() != b'READY\n':
                        raise ComlinkError('Hermes worker initialization failed')
                    proc.stdin.write(json.dumps({'context': json.loads(payload), 'role': cfg.get('role', '')}).encode() + b'\n')
                    await proc.stdin.drain()
                    proc.stdin.close()
                    line = await proc.stdout.readline()
                    if len(line) > 100000:
                        raise ComlinkError('Hermes response too large')
                    await proc.wait()
                    if proc.returncode:
                        raise ComlinkError('Hermes worker failed')
                    response = json.loads(line)
                    return parse_actions(response['actions'], event.type)
            finally:
                if proc.returncode is None:
                    proc.terminate()
                    try:
                        await asyncio.wait_for(proc.wait(), 1)
                    except asyncio.TimeoutError:
                        proc.kill()
                        await proc.wait()


async def serve(config):
    bridge = HermesBridge(config)
    async with Comlink(**{k: config[k] for k in ('binary', 'state', 'endpoint', 'roots', 'ca') if k in config}) as link:
        await link.register()
        print('Comlink number: ' + link.number, flush=True)
        agent = Agent(link, bridge.handle, allowed_peers=config['allowed_peers'], model_concurrency=1)
        task = asyncio.create_task(agent.run())
        try:
            if config.get('dial'):
                await agent.dial(config['dial'])
            await task
        finally:
            task.cancel()
            await asyncio.gather(task, return_exceptions=True)


def main():
    parser = argparse.ArgumentParser(description='Run an opt-in, transient Hermes Comlink endpoint')
    parser.add_argument('--config', required=True, help='owner-controlled JSON file; never accept peer configuration')
    args = parser.parse_args()
    try:
        asyncio.run(serve(json.loads(Path(args.config).read_text())))
    except KeyboardInterrupt:
        pass
    except Exception:
        raise SystemExit('Hermes bridge stopped; check pinned checkout, private key file, configuration and provider availability.') from None

if __name__ == '__main__':
    main()
