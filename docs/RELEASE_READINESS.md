# Before making this public

- Owner selects a source license; none is invented by this export.
- Separate Nex's client protocol dependency from its private core. Current Go
  source needs an authorized sibling Nex checkout; release binaries do not.
- Review linked Nex dependencies/initializers and third-party notices. Do not
  infer that stripping symbols removes bundled code or constitutes protection.
- Sign/notarize macOS executables and add independently verifiable release signing.
- Establish a supported event-aware model host. The synchronous example is a
  working protocol reference, not a production concurrent model runtime.
- Exercise Windows support before offering it; the beta supports Linux/macOS only.
- Verify clean-machine private/public download flows on every advertised platform.
- Keep updates explicit, preserve profiles, and never restart a live call to update.
- Keep incoming calls opt-in. No automatic provider configuration or spending.

The existing private Origin service repository and Nex source tree remain private.
Changing this repository's visibility does not authorize their publication.

## Reproduce a private beta

With an authorized, clean sibling Nex checkout at
`73814649998b202d2b9964a28814367d2e285e52`, Go 1.26.5, and this checkout:

```sh
go test ./cmd/comlink
go vet ./cmd/comlink ./internal/...
python3 -m unittest discover -s tests
python3 scripts/build.py
```

The build strips local paths/VCS embedding, disables CGO, gathers notices, and
writes four executables plus `release.json`, `THIRD_PARTY_NOTICES.txt`, and
`SHA256SUMS` under ignored `dist/`. It updates `release-lock.json`. Source archives
and Nex packages are not release assets. The build does not publish or enroll.
Beta 0.3.0-beta.1 was tested on macOS ARM64 with a local Elixir exchange; other
platform binaries were cross-compiled and require native clean-machine validation.
