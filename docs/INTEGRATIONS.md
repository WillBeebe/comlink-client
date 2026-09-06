# Cross-runtime Comlink integrations

Integration roadmap, checked against official documentation 2026-09-05.
Hermes now has an [implemented receiving bridge](HERMES.md), tested at its pinned
revision using a synthetic provider and bidirectional encrypted Elixir circuits.
The Python adapter has installed-package local acceptance; the Go starter is
build/unit-tested. Other entries remain proposals, not verified compatibility.
No paid provider or hosted-agent subscription was configured.

The common interface is a local encrypting handset plus an event-aware runtime.
Each runtime owns its private identity, exact peer permissions and model budget.
MCP tool support is necessary but does not prove handling of Comlink's custom
notifications or waking a dormant agent. Each bridge must prove receiving too.

| Platform | Documented integration surface | Proposed Comlink example and remaining verification |
| --- | --- | --- |
| Google ADK | [MCP toolsets](https://github.com/google/adk-docs/blob/main/docs/tools-custom/mcp-tools.md) | Embed the Python adapter and use a cancellable callback to run a bounded ADK turn. Prove turn interruption and use transient session/state services; do not silently save call text in a durable ADK session. |
| Claude Agent SDK | [MCP integration](https://code.claude.com/docs/en/agent-sdk/mcp) | A Python host owns the adapter and invokes a restricted SDK turn for each event. Validate cancellation and transcript/session persistence before claiming transient calls. Use explicit Comlink-only action results rather than granting the full coding toolset. |
| Hermes Agent | [MCP integration](https://hermes-agent.nousresearch.com/docs/user-guide/features/mcp) | [Standalone bridge](HERMES.md) implemented: live reception, isolated transient workers, bounded requests and hangup cancellation. Pinned Hermes passed synthetic-provider circuit tests; fresh-user hosted-model acceptance remains. The ordinary MCP server entry alone is not a receiving bridge. |
| OpenClaw | [MCP configuration](https://docs.openclaw.ai/cli/mcp) | Add a runtime/gateway event bridge with transient call state and exact peer policy. Prove lifecycle behavior without converting events into durable chat/task messages. |
| OpenCode | [SDK](https://opencode.ai/docs/sdk/) and [MCP servers](https://opencode.ai/docs/mcp-servers/) | Explore a plugin or external host bridge using the supported SDK. Its application event stream is not automatically Comlink's MCP event stream. Prove cancellation and review session persistence before using it for ephemeral call text. |
| Cursor | [MCP integration](https://docs.cursor.com/context/model-context-protocol) | Investigate a supported host/extension bridge that can trigger a receiving agent. Do not advertise background reception from a static MCP config or inject peer text into an ordinary persistent coding conversation. |
| Grok Build | [Official overview](https://docs.x.ai/build/overview) | Identify and pin its supported extension/host interface, then implement event wakeup and transient cancellation. Ordinary model API access or a Grok model test is not evidence of Grok Build integration. |

## Recommended sequence

Start with Google ADK and Claude Agent SDK because the Python callback adapter
can supply the integration boundary; this is an engineering judgment, not a
compatibility certification. Then Hermes and a Go-native agent; follow with
OpenClaw/OpenCode bridges and the more constrained IDE/CLI integrations once their
receiving hooks are demonstrated. Do not require a public protocol rewrite to
begin these examples.

Use one shared cross-runtime acceptance scenario: agent A asks agent B to review
synthetic task facts; B returns a dependent finding; A acknowledges it and hangs
up; B later initiates a fresh circuit. Verify explicit permissions, tool/spend
isolation, no automatic redial, revocation and cancellation. Add a malicious peer
instruction to prove that communication does not become owner authority.

For a civilization example, keep live discussion in Comlink and model separately
accepted work/commitments in the appropriate Nex workflow. Show the boundary
between a proposal, acceptance and independently verified completion. An agent's
familiar role or name does not itself establish authority.
