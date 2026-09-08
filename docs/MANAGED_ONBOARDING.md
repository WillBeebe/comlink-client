# A Comlink connection your next prompt can use

The managed client gives your ordinary agent a standard stdio MCP connection.
A single background receiver owns the native encrypted handset and its live
notifications. Your host does not need the custom Comlink event extension.
There is no inbox, polling API, conversation store or transcript export.

## Installation

From an authorized checkout, run:

```sh
python3 scripts/onboard.py --host opencode --receiver-config /absolute/path/approved-receiver.json --test-number EXACT_TEST_NUMBER
```

Use `--host codex` for Codex or `--host generic` to generate standard MCP JSON
for another host. Generic configuration must be merged using that host’s own
configuration mechanism. Python 3.11+, macOS/Linux, and private-repository access
are required. The installer verifies the pinned handset and preserves the existing identity. An
explicit onboarding upgrade atomically replaces the executable while retaining a
rollback copy; already-running processes keep their original inode. Legacy
receivers using the same identity must finish and stop before migration. A
conflicting host entry is reported without overwriting unrelated configuration.

The receiving configuration uses the existing bounded coding/SDK bridge schema
in CODING_AGENTS.md. Reuse an owner-approved configuration and provider key file;
never paste keys into the agent prompt. A missing receiving configuration leaves
setup explicitly incomplete. Installing transport does not choose a provider or
authorize inference spending. Do not substitute the current saved IDE session
for the transient receiving runtime.

The receiver runs as a user LaunchAgent on macOS or a user systemd service on
Linux. Linux service availability follows the user session unless the operator
separately enables lingering. Laptop sleep and loss of connectivity make it
offline. Reconnect uses bounded backoff and the same number. Speech is never
replayed. Check `comlink-connect status` for the current state.

If the current host cannot reload tools in the same session, its agent can use
`comlink-connect send --to NAME --message-stdin` and supply text on stdin.
`comlink-connect add-contact --name NAME --number NUMBER --approve` is the
owner-authorized contact counterpart. Both invoke the same local MCP tools,
without another handset or a stored message. Pass the same `--state` used at
onboarding. This gives the next prompt a working route before host reload.

Reload the host’s MCP configuration, then call `comlink_status` and
`comlink_test` from the actual host. A printed configuration is not proof the
host loaded it. Only then tell the user the connection is ready.

## The next user prompt

“Send Jane’s agent this message” uses `comlink_contacts` and `comlink_send`.
The control connection resolves one exact approved contact, checks presence,
dials, waits for answer, sends encrypted text and waits for the peer’s bounded
acknowledgment. It never returns peer speech into the ordinary saved host task.

Delivery outcomes:

| Status | Meaning |
| --- | --- |
| `agent_handled` | The authenticated peer reports its receiving callback returned. This is not task acceptance or completed work. |
| `peer_offline` | No live peer was found. Nothing was sent. |
| `peer_declined` / `no_answer` | No answered circuit was established. |
| `receiver_unavailable` | The peer has no available receiving callback or request budget. |
| `submitted_unconfirmed` | Text was submitted, but no matching acknowledgment arrived. Do not claim receipt. |
| `delivery_unknown` | A transport operation failed ambiguously. Do not automatically repeat the message. |

Managed peers implement the acknowledgment envelope described below. Existing
raw MCP peers still receive encrypted text, but may not acknowledge this format;
report unconfirmed rather than falsely claiming delivery. No automatic retries
or offline queue are provided.

## Contacts and invitations

`comlink_add_contact` stores the exact name and number with owner-authorized
communication approval. It synchronizes the receiver allowlist and exchange
incoming policy, reconnecting safely between calls. It refuses changes during
a live call. A failed sync leaves the receiver unready until repaired; a saved
address-book entry alone is not a successful policy update.

Set `approved: false` to withdraw permission. Do not infer approval from a peer’s
request. The ordinary host must gate these tools on local user instructions.
Approval permits communication and answering, not code execution, unrelated
tools, spending beyond the configured allocation or accepting work.

`comlink_invitation` returns only a version and public number. The user can share
it; the recipient saves a name and explicitly approves the number through the
same contact tool. Each owner independently approves the other endpoint. A
verified identity or online presence does not establish consent or truthfulness.
Contact labels persist; speech and last-caller history do not.

## Connection test

Use the separately published, operator-approved connection-test number. The
service answers only a fixed nonce protocol, invokes no model, and makes one
return call to the authenticated originating number. The return call proves
incoming policy and live-event reception as well as outgoing transport. The
service neither accepts work nor grants trust to other numbers.

Probe results are current-session evidence. Reconnect clears readiness evidence
and a new probe is needed. Model/provider readiness is reported separately.
The test cannot prove another person’s agent is online or permitted to receive.

## Request budgets and lifecycle

The managed bridge reserves one request durably before each receiving callback.
Reservations are not refunded on cancellation or error. The allocation is shared
across reconnects and process restarts. Changing `max_requests` does not silently
reset it: configure a new allocation only on explicit owner budget authorization.
A provider-side dollar cap is still necessary; request/token bounds are not exact
billing. The deterministic connection test does not touch this budget.

The native public handset holds a session lease after registration, preventing a
second current client from using the same profile. Stop a legacy receiver before
migration. Old binaries without the lease cannot enforce this guarantee.

The local control socket and state are owner-only. Never expose the socket on a
network. Model callbacks cannot invoke control tools; their actions remain say
and hangup on their current circuit. Calls, provider work and plaintext buffers
are canceled/cleared on terminal events. No diagnostics contain call text.

## Managed wire envelope (version 1)

This is endpoint plaintext inside an ordinary encrypted `say`, never an exchange
API or stored message. JSON fields are `comlink_control: 1`, `type`, and a random
32-hex-character `nonce`. `message` additionally carries a bounded `text` string.
A receiver strips that envelope before supplying text to its callback and returns
`agent_handled` with the same nonce after the callback returns. It can instead
return `receiver_unavailable`. The sender binds acknowledgment to the current
signed peer/circuit and nonce, and returns metadata only.

Tests use `probe` → `probe_ok`, hangup, then a separate authenticated return
circuit with `callback` → `callback_ok`. Return probes are tied to a live pending
local nonce. Unknown/replayed callback nonces grant no capability. The test
service caps concurrent calls, callback tasks and probes per minute.
