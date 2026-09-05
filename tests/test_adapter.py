import asyncio
import unittest
from comlink_adapter import Comlink, Agent, Action, Event, ComlinkError

CALL='a'*64
PEER='b'*128
class FakeLink:
    def __init__(self):
        self.events=asyncio.Queue();self.number='c'*128;self.on_terminal=None
        self.closed=False;self.sent=[]
    async def register(self): return self.number
    async def tool(self,name,**args):
        self.sent.append((name,args));return {'sent':True,'ok':True}
    def _stop(self):
        if not self.closed:
            self.closed=True
            if self.on_terminal:self.on_terminal(Event('disconnected'))
            self.events.put_nowait(Event('disconnected'))
    async def close(self):self._stop()
    def event(self,event):
        if event.type in {'hangup','closed','disconnected','reject'} and self.on_terminal:self.on_terminal(event)
        self.events.put_nowait(event)

class AdapterTests(unittest.IsolatedAsyncioTestCase):
    async def test_reader_events_arrive_before_tool_response(self):
        c=Comlink();c.process=type('P',(),{'returncode':0})()
        c._event({'version':1,'seq':1,'type':'ring','call':CALL,'from':PEER})
        self.assertEqual((await c.events.get()).type,'ring')
        with self.assertRaises(ComlinkError):c._event({'version':1,'seq':1,'type':'say','call':CALL,'text':'private'})
        c._stop()
    async def test_ring_wakes_callback_and_hangup_cancels_it(self):
        link=FakeLink();started=asyncio.Event();cancelled=asyncio.Event()
        async def callback(event,context):
            started.set()
            try:await asyncio.sleep(60)
            finally:cancelled.set()
            return []
        agent=Agent(link,callback,allowed_peers=[PEER])
        task=asyncio.create_task(agent.run());await asyncio.sleep(0)
        link.event(Event('ring',CALL,PEER))
        await asyncio.wait_for(started.wait(),1)
        link.event(Event('hangup',CALL,PEER))
        await asyncio.wait_for(cancelled.wait(),1)
        self.assertEqual(agent.calls,{})
        link._stop();await task
        self.assertEqual(link.sent,[])
    async def test_unknown_peer_never_invokes_model(self):
        link=FakeLink();calls=[]
        async def callback(*args):calls.append(args);return []
        agent=Agent(link,callback,allowed_peers=[])
        task=asyncio.create_task(agent.run());await asyncio.sleep(0)
        link.event(Event('ring',CALL,PEER));await asyncio.sleep(.01)
        self.assertEqual(link.sent[0][0],'reject');self.assertEqual(calls,[])
        link._stop();await task
    async def test_model_cannot_invoke_unrelated_tool(self):
        link=FakeLink()
        async def callback(*_):return [Action('shell','do something')]
        agent=Agent(link,callback,allowed_peers=[PEER])
        task=asyncio.create_task(agent.run());await asyncio.sleep(0)
        link.event(Event('ring',CALL,PEER))
        with self.assertRaises(ComlinkError):await asyncio.wait_for(task,1)
        self.assertTrue(link.closed);self.assertEqual(link.sent,[])
    async def test_terminal_queued_before_dispatch_prevents_stale_callback(self):
        link=FakeLink();seen=[]
        async def callback(*_):seen.append(1);return []
        agent=Agent(link,callback,allowed_peers=[PEER])
        task=asyncio.create_task(agent.run());await asyncio.sleep(0)
        link.event(Event('ring',CALL,PEER));link.event(Event('hangup',CALL,PEER))
        await asyncio.sleep(.01);self.assertEqual(seen,[])
        link._stop();await task
    async def test_context_limit_fails_closed(self):
        link=FakeLink()
        async def callback(*_):return []
        agent=Agent(link,callback,allowed_peers=[PEER],max_context_bytes=1)
        task=asyncio.create_task(agent.run());await asyncio.sleep(0)
        link.event(Event('ring',CALL,PEER));link.event(Event('say',CALL,PEER,'too long'))
        with self.assertRaises(ComlinkError):await asyncio.wait_for(task,1)
        self.assertFalse(agent.calls)
