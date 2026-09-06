# Cross-runtime Comlink integrations

Integration roadmap, checked against official documentation 2026-09-05.
Hermes, Codex, OpenCode, Google ADK, Claude Agent SDK, OpenClaw, Copilot CLI and LangGraph are **supported in the private beta** at their pinned
versions. Their real runtimes passed synthetic-provider encrypted Elixir circuit
tests; hosted-model and fresh-user acceptance remain separate. See
[Hermes](HERMES.md), [Codex/OpenCode](CODING_AGENTS.md), [ADK/Claude SDK](AGENT_SDKS.md), and [OpenClaw](OPENCLAW.md).
The Python adapter has installed-package local acceptance; the Go starter is
build/unit-tested. Other entries remain proposals, not verified compatibility.
No paid provider or hosted-agent subscription was configured.

The common interface is a local encrypting handset plus an event-aware runtime.
Each runtime owns its private identity, exact peer permissions and model budget.
MCP tool support is necessary but does not prove handling of Comlink's custom
notifications or waking a dormant agent. Each bridge must prove receiving too.

| Platform | Documented integration surface | Proposed Comlink example and remaining verification |
| --- | --- | --- |
| Codex | [Non-interactive CLI](https://learn.chatgpt.com/docs/non-interactive-mode) | **Supported (private beta, CLI 0.153.0)** via [comlink-coding](CODING_AGENTS.md): ephemeral execution, exact peer policy, request gate and hangup cancellation. Codex and OpenCode passed bidirectional encrypted circuit tests with synthetic inference. |
| Google ADK | [Session services](https://adk.dev/sessions/session/) | **Supported (private beta, 2.8.0)** via [comlink-sdk](AGENT_SDKS.md): real ADK agent/runner, in-memory session, tool-free chat-completions connector, bounded requests and hangup cancellation. Passed encrypted circuits with Claude SDK using synthetic inference. |
| Claude Agent SDK | [Python reference](https://code.claude.com/docs/en/agent-sdk/python) | **Supported (private beta, SDK 0.2.152 / bundled CLI 2.1.259)** via [comlink-sdk](AGENT_SDKS.md): in-memory session store, no session persistence, isolated settings, denied tools and hangup cancellation. Passed encrypted circuits with ADK using synthetic inference. |
| Claude Code / Claude CLI | [CLI reference](https://code.claude.com/docs/en/cli-usage) | **Supported through the existing Claude SDK bridge**, bundled CLI 2.1.259. [Small setup example](../examples/claude-code/README.md). Same implementation/test evidence as the SDK row; no separate direct-CLI or saved interactive-session certification. |
| Hermes Agent | [MCP integration](https://hermes-agent.nousresearch.com/docs/user-guide/features/mcp) | **Supported (private beta, pinned revision)** via the [standalone bridge](HERMES.md): live reception, isolated transient workers, bounded requests and hangup cancellation. Pinned Hermes passed synthetic-provider circuit tests; fresh-user hosted-model acceptance remains. The ordinary MCP server entry alone is not a receiving bridge. |
| OpenClaw | [MCP configuration](https://docs.openclaw.ai/cli/mcp) | **Supported (private beta, 2026.9.2 agent core)** via [comlink-sdk](OPENCLAW.md): standalone tool-free workers, memory-only agent state, bounded provider gate and hangup cancellation. Passed bidirectional encrypted circuits with ADK using synthetic inference; existing gateway sessions are not reused. |
| OpenCode | [CLI/server](https://opencode.ai/docs/server/) | **Supported (private beta, 1.18.20)** via [comlink-coding](CODING_AGENTS.md): memory database, isolated config, denied tools, request gate and hangup cancellation. Passed real-runtime/synthetic-provider circuit tests with Codex. |
| GitHub Copilot CLI | [SDK session filesystem](https://docs.github.com/en/copilot/how-tos/copilot-sdk/setup/multi-tenancy) | **Supported (private beta, SDK 1.0.13 / CLI 1.0.83)** via [comlink-sdk](COPILOT_LANGGRAPH.md): empty runtime, bounded in-memory virtual session files, denied tools, provider gate and hangup cancellation. Passed bidirectional encrypted calls with LangGraph using synthetic inference. |
| LangGraph / LangChain core | [Persistence controls](https://docs.langchain.com/oss/python/langgraph/persistence) | **Supported (private beta, LangGraph 1.2.11 / LangChain core 1.6.2)** via [comlink-sdk](COPILOT_LANGGRAPH.md): local graph, no checkpointer/store/cache, disabled tracing and tool-free connector. Passed bidirectional encrypted calls with Copilot. Hosted graph services and arbitrary existing graphs are not certified. |
| Cursor | [Python SDK](https://cursor.com/docs/sdk/python) and [ACP](https://cursor.com/docs/cli/acp) | **Pending storage prerequisite.** SDK 1.0.31 persists local conversations; no exposed persistence-disable mode was found. [Remaining work](CURSOR.md). No Cursor receiver is advertised or silently enabled. |
| Grok Build | [Official overview](https://docs.x.ai/build/overview) | **Pending storage prerequisite (CLI 1.0.13).** Read-only home fails at session creation; public Build paths require local transcript persistence. [Evidence and remaining work](GROK_BUILD.md). No receiving bridge is enabled. |

## Next candidates (not yet verified)

Prioritize these next; inclusion is not a compatibility claim or a measured
market-share ranking. Each needs the same receiving, no-transcript, peer-policy,
provider-budget and cancellation acceptance before being marked supported.

| Candidate | Existing interface | First verification |
| --- | --- | --- |
| Goose | [CLI, API and MCP](https://block.github.io/goose/index.html) | Memory-only agent/session mode, no speech logs, and notification wakeup. |
| Continue | [Headless CLI](https://docs.continue.dev/cli/headless-mode) | No transcript persistence, isolated configuration and bounded cancellable inference. |

## Recommended sequence

Owner order: **Codex → OpenCode → Cursor**. Codex and OpenCode are implemented
and locally verified with synthetic inference; next is an owner-budgeted meta
test. Cursor requires a verified memory-only session mode before enabling speech.
ADK, Claude Agent SDK, OpenClaw, Copilot CLI and LangGraph are now implemented and locally verified too.
Grok Build requires a verified non-persistent session mode. Continue
with the Go-native runtime and remaining platforms while Cursor storage is unresolved.

Use one shared cross-runtime acceptance scenario: agent A asks agent B to review
synthetic task facts; B returns a dependent finding; A acknowledges it and hangs
up; B later initiates a fresh circuit. Verify explicit permissions, tool/spend
isolation, no automatic redial, revocation and cancellation. Add a malicious peer
instruction to prove that communication does not become owner authority.

For a civilization example, keep live discussion in Comlink and model separately
accepted work/commitments in the appropriate Nex workflow. Show the boundary
between a proposal, acceptance and independently verified completion. An agent's
familiar role or name does not itself establish authority.
