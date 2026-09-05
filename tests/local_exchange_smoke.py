import asyncio,json,os,socket,subprocess,tempfile,time,sys
from pathlib import Path
from comlink_adapter import Comlink,Agent,Action
import argparse
p=argparse.ArgumentParser(description='Maintainer-only isolated local exchange acceptance; no production traffic')
p.add_argument('--server',required=True)
p.add_argument('--binary',required=True)
p.add_argument('--switch',required=True)
args=p.parse_args();full=args.server;binary=args.binary;switch=args.switch
async def exercise(d,endpoint):
    states=[d/'a',d/'b'];numbers=[]
    for state in states:
        async with Comlink(binary=binary,state=state,roots=d/'roots.json',endpoint=endpoint) as c:numbers.append(await c.register())
    for i,state in enumerate(states):
        policy=d/f'policy{i}.json';policy.write_text(json.dumps({'incoming':True,'allow_unknown':False,'allowed':[numbers[1-i]],'blocked':[]}))
        subprocess.run([binary,'policy','--file',str(state/'handset.json'),'--roots',str(d/'roots.json'),'--endpoint',endpoint,'--policy',str(policy)],check=True,stdout=subprocess.DEVNULL)
    completed=[asyncio.Event(),asyncio.Event()]
    def handler(i):
        async def respond(event,context):
            if event.type=='ring':return [Action('answer')]
            if event.type=='answer':return [Action('say','ping')]
            if event.type=='say':
                if event.text=='ping':return [Action('say','pong')]
                if event.text=='pong':completed[i].set();return [Action('hangup')]
                raise AssertionError('unexpected plaintext')
            return []
        return respond
    async with Comlink(binary=binary,state=states[0],roots=d/'roots.json',endpoint=endpoint) as a, Comlink(binary=binary,state=states[1],roots=d/'roots.json',endpoint=endpoint) as b:
        await a.register();await b.register()
        agents=[Agent(a,handler(0),allowed_peers=[numbers[1]]),Agent(b,handler(1),allowed_peers=[numbers[0]])]
        tasks=[asyncio.create_task(agent.run()) for agent in agents]
        try:
            await agents[0].dial(numbers[1]);await asyncio.wait_for(completed[0].wait(),20)
            for _ in range(100):
                if not any(agent.calls for agent in agents):break
                await asyncio.sleep(.02)
            assert not any(agent.calls for agent in agents)
            await agents[1].dial(numbers[0]);await asyncio.wait_for(completed[1].wait(),20)
            for _ in range(100):
                if not any(agent.calls for agent in agents):break
                await asyncio.sleep(.02)
            assert not any(agent.calls for agent in agents)
        finally:
            for task in tasks:task.cancel()
            await asyncio.gather(*tasks,return_exceptions=True)
    for state in states:subprocess.run([binary,'revoke','--file',str(state/'handset.json'),'--roots',str(d/'roots.json'),'--endpoint',endpoint],check=True,stdout=subprocess.DEVNULL)
    print('PASS: packaged async adapter woke both receivers, exchanged encrypted dependent replies, initiated both directions, cleared call state, and revoked both identities; zero model requests.')
with tempfile.TemporaryDirectory(prefix='comlink-adapter-smoke-') as tmp:
    d=Path(tmp);s=socket.socket();s.bind(('127.0.0.1',0));port=s.getsockname()[1];s.close();endpoint=f'http://127.0.0.1:{port}/mcp'
    subprocess.run([full,'mother','--file',str(d/'issuer.json')],check=True,stdout=subprocess.DEVNULL)
    args=['--issuer-file',str(d/'issuer.json'),'--prefix','localtst','--audience',endpoint,'--state',str(d/'authority')]
    (d/'roots.json').write_bytes(subprocess.check_output([full,'public-init',*args]))
    server=subprocess.Popen([full,'serve-public',*args,'--listen',f'127.0.0.1:{port}','--switch',switch],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL,env=dict(os.environ,ERL_FLAGS='+S 2:2'))
    try:time.sleep(2);asyncio.run(exercise(d,endpoint))
    finally:
        server.terminate()
        try:server.wait(timeout=10)
        except subprocess.TimeoutExpired:server.kill();server.wait()
