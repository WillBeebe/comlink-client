"""Actual Codex/OpenCode acceptance with synthetic inference only."""
import argparse
import asyncio
import json
import subprocess
import sys
import tempfile
import threading
import time
from pathlib import Path
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from unittest.mock import patch
from comlink_adapter import Action, Agent, Event
from comlink_adapter.coding import CodingBridge
from comlink_adapter.inference_gate import InferenceGate
from test_adapter import FakeLink

MARKER='synthetic-coding-private-speech-03c2ce19'
class Provider(BaseHTTPRequestHandler):
    mode='success';requests=0;entered=threading.Event();release=threading.Event()
    def log_message(self,*_):pass
    def do_POST(self):
        cls=type(self);cls.requests+=1
        b=json.loads(self.rfile.read(int(self.headers['Content-Length'])))
        assert b['model']=='fixture-model' and b['stream'] is False and b['tools']==[]
        if self.path!='/v1/messages':assert b['tool_choice']=='none'
        responses=self.path=='/v1/responses'
        assert responses or self.path in {'/v1/chat/completions','/v1/messages'}
        assert b['max_output_tokens' if responses else 'max_tokens']==512
        if responses:assert b['store'] is False
        cls.entered.set()
        if cls.mode=='delay':cls.release.wait(30)
        if cls.mode=='error':status,value=500,{'error':{'message':MARKER}}
        else:
            rows=b['input'] if responses else b['messages']
            event=None
            for row in reversed(rows):
                content=row.get('content','')
                text=''.join(c.get('text','') for c in content) if isinstance(content,list) else content
                start=text.find('{"events":')
                if start>=0:
                    packet,_=json.JSONDecoder().raw_decode(text[start:])
                    event=packet['events'][-1];break
            assert event is not None, 'fixture did not receive event context'
            if event['type']=='answer':actions=[{'name':'say','text':'ping'}]
            elif event['text']=='ping':actions=[{'name':'say','text':'pong'}]
            elif event['text']=='pong':actions=[{'name':'hangup'}]
            else:actions=[{'name':'say','text':'synthetic reply\nsecond line'}]
            answer=json.dumps(actions)
            if responses:
                output=[{'id':'msg_fixture','type':'message','role':'assistant','status':'completed','content':[{'type':'output_text','text':answer,'annotations':[]}]}]
                if cls.mode=='tool':output=[{'type':'function_call','name':'shell','arguments':'{}','call_id':'forbidden'}]
                value={'id':'resp_fixture','object':'response','created_at':1,'status':'completed','model':'fixture-model','output':output,'usage':{'input_tokens':10,'output_tokens':10,'total_tokens':20}}
            elif self.path=='/v1/messages':
                content=[{'type':'text','text':answer}]
                if cls.mode=='tool':content=[{'type':'tool_use','id':'forbidden','name':'shell','input':{}}]
                value={'id':'msg_fixture','type':'message','role':'assistant','model':'fixture-model','content':content,'stop_reason':'end_turn','stop_sequence':None,'usage':{'input_tokens':10,'output_tokens':10}}
            else:
                message={'role':'assistant','content':answer}
                if cls.mode=='tool':message['tool_calls']=[{'id':'forbidden','type':'function','function':{'name':'shell','arguments':'{}'}}]
                value={'id':'fixture','object':'chat.completion','created':1,'model':'fixture-model','choices':[{'index':0,'message':message,'finish_reason':'stop'}],'usage':{'prompt_tokens':10,'completion_tokens':10,'total_tokens':20}}
            status=200
        raw=json.dumps(value).encode()
        try:
            self.send_response(status);self.send_header('Content-Type','application/json');self.send_header('Content-Length',str(len(raw)));self.end_headers();self.wfile.write(raw)
        except (BrokenPipeError,ConnectionResetError):pass

async def exercise(cfg):
    processes,gates,homes=[],[],[]
    original=asyncio.create_subprocess_exec
    async def spawn(*args,**kwargs):
        proc=await original(*args,**kwargs);processes.append(proc);return proc
    original_temp=tempfile.TemporaryDirectory
    class Inspect(original_temp):
        def __exit__(self,*args):
            homes.append(Path(self.name))
            for f in Path(self.name).rglob('*'):
                if f.is_file() and not f.is_symlink():
                    data=f.read_bytes()
                    assert MARKER.encode() not in data and b'synthetic reply' not in data, 'speech persisted'
            return super().__exit__(*args)
    class Gate(InferenceGate):
        def __enter__(self):
            result=super().__enter__();gates.append(self.process);return result
    event=Event('say','b'*64,'a'*128,MARKER)
    with patch('comlink_adapter.coding.asyncio.create_subprocess_exec',spawn),patch('comlink_adapter.coding.tempfile.TemporaryDirectory',Inspect),patch('comlink_adapter.coding.InferenceGate',Gate):
        bridge=CodingBridge(cfg)
        Provider.mode='success';before=Provider.requests
        assert await bridge.handle(event,(event,))==[Action('say','synthetic reply\nsecond line')]
        assert Provider.requests==before+1
        for mode in ('error','tool'):
            Provider.mode=mode;before=Provider.requests
            try:await bridge.handle(event,(event,))
            except Exception as exc:
                assert not isinstance(exc,TimeoutError), "provider failure was not propagated promptly"
                assert MARKER not in str(exc)
            else:raise AssertionError('failure or tool response accepted')
            assert Provider.requests==before+1
        Provider.mode='delay';Provider.entered.clear();Provider.release.clear()
        link=FakeLink();agent=Agent(link,bridge.handle,allowed_peers=cfg['allowed_peers'])
        task=asyncio.create_task(agent.run());await asyncio.sleep(0)
        link.event(Event('ring',event.call,event.sender));link.event(event)
        assert await asyncio.to_thread(Provider.entered.wait,30)
        started=time.monotonic();link.event(Event('hangup',event.call,event.sender))
        await asyncio.wait_for(asyncio.gather(*list(agent.tasks),return_exceptions=True),3)
        assert time.monotonic()-started<3 and not agent.calls
        assert [n for n,_ in link.sent]==['answer']
        link._stop();await task
        Provider.release.set();Provider.mode='success'
        assert bridge.remaining==cfg['max_requests']-4
    assert all(p.returncode is not None for p in processes)
    assert all(not p.is_alive() for p in gates)
    assert not any(p.exists() for p in homes)
    print('PASS:',cfg['runtime'],'reply, error, tool rejection, single request, hangup kills runtime and gate, speech absent from scratch files.',flush=True)

def main():
    p=argparse.ArgumentParser(description=__doc__)
    p.add_argument('--copilot-python');p.add_argument('--copilot-cli');p.add_argument('--langgraph-python');p.add_argument('--openclaw-node');p.add_argument('--openclaw-package');p.add_argument('--codex');p.add_argument('--opencode');p.add_argument('--adk-python');p.add_argument('--claude-python')
    for n in ('server','binary','switch'):p.add_argument('--'+n)
    a=p.parse_args()
    selected=[(r,b) for r,b in [('copilot',a.copilot_python),('langgraph',a.langgraph_python),('openclaw',a.openclaw_node),('codex',a.codex),('opencode',a.opencode),('adk',a.adk_python),('claude-sdk',a.claude_python)] if b]
    if not selected:p.error('select at least one runtime')
    if any((a.server,a.binary,a.switch)) and not all((a.server,a.binary,a.switch)):p.error('all exchange paths required')
    server=ThreadingHTTPServer(('127.0.0.1',0),Provider);threading.Thread(target=server.serve_forever,daemon=True).start()
    try:
        with tempfile.TemporaryDirectory(prefix='comlink-coding-test-') as d:
            key=Path(d)/'key';key.write_text('synthetic-key');key.chmod(0o600)
            configs=[dict(runtime=n,runtime_binary=binary,base_url=f'http://127.0.0.1:{server.server_port}/v1',model='fixture-model',api_key_file=str(key),allowed_peers=['a'*128],max_requests=8,turn_seconds=60) for n,binary in selected]
            for cfg in configs:
                if cfg['runtime']=='copilot':cfg['runtime_cli']=a.copilot_cli
                if cfg['runtime']=='openclaw':cfg['runtime_package']=a.openclaw_package
                asyncio.run(exercise(cfg))
            if a.server:
                assert len(configs)==2, 'select exactly two runtimes for circuit acceptance'
                f=Path(d)/'config.json';f.write_text(json.dumps(configs));before=Provider.requests
                subprocess.run([sys.executable,str(Path(__file__).with_name('local_exchange_smoke.py')),'--server',a.server,'--binary',a.binary,'--switch',a.switch,'--coding-config',str(f)],check=True,timeout=180)
                assert Provider.requests==before+6
                print('PASS:',configs[0]['runtime'],'and',configs[1]['runtime'],'called each other both ways through Elixir; six synthetic inference turns.',flush=True)
    finally:Provider.release.set();server.shutdown();server.server_close()
if __name__=='__main__':main()
