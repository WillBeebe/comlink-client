"""Real Go/OTP exchange acceptance of the ordinary-host control path. No inference."""
import argparse
import asyncio
import json
import os
from pathlib import Path
import socket
import subprocess
import tempfile
import time
import sys
from comlink_adapter.managed import Receiver, write_private, dispatch, socket_client
from comlink_adapter import Action

async def exercise(d, endpoint, binary):
    receivers = []
    tasks = []
    handled = []
    control = None
    async def handler(event, context):
        assert event.text == 'Please inspect the agreed artifact.'
        handled.append(True)
        return []
    try:
        for i in range(3):
            state = d / str(i); state.mkdir(mode=0o700)
            config = dict(state=str(state), binary=binary, endpoint=endpoint, roots=str(d / 'roots.json'), test_service=i==2)
            r = Receiver(config, handler=handler if i < 2 else None)
            receivers.append(r); tasks.append(asyncio.create_task(r.run()))
            try: await asyncio.wait_for(r.ready.wait(), 20)
            except TimeoutError: raise AssertionError(r.status_code) from None
        a, b, test = receivers
        control = await asyncio.start_unix_server(lambda r,w: socket_client(a,r,w), path=a.state/"control.sock")
        (a.state/"control.sock").chmod(0o600)
        for r, name, peer in [(a,'Jane',b),(b,'Alex',a),(a,'Comlink connection test',test),(b,'Comlink connection test',test)]:
            result = await r.contact(name, peer.link.number, True)
            assert result['policy_synced'], result
        probe = await a.probe('Comlink connection test'); assert probe['passed'], (probe, [(r.status_code, len(r.calls),len(r.callbacks)) for r in receivers])
        assert a.status()['ready']
        async def ordinary_host_send():
            host = await asyncio.create_subprocess_exec(sys.executable, '-m', 'comlink_adapter.onboarding', 'mcp', '--state', str(a.state), stdin=asyncio.subprocess.PIPE, stdout=asyncio.subprocess.PIPE, stderr=asyncio.subprocess.DEVNULL)
            async def rpc(i,method,params):
                host.stdin.write(json.dumps({'jsonrpc':'2.0','id':i,'method':method,'params':params}).encode()+b'\n');await host.stdin.drain()
                return json.loads(await asyncio.wait_for(host.stdout.readline(),90))
            try:
                init=await rpc(1,'initialize',{'protocolVersion':'2025-06-18','capabilities':{},'clientInfo':{'name':'ordinary-host','version':'1'}})
                assert init['result']['capabilities']=={'tools':{}}
                listed=await rpc(2,'tools/list',{})
                assert any(t['name']=='comlink_send' for t in listed['result']['tools'])
                return await rpc(3,'tools/call',{'name':'comlink_send','arguments':{'to':'Jane','text':'Please inspect the agreed artifact.'}})
            finally:
                host.stdin.close();await asyncio.wait_for(host.wait(),5)
        result = await ordinary_host_send()
        assert result.get('result',{}).get('structuredContent',{}).get('status') == 'agent_handled', result
        assert handled == [True]
        async with asyncio.timeout(5):
            while a.calls or b.calls: await asyncio.sleep(.01)
        first_number = a.link.number
        a.link._stop()
        await asyncio.sleep(.1)
        await asyncio.wait_for(a.ready.wait(), 20)
        assert a.link.number == first_number
        assert not a.status()['connection_test_passed']
        probe = await a.probe('Comlink connection test'); assert probe['passed'], (probe, [(r.status_code, len(r.calls),len(r.callbacks)) for r in receivers])
        assert (await ordinary_host_send())['result']['structuredContent']['status'] == 'agent_handled'
        async with asyncio.timeout(5):
            while a.calls or b.calls: await asyncio.sleep(.01)
        cli = await asyncio.create_subprocess_exec(sys.executable, '-m', 'comlink_adapter.onboarding', 'send', '--state', str(a.state), '--to', 'Jane', '--message-stdin', stdin=asyncio.subprocess.PIPE, stdout=asyncio.subprocess.PIPE, stderr=asyncio.subprocess.DEVNULL)
        output, _ = await asyncio.wait_for(cli.communicate(b'Please inspect the agreed artifact.'),90)
        assert cli.returncode == 0 and json.loads(output)['status'] == 'agent_handled'
        async with asyncio.timeout(5):
            while a.calls or b.calls: await asyncio.sleep(.01)
        await b.contact('Alex', first_number, False)
        result = await a.send('Jane','Please inspect the agreed artifact.')
        assert result['status'] != 'agent_handled', result
        assert handled == [True,True,True]
        await asyncio.sleep(.1)
        await asyncio.wait_for(a.ready.wait(),20)
        await b.close(); tasks[1].cancel(); await asyncio.gather(tasks[1], return_exceptions=True)
        result = await a.send('Jane','Please inspect the agreed artifact.')
        assert result['status'] == 'peer_offline', result
        for path in d.rglob('*'):
            if path.is_file(): assert b'Please inspect the agreed artifact.' not in path.read_bytes(), path
        print('PASS: managed MCP send, mutual consent, no-model round trip and return call, reconnect/number reuse, rejection, offline failure, no speech on disk.')
    finally:
        if control: control.close();await control.wait_closed()
        for r in receivers: await r.close()
        for task in tasks: task.cancel()
        await asyncio.gather(*tasks, return_exceptions=True)


def main():
    p=argparse.ArgumentParser();p.add_argument('--server',required=True);p.add_argument('--binary',required=True);p.add_argument('--switch',required=True);a=p.parse_args()
    with tempfile.TemporaryDirectory(prefix='cl-smoke-',dir='/private/tmp') as temp:
        d=Path(temp).resolve();s=socket.socket();s.bind(('127.0.0.1',0));port=s.getsockname()[1];s.close();endpoint=f'http://127.0.0.1:{port}/mcp'
        subprocess.run([a.server,'mother','--file',str(d/'issuer.json')],check=True,stdout=subprocess.DEVNULL)
        flags=['--issuer-file',str(d/'issuer.json'),'--prefix','localtst','--audience',endpoint,'--state',str(d/'authority')]
        (d/'roots.json').write_bytes(subprocess.check_output([a.server,'public-init',*flags]))
        server=subprocess.Popen([a.server,'serve-public',*flags,'--listen',f'127.0.0.1:{port}','--switch',a.switch],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL,env=dict(os.environ,ERL_FLAGS='+S 2:2'))
        try: time.sleep(2);asyncio.run(exercise(d,endpoint,a.binary))
        finally:
            server.terminate()
            try:server.wait(timeout=10)
            except subprocess.TimeoutExpired:server.kill();server.wait()
if __name__=='__main__':main()
