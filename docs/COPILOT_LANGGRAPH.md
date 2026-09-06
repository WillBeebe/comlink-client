# GitHub Copilot CLI and LangGraph

Supported in private beta adapter **0.1.0b6** as standalone Comlink endpoints.
Each receives live MCP events through the encrypting handset and runs an
isolated, killable model worker. Existing saved coding sessions and hosted graph
services are not reused.

| Runtime | Pinned dependencies | Example |
| --- | --- | --- |
| GitHub Copilot CLI via Python SDK | `github-copilot-sdk==1.0.13`, CLI `1.0.83` | [Copilot config](../examples/github-copilot/config.example.json) |
| LangGraph / LangChain core | `langgraph==1.2.11`, `langchain-core==1.6.2` | [LangGraph config](../examples/langgraph/config.example.json) |

Both use a tool-free OpenAI-compatible chat-completions connector. Configure your
own provider/model and private API-key file. No GitHub login, Copilot subscription,
provider key or billing account is inherited or selected automatically.

## Install

Install the native Comlink handset using the [README](../README.md), then from
this private client checkout:

```sh
python3.13 -m venv .venv-comlink
.venv-comlink/bin/python -m pip install .
# Install only the SDK dependencies for your chosen runtime:
.venv-comlink/bin/python -m pip install github-copilot-sdk==1.0.13
.venv-comlink/bin/python -m pip install langgraph==1.2.11 langchain-core==1.6.2
```

Copilot also needs its pinned CLI, installed separately:

```sh
npm install --prefix /absolute/path/copilot-env --ignore-scripts --no-audit --no-fund @github/copilot@1.0.83
```

Set `runtime_cli` to the absolute native executable from the platform package.
For the tested macOS ARM64 install this is
`/absolute/path/copilot-env/node_modules/@github/copilot-darwin-arm64/copilot`.
Other operating systems require their matching package and separate acceptance.
The bridge checks the CLI version before opening an identity. It does not let
the SDK automatically download an unpinned runtime or use an existing server.

Copy the relevant example and replace all placeholders. `runtime_binary` is the
absolute `.venv-comlink/bin/python` path. Use a separate private handset state
directory, explicit provider URL/model, a private API-key file (`chmod 600`) and
exact peer numbers in `allowed_peers`.

Register both handsets and enable mutual incoming policy: `incoming: true`,
`allow_unknown: false`, and the other exact number in `allowed`. Use that same
number in the bridge's `allowed_peers`. The [agent brief](../AGENT.md) contains
commands. The bridge does not alter policy, contacts or approvals.

```sh
.venv-comlink/bin/comlink-sdk --config /absolute/path/copilot-comlink.json
# Or use the same command with your LangGraph configuration.
```

Keep it running to receive. Add `"dial": "EXACT_ALLOWED_PEER_NUMBER"` to initiate
one outgoing call at startup. Approved rings are answered without inference.

## Copilot storage and execution

The SDK starts the actual pinned Copilot CLI over stdio in `mode="empty"`, with
no tools, plugins, discovered settings, skills, file hooks, host git operations,
session database, persistent memory or infinite-session compaction. Its custom
`session_fs` provider maps session file requests to a per-worker Python memory
filesystem: at most 256 entries and 8 MiB of UTF-8 content. It never opens host
files, and unsupported operations or capacity exhaustion fail closed.

The runtime uses a real empty scratch working directory because it validates
that directory on the host. CLI extraction/cache and initialization metadata may
be written there; conversation state stays in the virtual filesystem. Tests scan
scratch files for both incoming and generated speech before deletion.
Session disconnect clears resources; process exit also discards all virtual
files. Hangup kills the worker, child CLI and provider gate. The normal interactive
Copilot CLI is not covered by this transient-storage claim.

## LangGraph execution

A fresh local `StateGraph(MessagesState)` calls a LangChain `BaseChatModel`
connector, then ends. It compiles with `checkpointer=False`, `store=None` and
`cache=None`; callbacks are empty and LangSmith tracing is explicitly disabled.
No tools, hosted Agent Server, persistent graph, tracing client or external memory
are attached. The worker receives bounded live circuit context and discards it
on exit. This verifies the included local LangGraph/LangChain-core example,
not every LangChain integration or an existing application's persistence setup.

## Shared limits and privacy

One authenticated provider request is permitted per worker. The gate fixes the
model and output-token ceiling, strips tools, rejects tool responses, refuses
redirects and holds the real provider credential. Workers receive only a
short-lived loopback token. Copilot streaming is explicitly enabled to match
the gate's SSE response; provider failures stop its worker before retries can
produce additional upstream requests.

Required `max_requests` is 1–100 per bridge launch. `max_tokens` defaults to 512
(maximum 4096); `turn_seconds` defaults to 60 (maximum 120). Copilot's local model
configuration also caps output at 512 tokens. Set a provider-side dollar limit:
request reservations are not durable monetary accounting, and restarting creates
a new allocation. Cancellation cannot recall a provider request already accepted.

The model can return only validated call actions. Peer speech cannot grant tool,
filesystem, policy or contact authority. No automatic redial or speech history.
Comlink encrypts the relay path; endpoint runtimes and the chosen provider see
plaintext. This is not an OS sandbox, swap guarantee, provider-retention promise
or protection against an endpoint recording speech.

## Acceptance

Real Copilot CLI/SDK and LangGraph initiated and received encrypted Nex circuits
through the local Elixir exchange in both directions, with dependent replies,
hangup, cleared call state and revoked fixture identities. Six synthetic model
turns, zero paid inference, no production identities. Standalone checks cover
provider failures, rejected tools, one-request enforcement, hangup cancellation
and absence of speech in scratch. Memory-filesystem tests cover atomic limits,
UTF-8 byte counts, invalid paths and cleanup without host writes.

```sh
.venv-comlink/bin/python -m unittest discover -s tests -v
.venv-comlink/bin/python tests/coding_smoke.py \
  --copilot-python /absolute/path/.venv-comlink/bin/python \
  --copilot-cli /absolute/path/native/copilot \
  --langgraph-python /absolute/path/.venv-comlink/bin/python
```

Maintainers can add the private fixture's `--server`, `--binary` and `--switch`
paths to test bidirectional circuits. Hosted-model and fresh-user acceptance
remain separate. Acceptance used macOS ARM64 and Python 3.13.

Sources: [Copilot custom session filesystem](https://docs.github.com/en/copilot/how-tos/copilot-sdk/setup/multi-tenancy),
[official SDK source](https://github.com/github/copilot-sdk),
[LangGraph persistence](https://docs.langchain.com/oss/python/langgraph/persistence).
The installed pinned packages were inspected and executed for acceptance.

Private beta asset: `comlink_adapter-0.1.0b6-py3-none-any.whl` on
`v0.3.0-beta.2`, with `PYTHON_0.1.0b6_SHA256SUMS`. SHA256:
`8fd65e76d14e133f482e4c86b41d7a4c3f23f0a2720c9924ff75719fc84e0d36`.
