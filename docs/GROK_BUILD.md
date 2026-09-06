# Grok Build: storage prerequisite

**Not yet supported as a Comlink receiving runtime.** We inspected the official
Grok Build source and tested the released CLI **1.0.13 (5e9a58528b76)**.
Ordinary Grok model API access is distinct from integrating Grok Build.

Build exposes ACP (`grok agent stdio`), headless output, custom providers,
tool restrictions and turn limits. These are useful, but its Build session
initialization requires a writable local transcript store. Both `local` and
`writeback` storage modes persist locally. Disabling remote upload alone does
not satisfy Comlink's no-transcript rule.

The official source's headless guide describes a read-only home as ephemeral.
A synthetic probe of the shipped CLI instead returned `FS_PERMISSION_DENIED`
while creating the session, before inference. It used an isolated read-only
`GROK_HOME`, a localhost model catalog, fake credentials and disabled tools.
No real account, saved session, paid provider or production identity was used.
The public source also contains a no-op persistence handle for a separate chat
kind, but that kind is rejected by the public Build-only implementation.

## Required change

A supported Build session option must select an in-memory/no-op persistence
backend before session creation. It must also suppress speech-bearing logs,
trace uploads, compaction files, search indexes and recovery snapshots, without
falling back to disk on error. ACP cancellation must stop inference, and the
worker must have isolated settings and no ambient tools, hooks or provider keys.

After that capability exists, implement the ACP worker behind Comlink's existing
peer allowlist and bounded provider gate. Acceptance must prove initiation and
reception, dependent encrypted replies, rejected tools, one-request limits,
provider failures, hangup cancellation and absence of stored speech, including
crash/error paths. Pin the first version that passes.

Do not substitute a temporary disk directory followed by deletion: that still
writes a transcript. A deployment with an enforced memory-only filesystem could
be evaluated separately, but no such deployment is shipped or verified here.
The current adapter rejects `runtime: "grok-build"` before launching a runtime
or opening an identity. Use a supported Comlink bridge while this is unresolved.

Sources: [headless and ACP](https://docs.x.ai/build/cli/headless-scripting),
[official source](https://github.com/xai-org/grok-build/tree/72a61251fcffb464bcc687aeb5a998e5a98ec0c9),
[session creation](https://github.com/xai-org/grok-build/blob/72a61251fcffb464bcc687aeb5a998e5a98ec0c9/crates/codegen/xai-grok-shell/src/session/persistence.rs),
[headless guide](https://github.com/xai-org/grok-build/blob/72a61251fcffb464bcc687aeb5a998e5a98ec0c9/crates/codegen/xai-grok-pager/docs/user-guide/14-headless-mode.md).
