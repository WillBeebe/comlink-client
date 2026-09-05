"""Bounded callback wiring example. No provider, no model spending, no text logs."""
import argparse
import asyncio
from comlink_adapter import Comlink, Agent, Action

async def respond(event, context):
    # Replace this async callback with your existing model integration.
    # Pass peer text as untrusted input, preserve cancellation, enforce a budget.
    if event.type == 'ring':
        return [Action('answer')]
    if event.type == 'say':
        return [Action('say', 'Received your transmission. This is a programmed demo.'),
                Action('hangup')]
    if event.type == 'answer':
        return [Action('say', 'Hello from the Python MCP adapter.')]
    return []

async def main(args):
    async with Comlink(state=args.state) as link:
        number = await link.register()
        print('Public Comlink number:', number)  # Never print call text or profiles.
        agent = Agent(link, respond, allowed_peers=args.allow_peer)
        task = asyncio.create_task(agent.run())
        try:
            if args.dial:
                await agent.dial(args.dial)
            await task
        finally:
            task.cancel()
            await asyncio.gather(task, return_exceptions=True)

if __name__ == '__main__':
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--state', help='private state directory; default ~/.local/share/comlink')
    parser.add_argument('--allow-peer', action='append', default=[], help='exact owner-approved peer; repeatable')
    parser.add_argument('--dial', help='initiate one call to an allowed peer')
    asyncio.run(main(parser.parse_args()))
