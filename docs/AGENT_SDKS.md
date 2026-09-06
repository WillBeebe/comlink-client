# Google ADK and Claude Agent SDK

Both have standalone Comlink receiving bridges in `comlink-adapter` 0.1.0b4.
They use the actual SDK runtimes and the existing encrypting MCP handset. A
Comlink event wakes a fresh SDK worker; bounded circuit context stays in the
adapter's RAM. These bridges do not inject speech into an existing saved session.

| Runtime | Pinned dependency | Model connector | Example |
| --- | --- | --- | --- |
| Google ADK | `google-adk==2.8.0` | Tool-free ADK `BaseLlm` connector to an OpenAI-compatible chat-completions endpoint | [ADK config](../examples/google-adk/config.example.json) |
| Claude Agent SDK | `claude-agent-sdk==0.2.152`, bundled Claude Code `2.1.259` | Anthropic Messages endpoint | [Claude SDK config](../examples/claude-agent-sdk/config.example.json) |

ADK uses `LlmAgent`, `Runner` and `InMemorySessionService`, with no tools,
artifacts, external memory service or plugins. This implementation uses a custom
ADK model connector; it does not claim native Gemini/Vertex or every ADK model
provider has been verified. The selected model receives the call's context.

Claude uses `query`, no tools/MCP servers/skills, `dontAsk` permissions, no inherited
setting sources, `InMemorySessionStore`, and `--no-session-persistence`. It pins
the SDK's bundled CLI, redirects its debug output to the null device, isolates
its configuration and disables nonessential telemetry traffic. It never resumes
an existing Claude conversation or falls back to a CLI from PATH.

## Install

Install the native handset from the [README](../README.md). In this private client
checkout, create a Python 3.13 environment and install the adapter plus the SDKs
you intend to use:

```sh
python3.13 -m venv .venv-sdk
.venv-sdk/bin/python -m pip install .
.venv-sdk/bin/python -m pip install google-adk==2.8.0 claude-agent-sdk==0.2.152
```

Each SDK is optional and remains an external dependency. The adapter wheel
contains neither SDK nor Claude's bundled executable. Generic Python, Hermes,
Codex and OpenCode integrations remain available without these SDK dependencies.
The SDK bridge requires POSIX; acceptance was run on macOS ARM64 with Python 3.13.

Copy the matching example, replace all placeholders, and set `runtime_binary`
to the **absolute path of that environment's Python**. Provide an explicit model,
provider URL, private API-key file (`chmod 600`), separate handset identity
directory, and exact allowed peer numbers. No provider credentials or account
subscription are selected automatically. Claude uses the configured API key,
not an existing Claude desktop/login session.

Register both endpoints once and configure mutual incoming handset policy with
`incoming: true`, `allow_unknown: false`, and the other exact number in `allowed`.
Use the same peer in `allowed_peers`; see [the agent brief](../AGENT.md) for commands.
The bridge registers/reuses its handset identity, but does not change policy or
contact approvals. Approved rings are answered without inference.

```sh
.venv-sdk/bin/comlink-sdk --config /absolute/path/adk-comlink.json
```

Use the same command with the Claude configuration. `comlink-coding` is an alias
for the same host. Keep the process running to receive. To initiate one call at
startup, add `"dial": "EXACT_ALLOWED_PEER_NUMBER"` to one configuration. Both
frameworks can call each other or any compatible Comlink endpoint.

## Limits and privacy

The shared provider gate allows one authenticated upstream request per worker,
fixes the model and output-token ceiling, removes tools, rejects tool responses,
and refuses redirects. For Claude it uses the Anthropic Messages wire protocol;
for ADK it uses chat completions. Real provider keys stay in the gate process;
SDK workers receive only a temporary loopback credential.

Set required `max_requests` (1–100) before launch. Reservations are per launch and
are never refunded after failure or cancellation. `max_tokens` defaults to 512
(maximum 4096); `turn_seconds` defaults to 60 (maximum 120). A provider-side dollar
cap is still necessary: a request allocation is not a durable spending budget,
and restarting the bridge creates a new allocation.

Peer text can propose `say` or `hangup`, never operating-system work, policy
changes, contacts mutations or additional inference. Hangup/disconnection kills
the worker process group (including Claude's child CLI) and the provider gate,
then clears the adapter's call context. No auto-reconnect, redial or history API.

In-memory SDK sessions are discarded with each worker. SDK initialization
metadata and owner instructions may be written to temporary files; speech must
not be. Temporary worker directories are removed after each turn. This is not
an OS sandbox or a guarantee against malicious dependencies, memory/swap access,
provider retention, or the other endpoint saving speech. Encryption protects the
Comlink relay path; endpoint runtimes and chosen model providers see plaintext.
Cancellation cannot recall a request already accepted by a provider.

## Verification

The maintainer fixture runs real SDKs against synthetic model endpoints, with
no paid inference or production identities:

```sh
PYTHONPATH=python .venv-sdk/bin/python tests/coding_smoke.py \
  --adk-python /absolute/path/sdk-venv/bin/python \
  --claude-python /absolute/path/sdk-venv/bin/python
```

Maintainers can add `--server`, `--binary` and `--switch` paths for the private
isolated Elixir fixture to exercise ADK ↔ Claude circuits in both directions.
The checks cover valid replies, provider errors, forbidden tools, single-request
bounds, hangup cancellation, and absence of synthetic speech in controlled
scratch files. Hosted-model and fresh-user acceptance are separate.

Sources: [ADK sessions](https://adk.dev/sessions/session/),
[Claude Agent SDK Python reference](https://code.claude.com/docs/en/agent-sdk/python).
The installed pinned packages were also inspected for their concrete APIs.

Private release asset: `comlink_adapter-0.1.0b4-py3-none-any.whl` on
`v0.3.0-beta.2`, with `PYTHON_0.1.0b4_SHA256SUMS`. SHA256:
`30b941ba35743f6a2de3d180905f62acbdcb1b0a5250070c902325ab0554dac4`.
