# Claude Code / Claude CLI

This example runs the actual Claude Code executable through the existing verified
Claude Agent SDK bridge. It shares that implementation and acceptance evidence;
it is not a second independent runtime or a plugin for an existing saved chat.

Pinned: `claude-agent-sdk==0.2.152`, bundled Claude Code `2.1.259`.
The worker uses the SDK's bundled executable, not whichever `claude` is on PATH.

## Start a receiving endpoint

Install the native handset using the repository README, then from this private
client checkout:

```sh
python3.13 -m venv .venv-claude
.venv-claude/bin/python -m pip install . claude-agent-sdk==0.2.152
```

Copy `config.example.json` to a private location. Replace every placeholder.
`runtime_binary` is the absolute `.venv-claude/bin/python` path; `runtime` remains
`claude-sdk`. Set the model, Anthropic Messages endpoint, private API-key file
(`chmod 600`), separate handset state directory and exact allowed peer numbers.
The API key belongs to the selected provider; existing Claude subscription/login
credentials are not used automatically.

Register both endpoints and enable mutual handset policy with `incoming: true`,
`allow_unknown: false` and the exact peer in `allowed`. Match this with each
bridge's `allowed_peers`. See [the agent brief](../../AGENT.md) for commands.
The bridge does not alter policy or contacts.

```sh
.venv-claude/bin/comlink-sdk --config /absolute/path/claude-code-comlink.json
```

Leave it running to receive. Set `"dial": "EXACT_ALLOWED_PEER_NUMBER"` to place
one outgoing call at startup. Approved rings are answered without model spend.

Each turn starts isolated Claude Code with in-memory SDK session state,
`--no-session-persistence`, no tools/MCP servers/skills, empty inherited settings,
`dontAsk` permissions and disabled debug output. A bounded provider gate holds
the real credential; the runtime receives only a temporary loopback token.
Hangup kills the runtime and gate, then clears live context.

`max_requests` is per launch, not a durable dollar cap. Also set a provider-side
budget. Endpoint runtimes and the selected model provider see plaintext;
Comlink's relay cannot decrypt it. Cancellation cannot recall a provider request.

## Why not just add an MCP server to Claude?

MCP exposes tools, but a tool entry alone does not prove that a dormant agent
will receive and handle Comlink notifications. Ordinary interactive Claude Code
sessions also save transcripts by default. This standalone bridge supplies the
receiving loop and transient execution settings together. Do not feed call
speech into a normal saved Claude session and claim this privacy behavior.

The actual bundled Claude executable passed encrypted bidirectional calls with
ADK using synthetic inference, plus failure, tool-rejection and hangup checks.
This example reuses those tests. Other CLI versions, direct standalone CLI
launches, hosted-model runs and fresh-user installs remain unverified.

Full limits and test commands: [Claude Agent SDK guide](../../docs/AGENT_SDKS.md).
Official references: [CLI flags](https://code.claude.com/docs/en/cli-usage),
[session persistence](https://code.claude.com/docs/en/sessions).
