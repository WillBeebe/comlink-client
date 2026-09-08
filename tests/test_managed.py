import asyncio
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch
import sys

from comlink_adapter.managed import Receiver, Call, ManagedError, Event, dispatch, write_private, private_dir, number, socket_client
from comlink_adapter.onboarding import configure_host

PEER='b'*128

class ManagedTests(unittest.IsolatedAsyncioTestCase):
    def setUp(self):
        self.temp=tempfile.TemporaryDirectory();self.state=Path(self.temp.name).resolve()
        self.receiver=Receiver({'state':str(self.state)})
    def tearDown(self): self.temp.cleanup()
    async def test_install_and_registration_are_not_ready(self):
        self.receiver.ready.set();self.receiver.policy_synced=True
        self.assertFalse(self.receiver.status()['ready'])
    async def test_unknown_and_unapproved_contact_fail_before_egress(self):
        with self.assertRaises(ManagedError):await self.receiver.send('Jane','hello')
        self.receiver.contacts=[{'name':'Jane','number':PEER,'communication_approved':False}]
        with self.assertRaises(ManagedError):await self.receiver.send('Jane','hello')
    async def test_duplicate_names_are_ambiguous(self):
        self.receiver.contacts=[{'name':'Jane','number':x*128,'communication_approved':True} for x in 'bc']
        with self.assertRaises(ManagedError):self.receiver.resolve('Jane')
    async def test_no_raw_handset_or_peer_text_tools(self):
        result=await dispatch(self.receiver,{'jsonrpc':'2.0','id':1,'method':'tools/call','params':{'name':'say','arguments':{}}})
        self.assertTrue(result['result']['isError'])
    async def test_closed_tool_schema(self):
        result=await dispatch(self.receiver,{'jsonrpc':'2.0','id':1,'method':'tools/call','params':{'name':'comlink_send','arguments':{'to':'Jane','text':'hello','approved':True}}})
        self.assertTrue(result['result']['isError'])
    async def test_terminal_cancels_receiver(self):
        started=asyncio.Event();cancelled=asyncio.Event()
        async def worker():
            started.set()
            try:await asyncio.sleep(60)
            finally:cancelled.set()
        call=Call(PEER);call.task=asyncio.create_task(worker());self.receiver.calls['a']=call
        await started.wait();self.receiver.end('a');await cancelled.wait()
        self.assertEqual(self.receiver.calls,{})
    async def test_host_config_preserves_other_entries_and_is_idempotent(self):
        path=self.state/'opencode.json';path.write_text(json.dumps({'model':'keep','mcp':{'other':{'type':'remote','url':'https://example.com'}}}))
        configure_host('opencode',self.state,path);first=path.read_text()
        configure_host('opencode',self.state,path);self.assertEqual(path.read_text(),first)
        self.assertEqual(json.loads(first)['model'],'keep')
    async def test_host_conflict_does_not_overwrite(self):
        path=self.state/'config.toml';raw='model = "keep"\n[mcp_servers.comlink]\ncommand="other"\n';path.write_text(raw)
        with self.assertRaises(ManagedError):configure_host('codex',self.state,path)
        self.assertEqual(path.read_text(),raw)
    async def test_private_state_rejects_symlink(self):
        path=self.state/'linked';path.symlink_to(self.state,target_is_directory=True)
        with self.assertRaises(ManagedError):private_dir(path)
    async def test_durable_budget_survives_restart_and_missing_ledger_fails(self):
        config = self.state/'receiver.json'
        write_private(config, {'max_requests': 2})
        write_private(self.state/'receiver-budget.json', {'limit':2,'used':2})
        class Bridge:
            def __init__(self,cfg):self.config=cfg
            async def handle(self,*_):return []
        with patch('comlink_adapter.coding.CodingBridge', Bridge):
            cfg={'state':str(self.state),'receiver_config':str(config)}
            first=Receiver(cfg); second=Receiver(cfg)
            self.assertFalse(first.model_available());self.assertFalse(second.model_available())
            (self.state/'receiver-budget.json').unlink()
            with self.assertRaises(FileNotFoundError):Receiver(cfg)
            self.assertFalse((self.state/'receiver-budget.json').exists())
    async def test_mcp_cancellation_stops_inflight_local_operation(self):
        with tempfile.TemporaryDirectory(dir='/private/tmp',prefix='cl-cancel-') as d:
            state=Path(d)
            started=asyncio.Event();cancelled=asyncio.Event()
            class Endpoint:
                connections=0
                async def send(self,*args):
                    started.set()
                    try:await asyncio.sleep(60)
                    finally:cancelled.set()
            endpoint=Endpoint()
            server=await asyncio.start_unix_server(lambda r,w:socket_client(endpoint,r,w),path=state/'control.sock')
            (state/'control.sock').chmod(0o600)
            host=await asyncio.create_subprocess_exec(sys.executable,'-m','comlink_adapter.onboarding','mcp','--state',str(state),stdin=asyncio.subprocess.PIPE,stdout=asyncio.subprocess.PIPE,stderr=asyncio.subprocess.DEVNULL)
            async def send(message):
                host.stdin.write(json.dumps(message).encode()+b'\n');await host.stdin.drain()
            try:
                await send({'jsonrpc':'2.0','id':1,'method':'initialize','params':{}})
                self.assertIn('result',json.loads(await asyncio.wait_for(host.stdout.readline(),3)))
                await send({'jsonrpc':'2.0','id':2,'method':'tools/call','params':{'name':'comlink_send','arguments':{'to':'Jane','text':'private'}}})
                await asyncio.wait_for(started.wait(),3)
                await send({'jsonrpc':'2.0','method':'notifications/cancelled','params':{'requestId':2}})
                await asyncio.wait_for(cancelled.wait(),3)
            finally:
                host.stdin.close();await asyncio.wait_for(host.wait(),3)
                server.close();await server.wait_closed()
    async def test_connection_test_contact_cannot_receive_ordinary_messages(self):
        self.receiver.test_peer=PEER
        self.receiver.contacts=[{'name':'Comlink connection test','number':PEER,'communication_approved':True}]
        with self.assertRaisesRegex(ManagedError,'connection_tests_only'):
            await self.receiver.send('Comlink connection test','please do work')
    async def test_pause_is_atomic_and_refuses_active_calls(self):
        self.receiver.ready.set()
        self.receiver.calls['active']=Call(PEER)
        message={'jsonrpc':'2.0','id':1,'method':'tools/call','params':{'name':'comlink_pause','arguments':{}}}
        result=await dispatch(self.receiver,message)
        self.assertTrue(result['result']['isError']);self.assertFalse(self.receiver.maintenance)
        self.receiver.calls.clear()
        result=await dispatch(self.receiver,message)
        self.assertTrue(result['result']['structuredContent']['paused'])
        self.assertTrue(self.receiver.maintenance);self.assertFalse(self.receiver.ready.is_set())
    async def test_number_normalization(self):self.assertEqual(number('B'*64+'-'+'B'*64),PEER)
    async def test_invitation_contains_no_private_data(self):
        write_private(self.state/'managed-number.json',{'number':PEER})
        result=await dispatch(self.receiver,{'jsonrpc':'2.0','id':1,'method':'tools/call','params':{'name':'comlink_invitation','arguments':{}}})
        self.assertEqual(set(result['result']['structuredContent']),{'version','number','permission'})

if __name__=='__main__':unittest.main()
