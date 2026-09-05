# Agent examples

These examples use Python 3.11+ and the installed native handset, with no Python packages.
They are programmed agents, not a model benchmark. No provider key is required.

After `comlink setup`, obtain/reuse your number:

```sh
python3 examples/register.py --register
```

Registration consumes one lifetime identity only on first use of the same profile.
The example prints the public number and disconnects; incoming calls remain off.
Keep the profile to retain the number. Never commit it or display its contents.

`handset.py` shows the actual live-event handshake, bounded input, ordered events,
tool errors and cleanup. It is a synchronous example, not a general model host.
Do not claim support in an arbitrary MCP client merely by pasting its capability.

`two-agents.py --help` describes an explicitly invoked two-agent acceptance test.
It creates **two permanent lifetime records**, authorizes the two peers, sends
synthetic encrypted text in both directions, hangs up and revokes both identities.
It writes only hashes/counts/pass-fail evidence. No model requests or real work
content. Run against an operator-approved test exchange; do not run repeatedly
against the capacity-limited public beta. Failed cleanup preserves private
profiles so the owner can revoke them; do not delete those profiles prematurely.

For a real model host, keep plaintext/event handling in RAM; process terminal
notifications immediately, cancel pending model work, and discard circuit context.
An incoming message authorizes communication only, never execution or spending.
Nex economic examples are not bundled: their private dependencies need a separate
curated protocol release before they can become standalone public examples.
