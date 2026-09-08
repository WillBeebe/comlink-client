"""Installable owner-facing lifecycle and ordinary-host MCP entry point."""
import argparse
import asyncio
from contextlib import suppress
import fcntl
import json
import os
from pathlib import Path
import plistlib
import shutil
import signal
import subprocess
import sys
import time
import tomllib

from .managed import ManagedError, private_dir, read_private, write_private, number, request, serve

DEFAULT_STATE = Path.home() / '.local/share/comlink'


def command(state, action):
    return [sys.executable, '-m', 'comlink_adapter.onboarding', action, '--state', str(state)]


def host_configuration(host, state):
    cmd = command(state, 'mcp')
    if host == 'opencode': return {'mcp': {'comlink': {'type': 'local', 'command': cmd, 'enabled': True, 'timeout': 100000}}}
    if host == 'codex': return {'mcp_servers': {'comlink': {'command': cmd[0], 'args': cmd[1:], 'tool_timeout_sec': 100}}}
    return {'mcpServers': {'comlink': {'command': cmd[0], 'args': cmd[1:]}}}


def configure_host(host, state, path=None):
    """Preserve other host settings; refuse conflicting existing Comlink entries."""
    config = host_configuration(host, state)
    if host == 'generic':
        write_private(state / 'mcp-config.json', config)
        return {'configuration': str(state / 'mcp-config.json'), 'host_reload_required': True, 'host_configured': False}
    if path is None:
        path = ((Path(os.environ.get('CODEX_HOME', str(Path.home() / '.codex'))) / 'config.toml') if host == 'codex'
                else Path(os.environ.get('XDG_CONFIG_HOME', str(Path.home() / '.config'))) / 'opencode/opencode.json')
    path = Path(path).absolute()
    # Config can contain secrets: never include its contents in errors/backups/logs.
    if path.is_symlink(): raise ManagedError('host_config_symlink')
    path.parent.mkdir(mode=0o700, parents=True, exist_ok=True)
    if host == 'opencode' and path.with_suffix('.jsonc').exists():
        raise ManagedError('opencode_jsonc_requires_host_native_merge')
    raw = path.read_text() if path.exists() else ''
    try: existing = (tomllib.loads(raw) if host == 'codex' else json.loads(raw)) if raw else {}
    except ValueError: raise ManagedError('host_config_needs_supported_parser') from None
    key = 'mcp_servers' if host == 'codex' else 'mcp'
    desired = config[key]['comlink']
    prior = existing.get(key, {}).get('comlink')
    if prior is not None and prior != desired: raise ManagedError('existing_comlink_host_entry_conflicts')
    if prior is None:
        if host == 'codex':
            # Append a new TOML table; parse again before replacing the original.
            new = raw + '\n[mcp_servers.comlink]\n' + '\n'.join(k + ' = ' + json.dumps(v) for k, v in desired.items()) + '\n'
            tomllib.loads(new)
        else:
            existing.setdefault(key, {})['comlink'] = desired
            new = json.dumps(existing, indent=2) + '\n'
        import tempfile
        fd, tmp = tempfile.mkstemp(prefix='.comlink-config-', dir=path.parent)
        try:
            with os.fdopen(fd, 'w') as f: f.write(new); f.flush(); os.fsync(f.fileno())
            # Refuse concurrent edits. Nothing is overwritten from a stale read.
            if (path.read_text() if path.exists() else '') != raw: raise ManagedError('host_config_changed_retry')
            os.replace(tmp, path)
        finally:
            with suppress(FileNotFoundError): os.unlink(tmp)
    return {'configuration': str(path), 'host_reload_required': True, 'host_configured': True}


def service_name(state):
    import hashlib
    return 'comlink.receiver.' + hashlib.sha256(str(state).encode()).hexdigest()[:12]


def install_service(state):
    cmd = command(state, 'serve')
    name = service_name(state)
    if sys.platform == 'darwin':
        destination = Path.home() / 'Library/LaunchAgents' / (name + '.plist')
        destination.parent.mkdir(parents=True, exist_ok=True)
        data = plistlib.dumps({'Label': name, 'ProgramArguments': cmd, 'RunAtLoad': True,
                              'KeepAlive': {'SuccessfulExit': False}, 'ThrottleInterval': 30,
                              'StandardOutPath': '/dev/null', 'StandardErrorPath': '/dev/null',
                              'EnvironmentVariables': {'PATH': os.environ.get('PATH', '/usr/bin:/bin')}})
        if destination.is_symlink(): raise ManagedError('service_path_symlink')
        if destination.exists() and destination.read_bytes() != data: raise ManagedError('service_configuration_conflicts')
        if not destination.exists():
            fd = os.open(destination, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
            with os.fdopen(fd, 'wb') as f: f.write(data)
        domain = f'gui/{os.getuid()}'
        if subprocess.run(['launchctl', 'print', domain + '/' + name], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL).returncode:
            subprocess.run(['launchctl', 'bootstrap', domain, str(destination)], check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        return {'manager': 'launchd', 'service': name}
    if sys.platform.startswith('linux'):
        destination = Path.home() / '.config/systemd/user' / (name + '.service')
        destination.parent.mkdir(parents=True, exist_ok=True)
        # systemd quoting is not shell quoting; escape percent specifiers too.
        quoted = ' '.join('"' + x.replace('\\', '\\\\').replace('"', '\\"').replace('%', '%%') + '"' for x in cmd)
        data = '[Unit]\nDescription=Comlink managed receiver\n[Service]\nType=simple\nExecStart=' + quoted + '\nRestart=on-failure\nRestartSec=30\nUMask=0077\nStandardOutput=null\nStandardError=null\n[Install]\nWantedBy=default.target\n'
        if destination.is_symlink(): raise ManagedError('service_path_symlink')
        if destination.exists() and destination.read_text() != data: raise ManagedError('service_configuration_conflicts')
        if not destination.exists():
            with destination.open('x') as f: f.write(data)
            destination.chmod(0o600)
        subprocess.run(['systemctl', '--user', 'daemon-reload'], check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        subprocess.run(['systemctl', '--user', 'enable', '--now', name + '.service'], check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        return {'manager': 'systemd-user', 'service': name, 'lifetime': 'user_session; enable lingering separately if needed'}
    raise ManagedError('supported_platforms_macos_linux')


async def tool(state, name, arguments=None):
    result = await request(state, {'jsonrpc': '2.0', 'id': 1, 'method': 'tools/call',
                                   'params': {'name': name, 'arguments': arguments or {}}})
    if result.get('error') or result.get('result', {}).get('isError'):
        raise ManagedError('control_operation_failed')
    return result['result']['structuredContent']


async def stop_service(state):
    try:
        status = await tool(state, 'comlink_status')
    except (OSError, ValueError, ManagedError):
        # No local socket means no managed service to migrate. A stale socket is ambiguous.
        if (state / 'control.sock').exists(): raise ManagedError('receiver_status_unavailable_do_not_interrupt')
        return
    if status['active_calls']: raise ManagedError('busy_finish_call_first')
    await tool(state, 'comlink_pause')
    name = service_name(state)
    if sys.platform == 'darwin':
        args = ['launchctl', 'bootout', f'gui/{os.getuid()}/' + name]
    else:
        args = ['systemctl', '--user', 'stop', name + '.service']
    subprocess.run(args, check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    for _ in range(50):
        if not (state / 'control.sock').exists(): return
        await asyncio.sleep(.1)
    raise ManagedError('receiver_stop_incomplete')


async def onboard(args):
    state = private_dir(args.state)
    config_path = state / 'managed.json'
    if not args.test_number:
        published = json.loads(Path(__file__).with_name('test_contact.json').read_text())
        if published.get('available') is True: args.test_number = number(published['number'])
    if config_path.exists():
        config = read_private(config_path)
        # Re-running onboarding must not silently rebind a profile/provider.
        if args.receiver_config:
            selected = str(Path(args.receiver_config).absolute())
            if config.get('receiver_config') not in (None, selected): raise ManagedError('receiver_config_conflicts')
            if not config.get('receiver_config'):
                from .coding import validate
                receiver = validate(read_private(selected))
                await stop_service(state)
                budget = state / 'receiver-budget.json'
                if not budget.exists(): write_private(budget, {'limit': receiver['max_requests'], 'used': 0})
                config['receiver_config'] = selected
                write_private(config_path, config)
        if args.test_number and config.get('test_number') != number(args.test_number):
            if config.get('test_number'): raise ManagedError('published_test_number_changed_verify_before_replacing')
            await stop_service(state)
            config['test_number'] = number(args.test_number)
            write_private(config_path, config)
    else:
        binary = str(Path(args.binary or Path.home() / '.local/bin/comlink').absolute())
        proc = await asyncio.create_subprocess_exec(binary, 'setup', '--state', str(state),
                                                   stdout=asyncio.subprocess.DEVNULL, stderr=asyncio.subprocess.DEVNULL)
        if await proc.wait(): raise ManagedError('native_setup_failed')
        config = {'binary': binary, 'state': str(state), 'roots': str(state / 'roots.json'),
                  'endpoint': 'https://api.comlink.fyi/mcp'}
        if args.receiver_config:
            config['receiver_config'] = str(Path(args.receiver_config).absolute())
            # Validate before persisting or installing a broken background service.
            from .coding import validate
            receiver = validate(read_private(config['receiver_config']))
            budget = state / 'receiver-budget.json'
            if not budget.exists(): write_private(budget, {'limit': receiver['max_requests'], 'used': 0})
        if args.test_number: config['test_number'] = number(args.test_number)
        write_private(config_path, config)
    host = configure_host(args.host, state, args.host_config)
    manager = install_service(state)
    status = None
    for _ in range(60):
        try:
            status = await tool(state, 'comlink_status')
            if status['connected']: break
        except (OSError, ValueError, ManagedError): pass
        await asyncio.sleep(.5)
    if not status or not status['connected']: raise ManagedError('receiver_not_connected_check_service')
    if not args.test_number:
        published = json.loads(Path(__file__).with_name('test_contact.json').read_text())
        if published.get('available') is True: args.test_number = number(published['number'])
    test_result = {'passed': False, 'status': 'test_contact_not_configured'}
    if args.test_number:
        contact = await tool(state, 'comlink_add_contact', {'name': 'Comlink connection test', 'number': number(args.test_number), 'approved': True})
        if contact['policy_synced']:
            test_result = await tool(state, 'comlink_test', {'contact': 'Comlink connection test'})
    status = await tool(state, 'comlink_status')
    result = dict(status, **host, **manager, test=test_result)
    # Host has to demonstrate a tool call after configuration reload.
    result['ready'] = False
    result['next_step'] = ('Reload this host’s MCP connection, call comlink_status and comlink_test from this agent.'
                           if status['receiver_available'] else 'Configure a bounded receiving agent; transport installed, agent reception incomplete.')
    print(json.dumps(result, indent=2))


async def stdio_mcp_async(state):
    # Read control messages independently so MCP cancellation closes the live IPC request.
    reader = asyncio.StreamReader(limit=131073)
    protocol = asyncio.StreamReaderProtocol(reader)
    transport, _ = await asyncio.get_running_loop().connect_read_pipe(lambda: protocol, sys.stdin.buffer)
    pending = {}
    initialized = False
    def emit(result):
        sys.stdout.buffer.write(json.dumps(result).encode() + b'\n')
        sys.stdout.buffer.flush()
    async def perform(message):
        try:
            result = await request(state, message)
        except asyncio.CancelledError:
            return
        except (OSError, ValueError, ManagedError, TimeoutError):
            result = {'jsonrpc': '2.0', 'id': message.get('id'), 'error': {'code': -32000, 'message': 'Comlink receiver offline; run onboarding to restore it'}}
        emit(result)
    try:
        while True:
            raw = await reader.readline()
            if not raw: break
            if len(raw) > 131072: raise ManagedError('request_too_large')
            try: message = json.loads(raw)
            except ValueError:
                emit({'jsonrpc': '2.0', 'id': None, 'error': {'code': -32700, 'message': 'Parse error'}}); continue
            if not isinstance(message, dict): continue
            if message.get('method') == 'notifications/cancelled':
                target = message.get('params', {}).get('requestId')
                if isinstance(target, (str, int)) and target in pending: pending[target].cancel()
                continue
            if 'id' not in message: continue
            ident = message['id']
            if type(ident) not in (str, int):
                emit({'jsonrpc': '2.0', 'id': None, 'error': {'code': -32600, 'message': 'Invalid id'}}); continue
            if not initialized:
                if message.get('method') != 'initialize':
                    emit({'jsonrpc': '2.0', 'id': ident, 'error': {'code': -32000, 'message': 'Initialize first'}})
                else:
                    try:
                        result = await request(state, message)
                        initialized = 'result' in result
                        emit(result)
                    except (OSError, ValueError, ManagedError):
                        emit({'jsonrpc': '2.0', 'id': ident, 'error': {'code': -32000, 'message': 'Comlink receiver offline; restore the managed service'}})
                continue
            if ident in pending or len(pending) >= 16:
                emit({'jsonrpc': '2.0', 'id': ident, 'error': {'code': -32000, 'message': 'Busy or duplicate request id'}}); continue
            task = pending[ident] = asyncio.create_task(perform(message))
            task.add_done_callback(lambda _, key=ident: pending.pop(key, None))
    finally:
        transport.close()
        for task in list(pending.values()): task.cancel()
        if pending: await asyncio.gather(*list(pending.values()), return_exceptions=True)


def stdio_mcp(state):
    asyncio.run(stdio_mcp_async(state))


def main():
    p = argparse.ArgumentParser(description='Your agent’s managed Comlink connection')
    p.add_argument('action', choices=['onboard', 'serve', 'mcp', 'status', 'test', 'invitation', 'stop', 'send', 'add-contact'])
    p.add_argument('--state', type=Path, default=DEFAULT_STATE)
    p.add_argument('--binary')
    p.add_argument('--host', choices=['opencode', 'codex', 'generic'], default='generic')
    p.add_argument('--host-config', type=Path)
    p.add_argument('--receiver-config')
    p.add_argument('--test-number')
    p.add_argument('--contact', default='Comlink connection test')
    p.add_argument('--to')
    p.add_argument('--message-stdin', action='store_true')
    p.add_argument('--name')
    p.add_argument('--number')
    approval = p.add_mutually_exclusive_group()
    approval.add_argument('--approve', dest='approved', action='store_true')
    approval.add_argument('--no-approve', dest='approved', action='store_false')
    p.set_defaults(approved=None)
    args = p.parse_args()
    try:
        state = private_dir(args.state)
        if args.action == 'onboard': asyncio.run(onboard(args))
        elif args.action == 'serve':
            async def run():
                task = asyncio.create_task(serve(read_private(state / 'managed.json')))
                loop = asyncio.get_running_loop()
                for sig in (signal.SIGINT, signal.SIGTERM): loop.add_signal_handler(sig, task.cancel)
                try: await task
                except asyncio.CancelledError: pass
            asyncio.run(run())
        elif args.action == 'mcp': stdio_mcp(state)
        elif args.action == 'stop': asyncio.run(stop_service(state)); print('Comlink receiver stopped; your number and contacts are preserved.')
        elif args.action == 'send':
            if not args.to or not args.message_stdin: raise ManagedError('send_requires_to_and_message_stdin')
            raw = sys.stdin.buffer.read(16385)
            if not 0 < len(raw) <= 16384: raise ManagedError('invalid_message')
            value = asyncio.run(tool(state, 'comlink_send', {'to': args.to, 'text': raw.decode('utf-8')}))
            print(json.dumps(value))
        elif args.action == 'add-contact':
            if not args.name or not args.number or args.approved is None: raise ManagedError('contact_requires_name_number_and_explicit_approval')
            print(json.dumps(asyncio.run(tool(state, 'comlink_add_contact', {'name': args.name, 'number': number(args.number), 'approved': args.approved}))))
        else:
            name = {'status': 'comlink_status', 'test': 'comlink_test', 'invitation': 'comlink_invitation'}[args.action]
            print(json.dumps(asyncio.run(tool(state, name, {'contact': args.contact} if args.action == 'test' else {})), indent=2))
    except KeyboardInterrupt: pass
    except ManagedError as e: raise SystemExit('Comlink: ' + str(e)) from None
    except Exception: raise SystemExit('Comlink setup incomplete; check private configuration, host access and service availability.') from None


if __name__ == '__main__': main()
