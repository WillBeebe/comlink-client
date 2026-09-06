"""One request, tool-free loopback gate for external coding runtimes."""
import json
import secrets
import multiprocessing
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.request import Request, urlopen

class _GateServer:
    def __init__(self, config, protocol, token, failed):
        self.config, self.protocol = config, protocol
        self.token = token
        self.failed = failed
        self.used = False
        self.lock = threading.Lock()
        gate = self
        class Handler(BaseHTTPRequestHandler):
            def log_message(self, *_): pass
            def do_POST(self):
                try:
                    route = '/v1/responses' if gate.protocol == 'responses' else '/v1/chat/completions'
                    if self.path != route or self.headers.get('Authorization') != 'Bearer ' + gate.token:
                        raise ValueError('route/auth denied')
                    length = int(self.headers.get('Content-Length', '0'))
                    if not 0 < length <= 1048576: raise ValueError('input size')
                    body = json.loads(self.rfile.read(length))
                    if body.get('model') != config['model']: raise ValueError('model mismatch')
                    with gate.lock:
                        if gate.used: raise ValueError('budget exhausted')
                        gate.used = True
                    body.update(stream=False, tools=[], tool_choice='none')
                    body.pop('stream_options', None)
                    if protocol == 'responses':
                        body.update(store=False, max_output_tokens=config.get('max_tokens',512))
                        body.pop('previous_response_id', None)
                    else:
                        body.pop('max_completion_tokens', None)
                        body['max_tokens'] = config.get('max_tokens',512)
                    req = Request(config['base_url'].rstrip('/') + route.removeprefix('/v1'),
                        data=json.dumps(body).encode(), headers={'Content-Type':'application/json',
                        'Authorization':'Bearer '+config['_api_key']}, method='POST')
                    # Redirects could move credentials/context to another route: refuse them.
                    import urllib.request
                    class NoRedirect(urllib.request.HTTPRedirectHandler):
                        def redirect_request(self,*a,**kw): return None
                    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), NoRedirect())
                    with opener.open(req,timeout=30) as response:
                        raw=response.read(262145)
                    if len(raw)>262144: raise ValueError('response size')
                    value=json.loads(raw)
                    if protocol == 'responses':
                        output=value.get('output',[])
                        if not output or any(i.get('type') not in {'message','reasoning'} for i in output):
                            raise ValueError('non-text response')
                        events=[{'type':'response.created','response':dict(value,output=[],status='in_progress')}]
                        for index,item in enumerate(output):
                            events.append({'type':'response.output_item.added','output_index':index,'item':dict(item,content=[]) if item['type']=='message' else item})
                            if item['type']=='message':
                                for part_index,part in enumerate(item.get('content',[])):
                                    if part.get('type')!='output_text':raise ValueError('non-text content')
                                    common={'item_id':item['id'],'output_index':index,'content_index':part_index}
                                    events.append(dict(common,type='response.content_part.added',part=dict(part,text='')))
                                    events.append(dict(common,type='response.output_text.delta',delta=part['text']))
                                    events.append(dict(common,type='response.output_text.done',text=part['text']))
                                    events.append(dict(common,type='response.content_part.done',part=part))
                            events.append({'type':'response.output_item.done','output_index':index,'item':item})
                        events.append({'type':'response.completed','response':value})
                        wire=''.join('event: '+e['type']+'\n'+'data: '+json.dumps(e)+'\n\n' for e in events)
                        self.send_response(200);self.send_header('Content-Type','text/event-stream');self.end_headers();self.wfile.write(wire.encode())
                    else:
                        choices=value['choices']
                        if len(choices)!=1 or choices[0]['message'].get('tool_calls') or choices[0]['message'].get('function_call'):
                            raise ValueError('non-text response')
                        message=choices[0]['message']
                        # OpenCode requests SSE. Emit only validated text and a stop marker.
                        base={'id':value.get('id','comlink'),'object':'chat.completion.chunk','created':0,'model':config['model']}
                        chunks=[dict(base,choices=[{'index':0,'delta':{'role':'assistant','content':message['content']},'finish_reason':None}]),dict(base,choices=[{'index':0,'delta':{},'finish_reason':'stop'}],usage=value.get('usage',{}))]
                        wire=''.join('data: '+json.dumps(c)+'\n\n' for c in chunks)+'data: [DONE]\n\n'
                        self.send_response(200);self.send_header('Content-Type','text/event-stream');self.end_headers();self.wfile.write(wire.encode())
                except Exception:
                    if gate.used: gate.failed.set()
                    try: self.send_error(502, 'Comlink inference refused or failed')
                    except OSError: pass
        self.server=ThreadingHTTPServer(('127.0.0.1',0),Handler)
        self.server.daemon_threads=True


def _serve(config, protocol, token, ready, failed):
    try:
        gate=_GateServer(config,protocol,token,failed)
        ready.send(gate.server.server_port)
        ready.close()
        gate.server.serve_forever()
    except BaseException:
        # Provider failures, configuration and speech never go to process diagnostics.
        pass

class InferenceGate:
    """Killable provider process: no lingering HTTP thread retains ended-call speech."""
    def __init__(self, config, protocol):
        self.config,self.protocol=config,protocol
        self.token=secrets.token_hex(32)
        self.process=None
    def __enter__(self):
        ctx=multiprocessing.get_context('spawn')
        parent,child=ctx.Pipe(duplex=False)
        self.failed=ctx.Event()
        self.process=ctx.Process(target=_serve,args=(self.config,self.protocol,self.token,child,self.failed),daemon=True)
        try:
            self.process.start();child.close()
            if not parent.poll(10):raise RuntimeError('inference gate startup timed out')
            self.url=f'http://127.0.0.1:{parent.recv()}/v1'
            return self
        except BaseException:
            self.__exit__()
            raise
        finally:
            parent.close();child.close()
    def __exit__(self,*_):
        if self.process and self.process.pid:
            if self.process.is_alive():self.process.terminate()
            self.process.join(1)
            if self.process.is_alive():self.process.kill();self.process.join(1)
