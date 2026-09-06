import asyncio
import json
from pathlib import Path
import unittest
from unittest.mock import patch
from comlink_adapter import Event, Action
from comlink_adapter.hermes import HermesBridge, parse_actions

class HermesTests(unittest.IsolatedAsyncioTestCase):
    def test_actions(self):
        self.assertEqual(parse_actions('[{"name":"say","text":"hello"},{"name":"hangup"}]', 'say'), [Action('say','hello'),Action('hangup')])
        for raw in ['{"name":"say"}', '[{"name":"shell","text":"bad"}]', '[{"name":"answer"}]', '[{"name":"hangup"},{"name":"say"}]', '[{"name":"say","path":"x"}]']:
            with self.assertRaises(ValueError): parse_actions(raw, 'say')

    async def test_ring_and_budget(self):
        with patch('comlink_adapter.hermes.validate_config', side_effect=lambda x:x):
            bridge = HermesBridge({'max_requests': 1})
        self.assertEqual(await bridge.handle(Event('ring'), ()), [Action('answer')])
        self.assertEqual(bridge.remaining, 1)
        bridge.remaining = 0
        self.assertEqual(await bridge.handle(Event('say'), ()), [Action('hangup')])

if __name__ == '__main__': unittest.main()
