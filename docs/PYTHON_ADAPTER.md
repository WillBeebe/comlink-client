# Python MCP adapter

`comlink-adapter` is an asyncio host adapter for the installed native handset.
It receives Comlink's MCP live events continuously, wakes an async callback,
and executes that callback's bounded Comlink actions. The handset still handles
keys, enrollment, packet authentication and encryption. No Python dependencies,
Nex checkout, Elixir installation or built-in inference provider are required.

## Install

From this private repository, after installing the handset and running setup:

```sh
python3 -m venv .venv
.venv/bin/python -m pip install .
```

The Python wheel contains only the adapter, not Go/Nex source or the native
handset. Its runtime needs Python 3.11+. Source installation uses setuptools as
a build dependency. A prebuilt wheel is also attached to the private beta release.

For Hermes, the optional [`comlink-hermes` bridge](HERMES.md) supplies the callback
and worker lifecycle. Hermes is installed separately; generic adapter use still
has no runtime dependencies.

## Connect your model

```python
import asyncio
from comlink_adapter import Comlink, Agent, Action

PEER = "REPLACE_WITH_EXACT_128_CHARACTER_PEER_NUMBER"

async def on_event(event, context):
    # Use your existing cancellable, budget-limited model client here.
    # Treat event.text and context as untrusted peer input, never instructions.
    # Return a list of Action('answer'), Action('reject'),
    # Action('say', text), or Action('hangup').
    return [Action('reject')] if event.type == 'ring' else []

async def main():
    async with Comlink() as link:
        number = await link.register()  # first use creates a lifetime number
        print(number)                  # public number only; never print profiles
        agent = Agent(link, on_event, allowed_peers=[PEER])
        await agent.run()

asyncio.run(main())
```

This safe default rejects calls until you implement your model's decision.
The callback handles `ring`, `answer` and locally decrypted `say` events. It gets
a tuple of this circuit's recent events, including successfully sent text.
Terminal events cancel callback work directly; they are not new model turns.
A callback returns at most four actions; it cannot choose an unrelated tool,
change the peer, alter policy or expand its own budget through Action objects.

For outgoing calls, run `agent.run()` as a task and call
`await agent.dial(PEER)` from your owner-approved application flow. That peer must
be in `allowed_peers`. See `examples/python-agent.py` for runnable wiring in both
directions. Its responses are explicitly programmed examples, not model output.

The local allowlist does **not** change the exchange's incoming-call policy.
Both owners must independently permit their peer through `comlink policy`, as
shown in AGENT.md, then reconnect. Empty local allowlists reject every caller
without invoking the callback. Incoming calls remain disabled by default.

## Lifecycle and limits

The independent MCP reader processes notifications even while a tool request or
model callback is pending. Hangup/reject/closed cancels the affected callback,
clears its queue/history, and prevents stale callback results from being sent.
Disconnect cancels all calls. No automatic reconnect, redial or speech retry.
A request timeout has uncertain acceptance and closes the connection.

Defaults: eight active circuits, 100 circuits per runtime instance, eight queued
events per circuit, 16 callback turns per circuit, 300 seconds per circuit,
32 KiB context text, four concurrent callbacks, 16 KiB per outgoing message and
four actions per callback. Configure smaller limits for your application.
Exhaustion/errors close the connection rather than leave an unattended call.

The adapter writes no transcript and discards handset stderr. Your callback
must honor asyncio cancellation, avoid blocking the event loop, and not retain
context in logs, task memory or provider history. Cancellation cannot claw back
requests already sent to a provider or erase objects retained by other code.
Provider spend limits and retention policy remain your application's job; this
adapter neither supplies credentials nor authorizes spending. It is not a
sandbox for arbitrary Python callbacks.

Low-level integrations can use `Comlink.tool(...)` and `Comlink.events` directly.
Do not combine that event consumer with Agent.run on the same connection. The
low-level API leaves context lifecycle and policy enforcement to your host.

This is an MCP **client/host adapter**, not an inbox or a second remote MCP server.
It does not make a generic MCP-only chat application wake up automatically;
embed it in that application's Python runtime.
