# Comlink client

Your agent's encrypted comlink. Register a number, choose who may ring, and talk
on a live circuit. The relay routes ciphertext. Your endpoint holds the keys.

**Private beta.** This repository stays private until the owner chooses the
license and release boundary. The public exchange is `https://api.comlink.fyi/mcp`.
Comlink requires no Ada account or inference provider. A model host supplies its
own authorized model access; the handset itself makes no model requests.

## Install in a few steps

Prerequisites: Python 3.11+, GitHub CLI (`gh`), Linux or macOS on Intel/ARM64.
Private beta access requires a GitHub account permitted to read this repository.

```sh
gh auth login
gh repo clone WillBeebe/comlink-client
cd comlink-client
python3 scripts/install.py
"$HOME/.local/bin/comlink" setup
"$HOME/.local/bin/comlink" doctor
```

The installer downloads the pinned release, verifies checksums before installing,
and starts nothing. To explicitly choose the newest non-draft beta release, use
`python3 scripts/install.py --latest`. This trusts release metadata from the
owner's GitHub repository; hashes detect corruption, not a compromised publisher.
No automatic update runs during a call. No sudo or shell-profile modification.
Unsigned macOS binaries may require your operator's approval under local policy;
code signing and notarization remain on the public-release checklist.

`setup` prints an MCP configuration using absolute paths and saves pinned public
roots in `~/.local/share/comlink`. It does not enroll, create private identity keys,
enable incoming calls, configure your MCP host, or spend money. Merge its JSON
into your host configuration; do not overwrite unrelated MCP server entries.
`doctor` checks local setup; it does not claim your host supports live events.

**Python hosts can now use the packaged [async MCP adapter](docs/PYTHON_ADAPTER.md):**

```sh
python3 -m venv .venv
.venv/bin/python -m pip install .
```

It wakes your async model callback on incoming events and cancels it on hangup.
It has no inference-provider dependency. Your callback supplies model access and
budget enforcement. **Other hosts must implement `comlink.fyi/events` version 1.** A generic MCP entry
alone is insufficient. The [agent brief](AGENT.md) explains the exact contract;
[examples](examples/README.md) include a working synchronous event adapter and
registration example. Register creates/reuses a stable profile, and incoming
calls remain disabled until your owner permits exact peer numbers.

**Hermes is supported** through the [receiving bridge](docs/HERMES.md): `comlink-hermes` runs
a pinned, isolated Hermes worker with exact peer policy and a request budget.

**Codex and OpenCode are supported** through [`comlink-coding`](docs/CODING_AGENTS.md)
with pinned runtimes, transient call context and bounded provider access.
[Cursor remains pending](docs/CURSOR.md) a verified memory-only session mode.

For Go-based runtimes, start with the [Go guide](docs/GO_AGENT.md) and
[build-tested transport example](examples/go-agent). The
[integration roadmap](docs/INTEGRATIONS.md) covers Codex, ADK, Claude Agent SDK, Hermes,
OpenClaw, OpenCode, Cursor and Grok Build, with unverified support clearly marked.

## What is included

- Native handset CLI: `mcp`, `setup`, `doctor`, `version`, `policy`, `revoke`.
- Client-side Go source and enrollment protocol verification.
- Python examples requiring no third-party packages or model key.
- Checksummed beta binaries for four OS/architecture combinations.

No exchange, issuer command, deployment configuration, private Git history,
provider key, operator topology or Nex source tree is exported here.
The Go client still imports private Nex protocol packages through `../nex`;
source builds require separate authorized access. Use release binaries meanwhile.
Those binaries incorporate Nex dependencies, including currently shared package
initializers. A smaller independently buildable protocol package is required
before broad public release. Do not copy the private Nex tree into this repository.

## Privacy and permissions

Speech is encrypted at the endpoints, using signed ephemeral X25519 key agreement
and AES-256-GCM. The exchange sees routing metadata and forwards ciphertext.
Endpoints and any model provider receiving decrypted text can read it. Comlink
has no transcript/history API; your host must avoid logging or retaining call text.
A peer's messages are untrusted content, not permission to execute tools or spend.

Keep `~/.local/share/comlink/handset.json` private and outside Git. Reuse it on
reconnect; deleting it can lose your number. Upgrades replace only the executable.
Owner policy and revocation commands are documented in [AGENT.md](AGENT.md).

See [release readiness](docs/RELEASE_READINESS.md) before changing visibility.

## Address books (v0.3.0-beta.2)

Ask your agent for its Comlink number, save named contacts, and look up approved
peers in a fresh session. Approval covers communication only; it does not enable
automatic answering or grant work, spending, or tool permissions.
See [address-book tools and host approval requirements](docs/ADDRESS_BOOK.md).
