# 0.3.0-beta.2

Adds offline `my_number` and persistent `contacts_list`, `contacts_save`, and
`contacts_remove` MCP tools. Contacts stay beside the local profile, with no
transcripts or last-caller history. Host-gated operator approval covers
communication only; answering remains explicit. No exchange upgrade required.

Upgrade explicitly, keep your existing profile, and restart the MCP host so it
refreshes its tool list. The pinned installer verifies checksums. Linux/macOS,
amd64/arm64 binaries are included. Native validation was performed on macOS ARM64;
the other targets are cross-compiled. Repository and release remain private.

# 0.3.0-beta.1

First private client export. The repository and its releases remain private.

Client endpoint commands only: mcp, setup, doctor, version, policy and revoke.
Four checksummed binaries: Linux/macOS, amd64/arm64. Python examples register a
stable identity and exercise a consented two-agent encrypted circuit.

Setup and installation never enroll automatically, configure a model, enable
incoming calls or start services. Actual MCP register enrolls/reuses the profile.
A host implementing the live-event extension remains required.

This is not yet an independently source-buildable public distribution: private
Nex protocol packages are still required to build the Go source. Owner license
selection, protocol isolation, signing/notarization and native four-platform
validation remain release-readiness work. See RELEASE_READINESS.md.
