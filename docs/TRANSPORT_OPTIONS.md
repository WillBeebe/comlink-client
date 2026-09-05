# MCP today; a possible local gRPC interface later

Decision: do not implement gRPC now. No material benefit over the existing local
MCP stream has been established. Revisit only for a concrete integration need.

Design note, not an implemented interface or a change to the public service.
The current product remains MCP-only. The owner asked whether gRPC would be
useful for direct runtime clients; this records the tradeoff for a future decision.

MCP already supports continuous notifications through stdio and Streamable HTTP.
Comlink uses them today. gRPC would therefore not create streaming where none
exists. Its potential benefit is generated typed clients, streaming RPC ergonomics
and native integration for Go/other custom runtimes. gRPC supports bidirectional
streams; this alone does not prove lower latency or higher throughput here.

First stabilize language adapters over the existing handset. If a measured
integration need remains, consider an owner-approved **local** gRPC facade:

    runtime -> local typed gRPC stream -> encrypting handset -> existing MCP exchange

The local side can contain plaintext just like stdio, so use endpoint-owned IPC
(e.g. a permission-restricted Unix socket). Loopback TCP is not authentication;
any TCP option needs explicit caller authentication. Expose the same identity,
consent, revocation and seven-tool semantics; no keys, issuer operations or history.
Map stream cancellation to call teardown, retain bounded queues/quotas, and never
retry uncertain speech automatically. Session resumption must not replay calls.

Do not add a second public exchange API while pressure testing the existing one.
A public gRPC protocol would be a separate product/security/operational change,
requiring parity tests, authentication, edge compatibility and measured value.
It is not required to integrate a Go agent: the MCP SDK is already native Go.

References: [MCP transports](https://modelcontextprotocol.io/specification/2025-06-18/basic/transports),
[gRPC concepts and stream lifecycle](https://grpc.io/docs/what-is-grpc/core-concepts/).
