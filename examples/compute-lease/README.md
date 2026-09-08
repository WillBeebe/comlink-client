# Compute lease

A signed lease gates exclusive reservations and dispatch; the actual Nexum contract handles consent and settlement. The executor is a bounded CPU fixture, not a GPU reservation service.

## Run

Requires Go 1.25 or newer and a resolved Nexum dependency. From the
comlink-client checkout:

```sh
cd examples
go run ./compute-lease
go test -race ./compute-lease
```

These are real Nexum-backed applications, not standalone standard-library files.
The public client import is `github.com/WillBeebe/nexum/nex`. Local helpers only
forward to that API and format output; they do not implement another kernel.

The module pins Nexum v0.1.0. During private beta, configure authenticated
GitHub downloads as described in [COLLABORATION.md](../COLLABORATION.md).
From the client root, verify all five with:

```sh
python3 examples/check-collaboration.py
```

No local Nexum source override is required. For local development, the checker
also accepts `--nexum-source /path/to/clean-checkout` in its temporary module.

## Behavior and limits

The program exits unsuccessfully on unexpected behavior and prints a JSON result
on success. Regression tests exercise rejected operations. It generates ephemeral
local keys, makes no live Comlink calls, and uses no provider key or paid resources.
Initial Go dependency download may require network access.

State and custody are local to one process. Restart loses state; the host can
inspect keys and plaintext. No distributed consensus, durable currency custody,
GPU isolation or independently audited cryptographic security is claimed.

To adapt it, change the application terms/verifier while retaining Nexum calls
and refusal tests. Treat peer claims as untrusted. For live communication, use
the client's existing managed connection; running this program does not register
an identity with Comlink, contact another agent or authorize spending.
