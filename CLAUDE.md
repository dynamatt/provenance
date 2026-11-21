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
| `internal/cli/` | The cobra command tree: every command from DES-0019 with its arguments, flags and help text. Stubs carry the `notImplementedYet` annotation and return `exitcode.NotImplementedError`. |
| `internal/exitcode/` | The only place exit codes are decided (`0` success, `1` expected failure via `exitcode.Failed`, `2` everything else). |
| `internal/repo/` | Finds the repository and reads `.component`. `Repo.Files()` is the file system every reader reads the repository through: the working tree, or a commit's tree (`FromFS`). |
| `internal/entity/` | An entity and its record: the values and positions its source wrote, and what a text value refers to. The Markdown record and file parser (frontmatter and body) are here too, and the record's canonical form (`Format`); so is the CSV row record and file parser (`ParseCSV`, DES-0053), which types each cell by its field as YAML would. |
| `internal/schema/` | Loads `schema/`: types, fields, enums, records, facets, and inheritance (`extends:`, `abstract:`): a type's fields are its parent's then its own, and `Expand` lists what a named type stands for. |
| `internal/canon/` | An entity file in canonical form (DES-0038): `entity.Format`, then the checks that only how the file is written changed. Used by `fmt` and validate's `content/format`. |
| `internal/model/` | Entities typed against the schema: values, resolved links, incoming facets. |
| `internal/load/` | Reads a repository the one way `export` and `validate` both use: schema, then entities from each source (`Source`: the Markdown walker, then the CSV files the schema's `source:` names), typed model and graph, collecting every problem under its built-in check. `Read` stops before typing, for commands that need only the files (`fmt`, `verify content`). Tells `history` which files are content and the entities a file holds, and reads the history once (`Result.History`, `Result.Git`) for every stamp, HEAD's content hash and log only when asked for. `At` loads a commit from git's objects, through the same sources. `load/loadtest` is a second source for tests, a table typed by field. |
| `internal/validate/` | `provenance validate`: the built-in checks (the catalogue in `validate.Checks`), rule files, every exporter's `Check`, and the reports: text, JSON, JUnit, SARIF and report templates. |
| `internal/compare/` | An entity's values and texts compared at two versions (DES-0055): typed, so formatting alone is no change; text by Markdown block and word (`text.go`, over Myers' edit script, `myers.go`). `diff` shows what it finds; `load` gives `history` the fields each commit changed through it (DES-0058). |
| `internal/diff/` | `provenance diff` (DES-0055–0057): two loaded sides compared, entities by ID and typed value, text by Markdown block and word, files that aren't entities by path (images by git object hash); the rendered pages compared block by block (`pages.go`); the outputs, built-in `templates/text.tmpl` and `html.tmpl` or a project's `templates/diff/<name>.tmpl` (`output.go`). |
| `internal/query/` | Condition grammar, query blocks, calculated fields and the dependency walk, over the Datalog engine in `query/datalog/`. The entities' changes from git (`changes`, `last_changed`, DES-0058) join the facts the first time a condition or formula names them (`history.go`). |
| `internal/tmpl/` | What project templates share, for the site and validation reports: reading, parsing with missing keys as errors, errors at their file and line, the template data of an entity, and the link functions. |
| `internal/problem/` | A problem with the repository's content, one shape whether loading or an exporter's check finds it: its check, place, entity, and the error that stops an export. |
| `internal/markdown/` | Markdown to HTML with wikilinks, embeds, captions, `{#tag}` tags and fenced blocks; the caller resolves them. |
| `internal/assets/` | Resolves and reads the image files Markdown refers to. |
| `internal/yamlnode/` | Reading YAML nodes with file line numbers, and YAML errors at their line. |
| `internal/history/` | Git via go-git: content hash, dirty state, first-parent log of the entities and files each commit changed, revisions, and a commit's tree as an `fs.FS` (`Tree`), and every entity's changes by value, each dated by the commit that made it (`Changes`, DES-0058). `history/historytest` builds repositories commit by commit for tests: parents, author and committer dates. |
| `internal/export/` | The `Exporter` interface (`Export`, and `Check` for validate), the registry, `--scope`, git stamps and the output folder; `export/website/` is the website exporter and its built-in templates, type pages, for types with `pages: false` (`typepages.go`), and the numbering of headings, list items and captions and the references to them, once every page has rendered (`references.go`, DES-0034). |
| `internal/version/` | Version and commit, injected by `make build` through `-ldflags`. |
| `starters/` | The starter projects `provenance init` writes (DES-0052), one folder each, embedded in the binary; its tests check each validates clean, exports, is canonical and cites a clause on every field. |
| `tools/gendocs/` | Generates the website's CLI reference from the command tree. |
| `scripts/acceptance.sh` | Acceptance steps run against the example repo. |
| `scripts/golden-stamps.sh` | Replaces the commit and content-hash stamps in an exported site by placeholders, so the golden site doesn't change on every pin move. |
| `testdata/example-repo.ref` | Pinned `provenance-example` commit. |
| `testdata/schemas/` | The SARIF 2.1.0 JSON schema and Jenkins' JUnit XSD the reports are checked against. |
| `testdata/violations/` | One example of every failure `validate` reports, each beside a clean counterpart (its `README.md`). |
| `PLAN.md` | The implementation plan and task checklist. |

## Make targets

`make` is required (on Windows use Git for Windows' SDK, MSYS2 or WSL).

| Target | What it does |
| --- | --- |
| `make build` | Reproducible build to `bin/provenance` (`CGO_ENABLED=0`, `-trimpath`, `-buildvcs=false`, version and commit via `-ldflags`). |
| `make dist` | The release platforms into `dist/` with the same recipe, and their manifest `dist/SHA256SUMS` (DES-0025). The release workflow publishes it for each `v*` tag, or for a version given to its Run workflow, which tags the commit it built. |
| `make test` | `go test ./...` |
| `make lint` | `gofmt -l`, `go vet`, `staticcheck` (pinned as a `go.mod` tool). |
| `make example` | Checks out `provenance-example` at the pinned SHA into `.cache/example`. |
| `make bump-example` | Moves the pin to `provenance-example`'s `main`, or to another branch with `EXAMPLE_BRANCH=` (an epic branch while the epic is open). |
| `make acceptance` | Builds, prepares the example checkout, runs `scripts/acceptance.sh`. Needs `xmllint` (`libxml2-utils`) to check the JUnit report. |
| `make docs` | Regenerates the CLI reference into `../provenance-website/content/docs/cli` (override with `WEBSITE_DIR=`). |
| `make ci` | `lint test build acceptance`. |

## Rules

These are the plan's *Rules for Claude Code*; `PLAN.md` has the full text.

1. **One epic = one branch; one task = one PR into it.** Each epic has a
   branch `epic/<id>-<slug>` cut from `main` in every repository it touches.
   A task is a branch `task/<id>-<slug>` cut from the epic branch, and its
   PRs target the epic branch. Don't start the next task until the previous
   one is merged into the epic branch. The epic merges into `main` once, at
   its close-out, in each repository. Plan-only changes go to `main`.
2. **Every task ends with something Matt can run**: the PR description has a
   *Check it yourself* block with exact commands and expected results.
3. **Every task adds automated checks**: Go unit tests plus a step in
   `scripts/acceptance.sh` against the example repo.
4. **The example repo is the fixture.** If a feature isn't exercised by
   `provenance-example`, add content there first (its own PR), then
   `make bump-example`. Synthetic fixtures only for edge cases.
5. **Design drift is written back.** Behaviour the design doesn't specify,
   contradicts, or settles from OPEN: update `provenance-ddf` (marked DECIDED
   with a date) and `provenance-website` where user-visible, in the same PR,
   listed under *Design changes*. More than a clarification → ask Matt first.
6. **Determinism.** No map-iteration order, wall-clock timestamps or absolute
   paths in output. Same command, same commit → byte-identical results.
7. **Exit codes**: `0` success, `1` expected failure reported, `2`
   tool/usage error.

When a check is added to `validate`, add its case to `testdata/violations`
(see its README); `TestViolationsCoverEveryRule` fails until it is there.

When a command is implemented, remove its `notImplementedYet` annotation along
with the `notImplemented` RunE, then run `make docs` and commit the regenerated
reference in `provenance-website`.

Tick the task off in `PLAN.md` in the task's own PR, not in a separate one.

## Design documents

- [`dynamatt/provenance-ddf`](https://github.com/dynamatt/provenance-ddf) —
  user needs (`USR/`), requirements (`REQ/`) and design elements (`DES/`),
  compiled with Provenance. The design home since 2026-10-04.
- `PLAN.md` — the implementation plan, the only copy since 2026-10-05.
- The Notion
  [Requirements Spec](https://app.notion.com/p/3d64f35212118181b9c4c4609c04ee14),
  [High-Level Design](https://app.notion.com/p/3d64f352121181f9a098c42b79ddea90),
  [Detailed Design](https://app.notion.com/p/3dc4f352121181909d0acc01a327a3e7)
  and [Implementation Plan](https://app.notion.com/p/3e74f3521211810ba328edefa07e8528)
  are retired and kept for history. Code comments cite the DDF's IDs
  (e.g. DES-0031, REQ-0073); completed tasks in `PLAN.md` still name the
  Notion sections they worked from.

## Related repositories

- [`dynamatt/provenance-example`](https://github.com/dynamatt/provenance-example)
  — example design history file; the test fixture.
- [`dynamatt/provenance-website`](https://github.com/dynamatt/provenance-website)
  — Hugo product site and documentation; hosts the generated CLI reference.
