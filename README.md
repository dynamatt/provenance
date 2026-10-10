# provenance
Provenance is a GxP-compliant, git-native design control platform that gives product development teams cryptographically auditable traceability across requirements, design, risk, and verification — without a product-operated server in the critical path.

## Status

Pre-release. `provenance export website` is complete: it renders a design
history file kept in git as a static website, with Documents built from live
query blocks and wikilinks, calculated fields, numbered captions, reference
lists and the commit and content hash behind every page.
`provenance verify content` prints the content hash. The other commands are
planned; [`PLAN.md`](PLAN.md) has the order, and the
[CLI reference](https://github.com/dynamatt/provenance-website/tree/main/content/docs/cli)
marks each one. [`provenance-example`](https://github.com/dynamatt/provenance-example)
is a worked example to try it on.

The design lives in [`provenance-ddf`](https://github.com/dynamatt/provenance-ddf),
compiled with Provenance itself.

## Install

Download the binary for your platform and `SHA256SUMS` from the
[latest release](https://github.com/dynamatt/provenance/releases), then check
it before running it:

```bash
sha256sum -c SHA256SUMS --ignore-missing   # macOS: shasum -a 256 <file>, compare with its line
chmod +x provenance-linux-amd64
./provenance-linux-amd64 version
```

Every release is built reproducibly (`CGO_ENABLED=0`, `-trimpath`, a pinned Go
toolchain): `make dist VERSION=<tag>` at the tagged commit produces the same
bytes, and `dist/SHA256SUMS` the same manifest.

## Build from source

`make build` writes `bin/provenance`; `make ci` runs lint, unit tests and the
acceptance tests against the pinned example repository. See
[`CLAUDE.md`](CLAUDE.md) for the layout of the code.
