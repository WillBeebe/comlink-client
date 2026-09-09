# Collaboration with Nexum and nex

These five applications use the Nexum framework through its public `nex` Go
client: `github.com/WillBeebe/nexum/nex`. There is one public module, not separate
Nexum, nex and nexus library downloads. The helpers in `internal/lab` delegate to
that client; the application code supplies predicates and resource policy.

The examples have their own Go module in this directory. This prevents the
private handset's circuit/identity dependencies from becoming a prerequisite
for running an agreement example. Go commands for these examples must run from
this directory; root `go test ./...` does not include this nested module.

## Install and run

The examples pin **Nexum v0.1.1**. Use Go 1.25 or newer. Both repositories are public. See the [Nexum installation guide](https://github.com/WillBeebe/nexum/blob/main/docs/INSTALL.md).
No local Nexum checkout or module replacement is needed.

From the client checkout:

```sh
cd examples
go mod download
go run ./work-bounty
go test -race ./...
```

From the client root, `python3 examples/check-collaboration.py` runs all five
programs and race tests using the pinned downloaded dependency in a temporary
module. It does not register identities, contact peers or make model calls.
Normal Go module downloads need no private-beta Git configuration. Go dependency downloads may require network access.

For local Nexum development only, `--nexum-source /path/to/clean-checkout`
overrides the dependency in the checker's temporary module, never this checkout.

## Choose an application

- [Work bounty](work-bounty): signed consent and verified-result settlement.
- [Compute lease](compute-lease): reservations, dispatch policy and settlement.
- [Delegation](delegation): signed grants, bounded tool use and settlement.
- [Knowledge exchange](knowledge-exchange): consent and condition-gated HPKE key release.
- [Collective fund](collective-fund): Paillier aggregation, exact-target proof and milestone release.

These run locally with ephemeral keys and in-memory state. They are framework
integrations, not live Comlink conversations or production custody. Live peer
communication uses the existing managed Comlink connection and separately
approved receiving policy. The private handset implementation has not been
relicensed or copied into Nexum.

Version 0.1.1 uses general agreement construction and proof-checked acceptance.
Failed contract settlement preserves the receipt head and evidence, allowing a
retry after the cause is resolved. This is in-memory atomicity, not crash recovery.
All five examples use this pinned module; their application policies are unchanged.
