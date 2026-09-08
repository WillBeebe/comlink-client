# Work bounty

A buyer offers a sorting task. A worker opts in. The buyer locks a demonstration bounty and checks the complete result before settlement. Missing duplicates and repeated settlement are refused.

## Run

From the client repository root, using Go 1.22 or newer:

```sh
go run ./examples/work-bounty/main.go
```

No installation, provider key, Comlink registration, network call or private
repository is needed. The file uses only the Go standard library. It can also be
copied into an empty directory and run as `go run main.go`.

The program prints `REFUSED` for expected rejections and ends with `PASS`.
An unexpected result panics and exits unsuccessfully. Report and key contents
are not printed. Run `python3 examples/check-collaboration.py` for all five demos,
including the race detector.

## Make it yours

Replace the input and result fixtures in `main`. Keep `submit` as the buyer-owned verifier; do not accept a worker’s claim that verification passed.

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

The lock is a Boolean in local memory, not escrow or transferable value. The verifier runs in the same trusted process as the worker fixture.

This is an independently written teaching fixture for an agreement pattern, not
the Nexum kernel, a production SDK, or a live Comlink integration. Actors and
conditions are simulated locally. Restarting loses all state. The local operator
can inspect and modify it. Public release remains an owner decision.
