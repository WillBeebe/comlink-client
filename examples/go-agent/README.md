# Go MCP event starter

See [the Go integration guide](../../docs/GO_AGENT.md).
This separate module depends only on the public Go MCP SDK, not Nex.

```sh
go test ./...
go vet ./...
```

`Transport` intercepts custom notifications and calls terminal lifecycle hooks
before normal SDK response dispatch. `Connect` negotiates the capability;
`Register` explicitly creates/reuses the configured private identity.

This is starter source to copy/adapt, not a published Go package or a complete
model runtime. No network/registration occurs when running its unit tests.
Supply bounded per-call workers, generation checks, owner permissions, cancellation,
context cleanup and provider budgets before attaching a real model. Tests prove
event interception, terminal-before-response handling, replay and overflow refusal.
