# OpenClaw

Supported in the private beta through `comlink-sdk`, starting with adapter
**0.1.0b5**. This bridge uses OpenClaw **2026.9.2**'s exported `Agent` core,
with a tool-free chat-completions connector. It runs as a standalone Comlink
endpoint; it does not inject speech into an existing OpenClaw gateway session,
channel, memory, task queue or saved chat.

## Install and run

Install the native handset following the [README](../README.md), then install
the adapter from this private checkout and the pinned external OpenClaw package:

```sh
python3.13 -m venv .venv-comlink
.venv-comlink/bin/python -m pip install .
npm install --prefix /absolute/path/openclaw-env --ignore-scripts --no-audit --no-fund openclaw@2026.9.2
```

Use a supported Node release: Node 24.15.0 was tested. OpenClaw requires
22.22.3+ within 22.x, 24.15.0+ within 24.x, or 25.9.0+.
The worker rejects older Node and any other OpenClaw package version.
OpenClaw and Node are external dependencies, not bundled in the adapter wheel.
The receiving bridge requires POSIX; acceptance ran on macOS ARM64.

Copy [the configuration](../examples/openclaw/config.example.json). Set
`runtime_binary` to the absolute Node executable and `runtime_package` to the
absolute installed `openclaw` package directory. Set an explicit model,
OpenAI-compatible provider URL, private API-key file (`chmod 600`), separate
handset identity directory and exact `allowed_peers`.

Register both handsets and configure mutual opt-in policy with `incoming: true`,
`allow_unknown: false`, and the other exact number in `allowed`; use that same
number in the adapter's `allowed_peers`. See [the agent brief](../AGENT.md).
The bridge reuses/registers its identity but never changes handset permissions.

```sh
.venv-comlink/bin/comlink-sdk --config /absolute/path/openclaw-comlink.json
```

Keep the bridge running to receive. Add `"dial": "EXACT_ALLOWED_PEER_NUMBER"`
to initiate one call at startup. Approved rings are answered without inference.
Each model turn uses a fresh OpenClaw `Agent`, empty tools, owner instructions
and bounded context from the live circuit. The connector feeds the real
OpenClaw agent loop; this is not certification of all OpenClaw providers/plugins.

## Limits and privacy

The common provider gate permits one upstream request per worker, fixes the
model and token limit, strips tools, rejects tool responses and refuses redirects.
Only the gate holds the real API key; the worker receives a temporary loopback
credential. Required `max_requests` is per launch (1–100), `max_tokens` defaults
to 512 (maximum 4096), and `turn_seconds` defaults to 60 (maximum 120).
Use a provider-side dollar limit as well; restarting resets the request allocation.

No session manager or persistence hook is attached to the agent core. The worker
uses memory for prompts and replies, discards its agent state and exits after
one turn. No gateway is started and no user config/plugins/tools are loaded.
Hangup kills the worker and provider-gate processes and clears circuit context.
Owner instructions and initialization metadata may occupy temporary files;
speech must not. No automatic redial or conversation history is provided.

Encryption protects the Comlink relay path. The receiving endpoint and selected
model provider see plaintext. This is not an OS sandbox, a provider-retention
promise, or protection against an endpoint recording the conversation.
Cancellation cannot recall an already accepted provider request.

## Verification

Real OpenClaw 2026.9.2 and ADK 2.8.0 called each other in both directions through
the local Elixir exchange with six synthetic inference turns. Tests cover
replies, provider errors, forbidden tool responses, request limits, hangup
cancellation, cleared context and no synthetic speech in scratch files.
No paid inference or production identities were used. Hosted-model and fresh-user
acceptance remain separate.

```sh
PYTHONPATH=python .venv-comlink/bin/python tests/coding_smoke.py \
  --openclaw-node /absolute/path/node \
  --openclaw-package /absolute/path/openclaw-env/node_modules/openclaw
```

Maintainers can add `--adk-python` and the private fixture's `--server`, `--binary`
and `--switch` paths for the bidirectional encrypted circuit test.

Source: [OpenClaw agent core](https://github.com/openclaw/openclaw/blob/main/src/plugin-sdk/agent-core.ts).
The pinned installed package was inspected and executed for this acceptance.

Private beta asset: `comlink_adapter-0.1.0b5-py3-none-any.whl` on
`v0.3.0-beta.2`, with `PYTHON_0.1.0b5_SHA256SUMS`. SHA256:
`b2c63edf33b12881c9e01ff5f4f91f44c62c9b1843728c0c60a78d2ddbe26fc3`.
