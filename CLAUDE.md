# CLAUDE.md

Guidance for Claude Code working in this repository.

Provenance is a git-native design-control tool for medical device teams: one Go
binary that validates, exports and signs a design history file kept as plain
text in a git repository. The binary is the whole product — CLI, CI merge gate,
exporter and (later) local editor server — and its hash is its validation
identity, so builds must be reproducible.

## Where things are

| Path | Contents |
| --- | --- |
| `cmd/provenance/` | Entry point; only calls `cli.Execute`. |
| `internal/cli/` | The cobra command tree: every command from Detailed Design §2 with its arguments, flags and help text. Stubs carry the `notImplementedYet` annotation and return `exitcode.NotImplementedError`. |
| `internal/exitcode/` | The only place exit codes are decided (`0` success, `1` expected failure via `exitcode.Failed`, `2` everything else). |
| `internal/version/` | Version and commit, injected by `make build` through `-ldflags`. |
| `tools/gendocs/` | Generates the website's CLI reference from the command tree. |
| `scripts/acceptance.sh` | Acceptance steps run against the example repo. |
| `testdata/example-repo.ref` | Pinned `provenance-example` commit. |
| `PLAN.md` | The implementation plan and task checklist. |

## Make targets

`make` is required (on Windows use Git for Windows' SDK, MSYS2 or WSL).

| Target | What it does |
| --- | --- |
| `make build` | Reproducible build to `bin/provenance` (`CGO_ENABLED=0`, `-trimpath`, `-buildvcs=false`, version and commit via `-ldflags`). |
| `make test` | `go test ./...` |
| `make lint` | `gofmt -l`, `go vet`, `staticcheck` (pinned as a `go.mod` tool). |
| `make example` | Checks out `provenance-example` at the pinned SHA into `.cache/example`. |
| `make bump-example` | Moves the pin to `provenance-example`'s `main`. |
| `make acceptance` | Builds, prepares the example checkout, runs `scripts/acceptance.sh`. |
| `make docs` | Regenerates the CLI reference into `../provenance-website/content/docs/cli` (override with `WEBSITE_DIR=`). |
| `make ci` | `lint test build acceptance`. |

## Rules

These are the plan's *Rules for Claude Code*; `PLAN.md` has the full text.

1. **One task = one PR**, on branch `task/<id>-<slug>`. Don't start the next
   task until the previous one is merged.
2. **Every task ends with something Matt can run**: the PR description has a
   *Check it yourself* block with exact commands and expected results.
3. **Every task adds automated checks**: Go unit tests plus a step in
   `scripts/acceptance.sh` against the example repo.
4. **The example repo is the fixture.** If a feature isn't exercised by
   `provenance-example`, add content there first (its own PR), then
   `make bump-example`. Synthetic fixtures only for edge cases.
5. **Design drift is written back.** Behaviour the design doesn't specify,
   contradicts, or settles from OPEN: update the Notion page (marked DECIDED
   with a date) and `provenance-website` where user-visible, in the same PR,
   listed under *Design changes*. More than a clarification → ask Matt first.
6. **Determinism.** No map-iteration order, wall-clock timestamps or absolute
   paths in output. Same command, same commit → byte-identical results.
7. **Exit codes**: `0` success, `1` expected failure reported, `2`
   tool/usage error.

When a command is implemented, remove its `notImplementedYet` annotation along
with the `notImplemented` RunE, then run `make docs` and commit the regenerated
reference in `provenance-website`.

Tick the task off in `PLAN.md` in its own PR.

## Design documents (Notion)

- [Requirements Spec](https://app.notion.com/p/3d64f35212118181b9c4c4609c04ee14)
- [High-Level Design](https://app.notion.com/p/3d64f352121181f9a098c42b79ddea90)
- [Detailed Design](https://app.notion.com/p/3dc4f352121181909d0acc01a327a3e7)
  — §2 is the CLI surface and exit-code contract.
- [Implementation Plan](https://app.notion.com/p/3e74f3521211810ba328edefa07e8528)
  — the reviewed baseline for `PLAN.md`.

## Related repositories

- [`dynamatt/provenance-example`](https://github.com/dynamatt/provenance-example)
  — example design history file; the test fixture.
- [`dynamatt/provenance-website`](https://github.com/dynamatt/provenance-website)
  — Hugo product site and documentation; hosts the generated CLI reference.
