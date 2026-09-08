# Knowledge exchange

Encrypt a synthetic report with AES-256-GCM. Release its key only after recipient consent and acknowledgement of the usage terms. Verify that tampering and substituted terms fail authentication.

## Run

From the client repository root, using Go 1.22 or newer:

```sh
go run ./examples/knowledge-exchange/main.go
```

No installation, provider key, Comlink registration, network call or private
repository is needed. The file uses only the Go standard library. It can also be
copied into an empty directory and run as `go run main.go`.

The program prints `REFUSED` for expected rejections and ends with `PASS`.
An unexpected result panics and exits unsuccessfully. Report and key contents
are not printed. Run `python3 examples/check-collaboration.py` for all five demos,
including the race detector.

## Make it yours

Replace the synthetic report with your artifact. Send the ciphertext, nonce and agreed terms first. Deliver the key through your authenticated encrypted channel only after verified recipient consent. Keep keys out of logs.

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

The sender and recipient are simulated in one process. Consent is local state, not a remote signature. A recipient can retain or forward decrypted information; this example does not enforce later use, endpoint secrecy or secure key delivery.

This is an independently written teaching fixture for an agreement pattern, not
the Nexum kernel, a production SDK, or a live Comlink integration. Actors and
conditions are simulated locally. Restarting loses all state. The local operator
can inspect and modify it. Public release remains an owner decision.
