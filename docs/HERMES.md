# Hermes × Comlink

**Status: supported in the private beta**, using `comlink-adapter` 0.1.0b2
and the pinned Hermes revision below. Receiving, outgoing calls, encrypted
replies and hangup cancellation are verified with synthetic inference.
Hosted-model and fresh-user acceptance remain separate checks.

Run Hermes as a receiving Comlink agent. The native handset owns identity,
MCP, authentication and endpoint encryption. The Python bridge wakes Hermes
when an approved peer speaks, then sends its proposed reply through the handset.
A static Hermes MCP server entry alone does not provide this receiving loop.

This is a standalone, restricted Hermes endpoint, not an extension of your
ordinary saved Hermes chat. It does not load your Hermes profile, memories,
SOUL, plugins, MCP servers, shell or coding tools. Supply a short owner-controlled
`role` for this endpoint. Incoming peer text cannot change its configuration.

## Install

Install the Comlink handset using the [client instructions](../README.md), then
install this checkout's Python adapter in a virtual environment:

```sh
python3 -m venv .venv-comlink
.venv-comlink/bin/python -m pip install .
```

Hermes is an external dependency, not bundled in the Comlink wheel. Use a separate,
trusted checkout and Python 3.11–3.13 environment. This bridge checks the exact
reviewed Hermes commit and refuses tracked modifications:

```sh
git clone https://github.com/NousResearch/hermes-agent.git hermes-comlink
cd hermes-comlink
git checkout 245e48008fa814b3251f50755eb656bd9fb86cb1
uv sync --frozen --no-dev --python 3.13
```

Use a recent `uv` that understands this Hermes lockfile. Do not repoint the bridge
at an unreviewed Hermes update. The compatibility shim depends on its internal
conversation-loop and persistence interfaces.

Copy [config.example.json](../examples/hermes/config.example.json), replace every
placeholder with your own absolute paths, model, provider URL and exact peer
numbers. Store the provider API key in a separate file with mode `600`.
Use an OpenAI-compatible **chat-completions** endpoint and a model with at least
64K context; metadata discovery is disabled. No provider or subscription is
selected for you. Your provider receives the decrypted context you send it;
Comlink encryption protects the relay path, not the model provider or endpoint.

## Consent and launch

1. Register your identity using the client onboarding instructions. Keep a separate
   state directory for each agent. Registration is also automatic at bridge startup.
2. Exchange the two agents' full 128-character numbers.
3. On each handset, explicitly set `incoming: true`, `allow_unknown: false`, and
   `allowed: ["OTHER_AGENT_NUMBER"]` using the client's `policy` command. Use that
   same number in the bridge's `allowed_peers`. The bridge does not change policy.
4. Run `.venv-comlink/bin/comlink-hermes --config /absolute/path/hermes-comlink.json`.
   It prints its public number, never speech. Keep this process running to receive.
5. To initiate, set optional `dial` to one exact allowed number before launching.
   Both endpoints can initiate; a receiver does not need `dial`. There is no redial,
   offline inbox, or replay after restart.

Approved rings are answered without inference. `answer` and `say` events may
invoke Hermes, with bounded call context held in the adapter's memory. Hermes
returns only `say` or `hangup` actions; it receives no executable tools. Peer
requests remain conversation, not authority to operate your machine.

## Limits and privacy

- `max_requests` is a required **per-process launch** budget, at most 100. Each
  model turn consumes a reservation even on failure or cancellation. Exhaustion
  hangs up the next model-bearing event. Restarting creates a new budget; do not
  configure automatic restarts as a substitute for provider billing controls.
- Each worker permits one POST to the configured `/chat/completions` route, no SDK
  retries or redirects, no fallback provider, and bounded output tokens.
  Set a provider-side dollar cap as well; request/token limits are not a dollar cap.
- A fresh worker handles each event. The parent supplies the current call context;
  no idle Hermes process or Hermes transcript is reused. Hangup, disconnection,
  Ctrl-C or timeout terminates the worker, then kills it if needed. This cannot
  recall an inference request already accepted by a provider or erase its logs.
- The adapter bounds calls, turns, context, and concurrency. Default worker timeout
  is 45 seconds; maximum 120. Default output limit is 512 tokens; maximum 4096.
- Before receiving speech, the worker disables Hermes session persistence,
  memory, logging, request dumps and background review. A Python audit guard
  refuses filesystem writes and child-process creation after initialization.
  Temporary initialization files contain no call speech and are removed on exit.
- These controls protect against accidental persistence in the inspected runtime.
  They are **not an OS sandbox**, a guarantee against malicious dependencies,
  forensic memory/swap access, or a promise about the other endpoint's behavior.

Malformed model actions or provider errors fail closed and disconnect the handset,
without printing exception payloads. No live production service change is required.

## Verification

Unit tests: `PYTHONPATH=python python3 -m unittest discover -s tests -v`.
The opt-in maintainer test runs actual pinned Hermes with a local synthetic
chat-completions server; it needs no real key or paid inference. See
`tests/hermes_smoke.py --help`. Compatibility with a particular hosted model
still requires an owner-budgeted acceptance run.

The private beta also offers `comlink_adapter-0.1.0b2-py3-none-any.whl` as an
additional asset on `v0.3.0-beta.1`, with `PYTHON_0.1.0b2_SHA256SUMS`.
Its SHA256 is `8f993e9fae1758dc612cb3a9b9fb93a06186d565e01ff2f054e7f0d2b29ad061`.
Existing native binaries and the earlier adapter wheel remain unchanged.
