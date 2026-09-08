import json
from pathlib import Path
import tempfile
import threading
import unittest
from unittest.mock import patch
from urllib.request import Request, urlopen
from urllib.error import HTTPError
from http.server import ThreadingHTTPServer
from comlink_adapter.coding import validate, command
from comlink_adapter.inference_gate import InferenceGate
from coding_smoke import Provider

class CodingTests(unittest.TestCase):
    def test_new_handset_tools_do_not_expand_callback_permissions(self):
        from comlink_adapter.client import validate_tool_list, TOOLS, CONTACT_TOOLS, ComlinkError
        for names in (TOOLS, TOOLS | CONTACT_TOOLS):
            validate_tool_list({'tools':[{'name':n} for n in names]})
        self.assertNotIn('contacts_save',TOOLS)
        validate_tool_list({'tools':[{'name':n} for n in TOOLS | {'update_profile', 'shell'}]})
        self.assertNotIn('shell', TOOLS)
        with self.assertRaises(ComlinkError):
            validate_tool_list({'tools':[{'name':n} for n in TOOLS - {'say'}]})

    def test_unverified_runtimes_refused_before_launch(self):
        for runtime in ('cursor','grok-build'):
            with self.assertRaises(ValueError):
                validate(dict(runtime=runtime,runtime_binary='/unused',base_url='https://example.com/v1',model='m',api_key_file='/unused',allowed_peers=[],max_requests=1))

    def test_unverified_version_refused(self):
        with patch('comlink_adapter.coding.subprocess.check_output',return_value=b'codex-cli unknown'):
            with self.assertRaises(ValueError):
                validate(dict(runtime='codex',runtime_binary='/unused',base_url='https://example.com/v1',model='m',api_key_file='/unused',allowed_peers=[],max_requests=1))

    def test_gate_auth_route_and_budget(self):
        Provider.mode='success';Provider.requests=0
        server=ThreadingHTTPServer(('127.0.0.1',0),Provider)
        threading.Thread(target=server.serve_forever,daemon=True).start()
        try:
            cfg=dict(base_url=f'http://127.0.0.1:{server.server_port}/v1',model='fixture-model',_api_key='synthetic',max_tokens=512)
            with InferenceGate(cfg,'responses') as gate:
                body=dict(model='fixture-model',input=[{'role':'user','content':json.dumps({'events':[{'type':'say','text':'fixture'}]})}],tools=[{'type':'function','name':'shell'}],stream=True,store=True,max_output_tokens=999999)
                def post(path,token):
                    req=Request(gate.url+path,data=json.dumps(body).encode(),headers={'Content-Type':'application/json','Authorization':'Bearer '+token})
                    return urlopen(req,timeout=10).read()
                with self.assertRaises(HTTPError):post('/responses','wrong')
                with self.assertRaises(HTTPError):post('/other',gate.token)
                self.assertEqual(Provider.requests,0)
                self.assertIn(b'response.completed',post('/responses',gate.token))
                with self.assertRaises(HTTPError):post('/responses',gate.token)
                self.assertEqual(Provider.requests,1)
            self.assertFalse(gate.process.is_alive())
        finally:server.shutdown();server.server_close()

    def test_no_prompt_in_command_and_memory_db(self):
        gate=type('Gate',(),dict(url='http://127.0.0.1:1/v1',token='synthetic'))()
        with tempfile.TemporaryDirectory() as d:
            Path(d,'instructions.md').write_text('owner role')
            args,env=command(dict(runtime='opencode',runtime_binary='/opencode',model='fixture-model'),d,gate)
            self.assertEqual(env['OPENCODE_DB'],':memory:')
            self.assertNotIn('owner role',' '.join(args))
            self.assertTrue(Path(d,'data/opencode/log/opencode.log').is_symlink())

if __name__=='__main__':unittest.main()
