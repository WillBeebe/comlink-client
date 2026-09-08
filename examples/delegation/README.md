# Delegation

Give a specialist permission to use one tool within a fixed budget. Eight concurrent attempts compete for two permitted uses; the demo checks that exactly two succeed.

## Run

From the client repository root, using Go 1.22 or newer:

```sh
go run ./examples/delegation/main.go
```

No installation, provider key, Comlink registration, network call or private
repository is needed. The file uses only the Go standard library. It can also be
copied into an empty directory and run as `go run main.go`.

The program prints `REFUSED` for expected rejections and ends with `PASS`.
An unexpected result panics and exits unsuccessfully. Report and key contents
are not printed. Run `python3 examples/check-collaboration.py` for all five demos,
including the race detector.

## Make it yours

Put `use` immediately before an allowed tool call. Derive the peer from your authenticated session. Set the approved tool and its trusted cost in the gateway; never let the caller supply the price.

The whole implementation is in [main.go](main.go). The `main` function shows
both the working path and rejected actions. Copy the file; change the fixture;
run it again. No framework is required.

### Give this to your agent

> Read this example’s README and main.go. Run the example and confirm its PASS
> result. Adapt the agreement functions to my task, preserving the rejection
> checks. Explain which inputs must come from an authenticated caller or trusted
> verifier. Do not register identities, call peers, spend money, or deploy anything
> as part of this local example. For a live Comlink integration, use the client’s
> AGENT.md and existing managed connection instead of inventing a transport.

## Scope

The gateway models admission and fixed-cost accounting; it does not invoke a real tool. Local peer labels are not cryptographic identities. Distributed budgets, failure refunds and runtime isolation need separate implementation.

This is an independently written teaching fixture for an agreement pattern, not
the Nexum kernel, a production SDK, or a live Comlink integration. Actors and
conditions are simulated locally. Restarting loses all state. The local operator
can inspect and modify it. Public release remains an owner decision.
