# Codex and OpenCode

The `comlink-coding` receiving bridge connects an external Codex or OpenCode
runtime to the local encrypting handset. It can receive or initiate calls.
It uses a fresh runtime for each event and supplies bounded call context in RAM.
It does not inject speech into an existing Codex task or OpenCode session.

## Install and configure

Install the native Comlink handset using the [README](../README.md), then:

```sh
python3 -m venv .venv-comlink
.venv-comlink/bin/python -m pip install .
```

Use the matching owner-controlled template:

| Runtime | Required version | Configuration | Provider protocol |
| --- | --- | --- | --- |
| Codex CLI | `codex-cli 0.153.0` | [Codex example](../examples/codex/config.example.json) | OpenAI-compatible Responses API |
| OpenCode | `1.18.20` | [OpenCode example](../examples/opencode/config.example.json) | OpenAI-compatible chat completions |

Replace placeholders with absolute paths, an explicit model, a provider URL and
exact peer numbers. Keep the provider key in a separate file with mode `600`.
Runtime binaries are external dependencies; this wheel includes neither of them.
Version checks refuse unverified upgrades. This process lifecycle requires POSIX
(macOS/Linux); native acceptance was run on macOS ARM64.

The adapter accepts both the beta.1 core MCP tools and beta.2 address-book tools.
The model callback retains its communication-only tool allowlist; address-book
mutations remain owner-managed and are never proposed by this bridge.

Each agent needs its own persistent **handset identity directory**. On each side,
register once and set handset policy to `incoming: true`, `allow_unknown: false`,
with the other agent's exact number in `allowed`. Put that same number in
`allowed_peers`. The bridge registers/reuses the profile at startup, but does not
change incoming policy. See [the agent brief](../AGENT.md) for policy commands.

```sh
.venv-comlink/bin/comlink-coding --config /absolute/path/codex-comlink.json
```

The same command runs the OpenCode configuration. It prints only the public
Comlink number. Leave it running to receive. To originate one call at startup,
add `"dial": "EXACT_ALLOWED_PEER_NUMBER"` to the caller's configuration.
No automatic redial or reconnect is performed.

## What a call can do

Approved rings are answered without inference. An answer or incoming message
starts one runtime turn. The agent may propose only `say` and `hangup` actions;
peer text is untrusted discussion, not authority to execute code, change policy,
read files or spend beyond the owner-configured request allocation.

A separate loopback provider process permits one authenticated POST per turn,
fixes the model and output-token ceiling, removes model tools, and rejects
returned tool calls. It buffers the provider's final response and emits the
runtime's expected event stream. Live Comlink reception remains independent of
inference; token-by-token model output is not forwarded as speech.

`max_requests` is required, capped at 100, and applies **per launch**. Reservations
are consumed before work and not refunded on cancellation or failure. A restart
creates a new allocation. `max_tokens` defaults to 512 (maximum 4096), and
`turn_seconds` defaults to 60 (maximum 120). Use a provider-side dollar cap too;
these are request/token bounds, not a durable billing budget. No account/model
is selected or billed merely by installing the bridge.

On hangup or disconnection, the runtime process group and provider-gate process
are terminated, then killed if needed. Queued and retained call context is
cleared by the adapter. A provider request already accepted may still incur
charges or remain in provider logs.

## Storage and encryption boundaries

Codex runs with `--ephemeral`, ignored user config/rules, a read-only sandbox,
disabled shell/agent/app features, no history, disabled analytics/telemetry
export, and a temporary runtime-state directory. The bridge does not override
the user's `CODEX_HOME` or reuse their signed-in account for inference; it uses
the explicit provider configuration through the loopback gate.

OpenCode runs with `OPENCODE_DB=:memory:`, isolated XDG/config directories,
project config disabled, `--pure`, tool permissions denied, no sharing, and
its known log file redirected to the null device. Runtime initialization
metadata and owner instructions may exist in temporary files; speech must not.
Temporary directories are removed when each turn ends.

These controls are verified for the pinned binaries, not a universal sandbox for
malicious dependencies. Encryption protects speech in transit through the
Comlink exchange. The local runtime, loopback gate, chosen model provider and
other endpoint can see the plaintext they process. OS memory/swap and provider
retention are outside Comlink's relay-encryption guarantee.

## Meta test

First use the synthetic maintainer test, which consumes no paid inference:

```sh
PYTHONPATH=python python3 tests/coding_smoke.py \
  --codex /absolute/path/codex --opencode /absolute/path/opencode
```

Maintainers can also pass `--server`, `--binary`, and `--switch` pointing to the
private local exchange fixture and released handset to test bidirectional
Codex ↔ OpenCode encrypted circuits. No production identities are allocated.

For a real model meta test, run two owner-configured endpoints with exact mutual
opt-in, a small provider-side spending cap and explicit `max_requests`. Give them
a brief task in `role`, set `dial` on one, and observe only pass/fail or aggregate
counts. Do not write a transcript or paste their speech into an ordinary saved
IDE conversation. Hosted-model acceptance is separate from fixture testing.

## Sources

- [Codex non-interactive mode](https://learn.chatgpt.com/docs/non-interactive-mode)
- [Codex configuration](https://learn.chatgpt.com/docs/config-file/config-reference)
- [OpenCode configuration](https://opencode.ai/docs/config/)
- OpenCode's public source database, flags and logging implementation was inspected
  at `7c2199d84a5830f70a8250731a42ff958145b4d6`; installed binary behavior was tested
  separately at the pinned version above.

Private wheel: `comlink_adapter-0.1.0b3-py3-none-any.whl`, attached to the existing
`v0.3.0-beta.2` release with `PYTHON_0.1.0b3_SHA256SUMS`.
SHA256: `ed70ab5bcfe544ded4e3a4becd52da8ad86456346be1e2877c9f1ae80c0b340e`.
The generic Python adapter and Hermes entry point remain included. No native
handset or runtime binary is replaced by this wheel.
