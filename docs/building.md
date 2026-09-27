# Building Provenance

Provenance's release binaries are reproducible: anyone who builds the same
source with the same toolchain and the same two injected values gets a
byte-identical binary, whatever machine, operating system or directory they
build in. A binary's SHA-256 is therefore enough to show it was built from
public source, and it is the identity that `verify artifact` checks.

## Requirements

- **Go at the exact version in `go.mod`'s `toolchain` directive**
  (currently `go1.27.1`). With any Go 1.21 or later installed, the `go`
  command downloads and uses that version automatically unless
  `GOTOOLCHAIN=local` is set.
- **GNU make and bash** for the `make` targets. On Windows, use Git for
  Windows' SDK, MSYS2 or WSL — or run the plain `go build` command below.
- **git**, if you want `make` to fill in the version and commit for you.

Dependencies are pinned by checksum in `go.sum`. No C toolchain is needed.

## Recipe

```sh
make build        # bin/provenance for this machine
make dist         # dist/provenance-{linux-amd64,darwin-arm64,windows-amd64.exe}
```

Both run this command, once per platform:

```sh
CGO_ENABLED=0 GOOS=<os> GOARCH=<arch> go build \
  -trimpath -buildvcs=false \
  -ldflags '-X github.com/dynamatt/provenance/internal/version.Version=<version> -X github.com/dynamatt/provenance/internal/version.Commit=<commit>' \
  -o <output> ./cmd/provenance
```

| Setting | Why |
| --- | --- |
| `CGO_ENABLED=0` | Pure Go: no host C compiler or libc leaks into the binary, and cross-compiling needs nothing else. |
| `-trimpath` | Removes the checkout path and module cache path from the binary. |
| `-buildvcs=false` | Stops Go stamping git state (commit time, dirty flag) into the binary. The only VCS facts in the binary are the two injected below. |
| `-ldflags -X …Version`, `-X …Commit` | The only injected values. `make` uses `git describe --tags --always --dirty` and `git rev-parse HEAD`. Nothing time-dependent is injected. |

## Reproducing a published binary

The injected version depends on the tags in your clone: a full clone may
describe the same commit as `v0.1.0` while a shallow CI checkout says
`b5bd556`. To reproduce a specific binary, read its identity from the binary
itself and pass it explicitly:

```sh
./provenance version            # provenance <version> / commit: <commit> / go: go1.27.1
git checkout <commit>
make dist VERSION=<version> COMMIT=<commit>
sha256sum dist/*                # compare with the published hashes
```

## How CI proves it

`.github/workflows/reproducible.yml` runs on every pull request and every push
to `main`:

1. `make dist` runs on two runners with different CPU architectures —
   `ubuntu-latest` (x64) and `ubuntu-24.04-arm` (arm64) — each checking out
   to a different path.
2. A `compare` job puts both sets of SHA-256s side by side in the job
   summary and fails if any binary differs, or if the runners built different
   sets of binaries.

The same three binaries have also been built from a Windows host with the
plain `go build` command above, with matching hashes.

## Seeing it fail

The check only means something if it can fail. Inject a time-dependent value
and build twice:

```sh
make build VERSION=dev-$(date +%s%N) && sha256sum bin/provenance
make build VERSION=dev-$(date +%s%N) && sha256sum bin/provenance   # different hash
make build && sha256sum bin/provenance                             # back to the reproducible build
```
