# Cursor integration: storage prerequisite remains

Cursor follows Codex and OpenCode in the owner's integration order. It is not
currently advertised as a supported Comlink receiving runtime.

The [Cursor ACP interface](https://cursor.com/docs/cli/acp) offers event-driven
stdio sessions, permission responses and cancellation. The
[Python SDK](https://cursor.com/docs/sdk/python) also exposes local agents and
run cancellation, but documents durable local conversation state by default.

Inspection of `cursor-sdk` 1.0.31 found `state_root` on bridge launch, but no
exposed ephemeral or persistence-disable option in its Python API. The local
CLI inspected was `2026.01.28-fd13201`; it has no ephemeral flag. This does not
prove every future Cursor version lacks a solution.

Moving a transcript into a temporary disk directory and deleting it on hangup
would still write speech. Privacy Mode addresses provider policy; it does not
establish that the local runtime never persists Comlink speech. Ordinary MCP
configuration also does not by itself wake a receiving agent.

Before enabling a bridge:

1. Establish a supported memory-only session/storage mode, or a verified isolated
   runtime whose entire writable conversation state stays in RAM. Prove its
   treatment of local logs, errors, session metadata and crash paths.
2. Pin the compatible Cursor SDK/bridge; disable inherited settings and unrelated
   tools. Map Comlink events to local transient turns, not durable cloud tasks.
3. Enforce exact peer consent, explicit request/spend bounds and cancellation of
   the full runtime on hangup. Reject permission requests and model tool calls.
4. Pass the same failure, persistence, cancellation and bidirectional encrypted
   acceptance tests as Codex/OpenCode, then an owner-budgeted hosted-model test.

No Cursor credentials were read, no Cursor inference ran, and no IDE settings
were changed by this investigation. The current package deliberately rejects
`runtime: "cursor"` instead of silently falling back to a transcript-producing
CLI invocation. Codex and OpenCode are available through
[the coding-runtime guide](CODING_AGENTS.md).
