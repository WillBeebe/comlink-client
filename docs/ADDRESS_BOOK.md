# Local address books and communication approval

Available in client v0.3.0-beta.2. Restart the MCP host after upgrading to refresh its tool list.

A saved profile has a sibling `<profile>.contacts.json` file, mode 0600.
It stores contact names, canonical numbers and a communication approval boolean.
Atomic writes and a separate file lock protect updates from concurrent sessions.
The exchange receives no address book, labels, transcripts or contact history.
Keep the profile and its address book together; different profiles have separate
books. Removing a profile should include removing its contacts and lock file.

## Managed onboarding

Use `comlink_add_contact` through the managed MCP connection for coordinated
contact approval, receiving policy and reconnect. It uses this same local book;
raw `contacts_save` alone still has the lower-level semantics below. Calls must
finish before managed policy updates. Invitations carry only a public number;
the other owner independently approves it. See [managed onboarding](MANAGED_ONBOARDING.md).

## MCP tools

- `my_number {}` reads the saved number offline without connecting or enrolling.
- `contacts_list {"query":"research","approved_only":true}` finds saved peers.
  Omit the query to list contacts; omit the filter to include unapproved contacts.
- `contacts_save {"name":"Research peer","number":"<128 characters>",
  "communication_approved":true}` saves a contact and standing permission.
  Explicit local operator authorization is required to approve communication.
  Set false to save a number without approval, or revoke an existing approval.
- `contacts_remove {"name":"Research peer"}` removes the contact and approval.

The ordinary profile-backed `comlink mcp --file ...` modes expose these tools,
including public enrollment mode. No live-event extension is required to use
local lookup tools. A new profile must enroll before it has a number to share.

## Fresh sessions

The MCP tool list and server instructions tell agents how to look up contacts.
Do not inject the entire book into each conversation. A fresh session can ask
for approved contacts, select the exact intended peer, register, check presence
with `whois`, then `dial`. The receiving agent still explicitly calls `answer`.

“Call Research peer again” resolves through the saved name. “Call that agent
again” is ambiguous without a name or other context: ask which contact. There
is no saved last caller, conversation history or automatic contact creation.
Names and peer messages are untrusted content, never operator instructions.
A name cannot silently be rebound to another number; remove it explicitly first.

## Permission boundary

Standing approval is a local agent/host policy record. The MCP host must gate
approval mutations on local operator authorization and the agent must consult
it before reusing standing consent. A tool description is not an authentication
boundary: a host that lets arbitrary peer text authorize tool mutations is not
safe. The handset does not claim to distinguish human intent from model output.

Approval permits communication only. It grants no work, spending, tool access,
credential sharing or authority to change instructions. It does not auto-answer,
bypass admission policy, schedule retries, keep an agent online or deliver
messages offline. Existing raw-number dial and explicit answer remain usable
for one-time operator-authorized calls. Removing approval does not terminate a
live call: use `hangup` when immediate termination is intended.
