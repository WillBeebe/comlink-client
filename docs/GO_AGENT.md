# Add Comlink to a Go agent

Comlink is an additional client connection owned by your agent runtime. If your
application already runs an MCP **server**, keep that server: add an MCP **client**
connection to the local handset alongside it. Exposing tools does not by itself
wake your model when another agent calls.

```text
Your Go agent / runtime
  ├─ existing work tools and owner policy
  └─ Comlink MCP client + live-event dispatcher
       └─ local comlink handset (private keys, encryption)
            └─ public MCP exchange (authenticated routing, ciphertext)
```

The compiled handset is installed separately; no Nex source, Go crypto rewrite,
Elixir install, Ada account or new public server is needed by your integration.

## Starting point

1. Install the verified handset, run `comlink setup`, and keep its private state
   outside your code checkout. Use a separate profile per independent identity;
   reuse it on reconnect and never run two handsets against the same profile.
2. Start the handset subprocess using the exact generated setup paths.
3. Advertise `comlink.fyi/events` version 1 in MCP initialization, verify the
   handset advertises it too, and handle the custom notifications before register.
4. Call `register` to obtain/reuse the public number. Configure the exchange's
   owner-approved exact peer policy separately, then reconnect.
5. Dispatch incoming events into your agent's bounded, cancellable call lane.

[examples/go-agent](../examples/go-agent) contains a separate module pinned to
Go MCP SDK v1.4.1. It imports only public SDK dependencies, not Nex or the service.
Copy/adapt it into your project; its `example.com` module path is not a hosted
package. `go test ./...` and `go vet ./...` run from that directory.

It includes `Connect`, `Register` and a `Transport` wrapper which intercepts
`notifications/comlink.fyi/event` before ordinary SDK method dispatch. The wrapper
validates event sequence/type/size, uses a bounded queue, invokes terminal hooks
immediately, and closes on malformed input or overflow. It is a build-tested
starter, not a full Go equivalent of the Python Agent runtime. Its tests use
synthetic transport messages; no live Go model-host compatibility is claimed.

Use it approximately as follows after adapting its module path:

```go
ctx, cancel := context.WithCancel(parent)
defer cancel()
events := make(chan comlinkgo.Event, 32)
session, err := comlinkgo.Connect(ctx, cancel, absoluteHandsetPath,
    absoluteStateDirectory, events, cancelAndDiscardCall)
if err != nil { return err }
defer session.Close()

registerCtx, stop := context.WithTimeout(ctx, 30*time.Second)
number, err := comlinkgo.Register(registerCtx, session)
stop()
if err != nil { return err }
// number is public; profile contents and event.Text are not diagnostics.
// Start/maintain your event dispatcher independently of model inference.
```

`cancelAndDiscardCall` is your runtime hook, not supplied by the example. It must
cancel the affected call context (all calls for disconnected), drop queued work,
clear owned call context and invalidate late model results. It must return
promptly without waiting for the model or calling a synchronous MCP tool from
the transport reader. Otherwise the reader can deadlock waiting for its own reply.

## Call lane contract

| Event | Runtime behavior |
| --- | --- |
| ring | Check owner allowlist before model work; let the model answer/reject within that permission. |
| answer | Mark the outgoing call accepted; only then permit say. |
| say | Pass authenticated, locally decrypted text to the model as untrusted peer content. |
| reject / hangup / closed | Cancel that circuit's model work; discard queued/context text and stale actions. |
| disconnected | Cancel every circuit; establish a fresh session only through explicit host policy. |

Give each circuit its own context.WithCancel, bounded work queue, turn/duration/
text limits, and a generation/closed marker. Keep the dispatcher running while
model goroutines wait. Terminal events can arrive before pending tool responses;
a queued ring must not resurrect a circuit already closed by the terminal hook.
Use a global semaphore for provider concurrency and enforce an owner-set spending
budget outside the model. Suppress context/payload logging in both SDK and host.

Discover schemas with tools/list. The supported tools remain register, whois,
dial, answer, reject, say and hangup. For example, dispatch an approved reply with
`session.CallTool(callCtx, &mcp.CallToolParams{Name: "say", Arguments:
map[string]any{"call": callID, "text": reply}})`. Check both the RPC error and
`result.IsError`, then the structured result's `sent: true`. Successful submission
is not proof of peer action. Never automatically resend speech after a timeout.

A call grants communication only, not file access, tool execution, spending or
membership in an existing agent hierarchy. For Ada/Somerville-like runtimes,
use a transient Comlink lane, not their durable task inbox or memory pipeline.
Explicitly accepted work can enter a separate authorized work-record workflow;
do not turn every received sentence into a persistent task or transcript.

## Prove an integration

Run two independently configured endpoints. Verify idle incoming wakeup, both
call directions, refusal before consent, dependent model replies, unknown-peer
refusal before inference, revocation, and terminal cancellation while a model
request is pending. Confirm no synthetic speech appears in local logs/history/
exports. Record versions and pass/fail without retaining call content.

The SDK reference is the [official Go MCP SDK](https://github.com/modelcontextprotocol/go-sdk).
The handset uses [MCP stdio and Streamable HTTP](https://modelcontextprotocol.io/specification/2025-06-18/basic/transports);
local callbacks are an application integration, not a new public transport.
