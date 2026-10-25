# Implementation Plan

> **Status: approved by Matt (2026-09-26).** This file is the plan: tasks,
> checkboxes and plan changes, all reviewed through pull requests. It began
> as an export of the Notion
> [Implementation Plan](https://app.notion.com/p/3e74f3521211810ba328edefa07e8528),
> retired 2026-10-05 and kept for history only.

## Progress

- [x] S0.1 Go module and command skeleton
- [x] S0.2 Build tooling and example-repo fixture
- [x] S0.3 CLAUDE.md, PLAN.md and website CLI reference
- [x] S0.4 Design reconciliation
- [x] S1.1 GitHub Actions CI workflow
- [x] S1.2 Reproducible-build check
- [x] E1.1 Exporter interface and output folder
- [x] E1.2 Entity discovery and parsing
- [x] E1.3 Schema loading, default entity pages, golden test
- [x] E1.4 Links, reverse links and list fields
- [x] E1.5 Markdown and wikilinks
- [x] E1.6 Project templates and template functions
- [x] E1.7 Query engine spike (decision task)
- [x] E1.8 Query blocks
- [x] E1.9 Calculated fields
- [x] E1.9a Multi-type query blocks
- [x] E1.9b Per-query templates
- [x] E1.10 `--scope` and Document export
- [x] E1.11 Git context and content hash
- [x] E1.12 Captions, cross-reference numbering and images
- [ ] E1.13 Bundled Mermaid and offline guarantee — *deferred 2026-10-04*
- [x] E1.13a Reference lists
- [x] E1.13b Citation templates
- [ ] E1.14 Epic close-out: docs and first binary release
- [x] E2.1 Validate design decisions
- [x] E2.2 `validate` command, report model and load errors
- [x] E2.3 Schema conformance (built-in)
- [x] E2.4 Content and links (built-in)
- [x] E2.5 Grammar: sub-queries, `value_of`, `missing`, `not_in`
- [x] E2.6 Grammar: `aggregation` and `duplicated`
- [x] E2.7 Grammar: transitive links and `code_blocks`
- [x] E2.8 Rule files
- [x] E2.9 Report formats
- [x] E2.10 Report templates
- [x] E2.11 Epic close-out: gate in CI, docs, release
- [x] E3.1 One loader
- [x] E3.2 Sources and content files
- [ ] E3.3 Values and positions from the source
- [ ] E3.4 Entity-level history
- [ ] E3.5 Epic close-out

## Shape of the plan

- **Stage 0 — Skeleton CLI.** Every command from DES-0019 exists as a
  stub with its real flags and help text.
- **Stage 1 — CI.** GitHub Actions runs tests, lint, a reproducible-build check,
  and acceptance commands against the pinned example repo.
- **Epics — one per command.** Epic 1 is `export website`. Each task adds
  something visible to the same generated site, so progress can be checked by
  running the command and looking at the output.
- **Later epics** are outlined only; each gets detailed planning when the
  previous epic closes, using what was learned.

## Rules for Claude Code

1. **One epic = one branch; one task = one PR into it.** Each epic has a
   branch `epic/<id>-<slug>` (e.g. `epic/E2-validate`), cut from `main`, in
   every repository the epic touches: `provenance`, `provenance-ddf`,
   `provenance-example` and `provenance-website`. Each task is a branch
   `task/<id>-<slug>` cut from the epic branch, and its PRs target the epic
   branch in each repository. Don't start the next task until the previous
   one is merged into the epic branch. A task's PR ticks the task off in
   this plan's checklist, so the tick merges with the work, not in a PR
   of its own. While the epic is open, the example
   pin may point at a commit on the example's epic branch (`make
   bump-example EXAMPLE_BRANCH=epic/<id>-<slug>`). The epic merges into
   `main` once, at its close-out, in each repository, with the pin moved to
   the example's `main`. Plan-only changes go to `main`
   directly.
2. **Every task ends with something Matt can run.** The PR description has a
   *Check it yourself* block with exact commands and the expected result.
   Commands in this plan assume `make build` has produced `bin/provenance`,
   `alias prov=<path-to>/provenance/bin/provenance`, and a working directory
   inside a `provenance-example` checkout.
3. **Every task adds automated checks:** Go unit tests, plus a step in
   `scripts/acceptance.sh` that runs the new behaviour against the example repo.
   CI runs both.
4. **The example repo is the fixture.** CI checks out
   `dynamatt/provenance-example` at the commit pinned in
   `testdata/example-repo.ref`. If a feature isn't exercised by the example
   repo, the task first adds content to `provenance-example` (its own PR), then
   bumps the pin. Synthetic fixtures are for edge cases only, never instead of
   the example.
5. **Design drift is written back, not left in code.** If implementation needs
   behaviour the design docs don't specify, contradicts them, or settles
   something marked OPEN, the same PR (a) updates the design in
   `provenance-ddf` (its requirement and design elements; the Notion design
   pages are retired) marking the item DECIDED with a date, and (b) updates `provenance-website` where the change
   is user-visible (CLI reference, architecture page). The PR description lists
   these under *Design changes*. If the change is more than a clarification,
   stop and ask Matt before implementing.
6. **Determinism.** No map-iteration order, wall-clock timestamps or absolute
   paths in any output. Running the same command twice on the same commit
   produces byte-identical results.
7. **Exit code contract from day one** (DES-0018): `0` success, `1`
   expected failure reported, `2` tool/usage error.

## Decisions

**All proposals below approved by Matt, 2026-09-26.** S0.4 records them in the
design docs; the Datalog engine is still settled by spike E1.7.

| Decision | Proposal | Needed by |
| --- | --- | --- |
| CLI framework | `cobra` — nested subcommands (`sign verify`, `component add`, `verify artifact`), consistent help, can generate the website's CLI reference. Alternative: stdlib `flag` with hand-rolled dispatch (zero dependencies, more code to own). | S0.1 |
| Stub behaviour | Unimplemented commands print `<command>: not implemented yet` to stderr and exit `2`. | S0.1 |
| Version command | Add `provenance version` / `--version` (version, commit, Go toolchain). Not in Detailed Design §2 today; needed for bug reports and later by `verify artifact`. | S0.1 |
| Product docs timing | Requirements Spec §7b schedules product docs *after* core build. This plan starts a CLI reference section on the Hugo site now (Hugo already satisfies "off-the-shelf docs tool"). Update §7b to match. | S0.3 |
| Default `--out` | `./_site`. Export only cleans a non-empty output folder if it contains a `.provenance-export` marker it wrote itself; otherwise it refuses (exit 2), so it can never delete a user's folder. | E1.1 |
| Fallback rendering | When a type has no project template, export uses a built-in generic template (field table in schema order + body). Not currently in the design. | E1.3 |
| Template naming and data model | `templates/<TypeName>.tmpl`. Fields exposed to templates as PascalCase accessors (`.Title`, `.Statement`, `.Implements`), as the example's illustrative Requirement template already assumes — rather than `.Fields.title`. | E1.6 |
| Datalog engine | Resolved by spike task E1.7. Hard requirement: stratified aggregation (Detailed Design §5). Leaning hand-rolled semi-naive evaluator if no library supports that cleanly — small, auditable, fully owned. | E1.7 |
| Git access | `go-git` (pure Go) rather than shelling out to `git`: keeps the host's git version out of the validated configuration and the binary self-contained. Revisit only if performance on large histories is a real problem. | E1.11 |

## Design inconsistencies found while planning

Resolved by taking the most recent decision, judged from the design docs' own
narrative (later passes explicitly note what they superseded). S0.4 has applied
the edits to the design pages.

- **Template HTML constraint — RESOLVED: deferred.** High-Level Design §4.6 is
  marked "NARROWED, then deferred" and Detailed Design §7 calls the constrained
  vocabulary "the idea explored earlier", so both postdate the Requirements
  Spec §7 sentence. v1 templates may use unconstrained HTML. *Fix:*
  Requirements Spec §7 — replace "is built for the website exporter regardless"
  with the deferral and its deferred bill.
- **Query-block example — RESOLVED: word operators.** High-Level Design §4.3a
  itself records that the grammar was "consolidated to one grammar (Detailed
  Design §6) once noticed"; its example block just wasn't updated. *Fix:*
  High-Level Design §4.3a example → `operator: equals`, and replace the stale
  `component`/`COMP-04F2` filter with the `status: approved` example used in
  DOC-0001.
- **`--scope` grammar — RESOLVED: path only.** Detailed Design §2 says a
  compact CLI string syntax "was considered and dropped", which is the later
  decision. *Fix:* Detailed Design §6 opening — describe `--scope` as a path to
  a query file or entity, and the HTTP API as `where=` JSON /
  `POST /api/query`, dropping `q=`.
- **Reference Count `direction` — RESOLVED: removed.** Detailed Design §6
  explicitly says it "narrows what Requirements Spec §6 originally specified".
  *Fix:* Requirements Spec §6 — drop `direction` from the parameter list and
  example; note that incoming checks use the link's `reverse_name`.
- **Gate output formats — RESOLVED (Matt, 2026-09-26).** Add
  `validate --format json|junit|sarif` to Detailed Design §2, keeping exit
  codes as the only guaranteed contract.
- **Export exit codes — RESOLVED (Matt, 2026-09-26).** `0` written, `2` for
  anything else; export never returns `1`, because judging content is
  `validate`'s job.

## Stage 0 — Skeleton CLI

### S0.1 Go module and command skeleton

**Deliverables:** `go.mod` (`github.com/dynamatt/provenance`, pinned `go` and
`toolchain` directives); `cmd/provenance/main.go`; `internal/cli/` registering
every command and subcommand from Detailed Design §2 with its real arguments,
flags and help text, each returning a not-implemented error; central exit-code
handling in one package; `provenance version`.

**Tests:** every command exists; flags parse; stubs exit 2; unknown flags and
missing arguments exit 2.

**Check it yourself:**

```bash
go build -o bin/provenance ./cmd/provenance
prov --help                      # lists every command from Detailed Design §2
prov export --help               # shows <format>, --scope, --out
prov export website; echo $?     # "export: not implemented yet" then 2
prov version
```

### S0.2 Build tooling and example-repo fixture

**Deliverables:** `Makefile` with `build` (`CGO_ENABLED=0`, `-trimpath`, fixed
`-ldflags` with version injected), `test`, `lint` (`gofmt`, `go vet`,
`staticcheck`), `acceptance`, `example` and `ci` (all of the above).
`testdata/example-repo.ref` holding a `provenance-example` commit SHA;
`make example` clones it into `.cache/example` at that SHA; `make bump-example`
moves the pin to `main`. First `scripts/acceptance.sh`: runs `--help` and
`version` inside the example checkout.

**Check it yourself:**

```bash
make ci                                   # passes
make build && sha256sum bin/provenance
cp -r . /tmp/prov2 && (cd /tmp/prov2 && make build && sha256sum bin/provenance)   # same hash
```

### S0.3 CLAUDE.md, PLAN.md and website CLI reference

**Deliverables (provenance):** `CLAUDE.md` (layout, make targets, the rules
above, links to this project's Notion design pages and the example/website
repos); `PLAN.md` (export of this page); `tools/gendocs` that writes one
Markdown page per command from the CLI definitions, so reference docs can't
drift from `--help`.

**Deliverables (provenance-website):** `content/docs/cli/` generated from
`gendocs`, with each command marked *Not yet implemented* until its epic lands;
replace the nav's placeholder "Docs" link (currently pointing at
`provenance/tree/main/docs`); update README's "Known placeholders". Markdown
lint stays green.

**Check it yourself:** `hugo server` in the website repo → `/docs/cli/` lists
every command with its flags.

### S0.4 Design reconciliation — DONE 2026-09-26

Applied the fixes from *Design inconsistencies* and the approved *Decisions* to
the Notion design pages (each change marked DECIDED 2026-09-26). No code.

**Check it yourself:** Notion page history shows each change, dated and marked
DECIDED.

## Stage 1 — CI

### S1.1 GitHub Actions CI workflow

**Deliverables:** `.github/workflows/ci.yml` on push and pull request:
`setup-go` from `go.mod`, `make lint test build`, `make example`,
`make acceptance`; binary uploaded as an artifact with its SHA-256 in the job
summary. An `actionlint` job, matching the website repo.

**Check it yourself:** open a PR → green check. Push a commit that breaks a
unit test → red. Push one that breaks an acceptance step → red, and the log
names the failing command.

### S1.2 Reproducible-build check

Detailed Design §4 requires "a CI job that proves it". Doing it now, before
there's a frontend bundle, is cheap and catches regressions from the first line
of real code.

**Deliverables:** job that builds linux/amd64, darwin/arm64 and windows/amd64
on two different runners with different checkout paths, then compares
SHA-256s; publishes the hashes in the job summary; fails on any difference.
Build recipe documented in `docs/building.md`.

**Check it yourself:** job summary shows matching hash pairs. Locally, add a
timestamp to `-ldflags`, rebuild twice → hashes differ (then revert).

## Epic 1 — `export website`

**Goal:** `provenance export website` produces the full DHF static site for
`provenance-example`: every entity, links and reverse links, Documents rendered
from live query blocks and wikilinks, calculated fields, git provenance stamps,
bundled diagrams, zero network access, byte-for-byte deterministic.

**Why first:** it pulls in almost the whole core (parsing, schema, graph, query
engine, templates, content hash) that every later command reuses, and its
output is easy to eyeball.

**Standing acceptance for every E1 task:** exporting twice gives an empty
`diff -r`; the golden-site test (from E1.3) passes, and the PR shows the golden
diff so the rendering change can be reviewed directly.

### E1.1 Exporter interface and output folder

**Deliverables:** exporter interface (entity graph + templates in → output
folder out) with a registry, so formats aren't special-cased (Requirements
Spec §7). `website` registered; `pdf` and `docx` recognised but report "not
available in this release" and exit 2; unknown formats exit 2. Repo-root
discovery (walk up to `.component`); `.component` loaded. `--out` handling per
Detailed Design §2. Writes `index.html` showing the component name and code.

**Check it yourself:**

```bash
prov export website && open _site/index.html   # "NeuroPulse Implantable Stimulator System (NEURO)"
prov export pdf; echo $?                        # not available in this release, 2
mkdir notmine && touch notmine/x && prov export website --out notmine; echo $?   # refuses, 2
```

### E1.2 Entity discovery and parsing

**Deliverables:** repository walk skipping configuration folders (`schema/`,
`rules/`, `templates/`, `.signatures/`, `assets/`, `.git`, the output folder);
any `.md` with YAML frontmatter containing `id` and `type` is an entity,
everything else (e.g. `README.md`) is ignored. Parser keeps the raw file bytes
(needed for the content hash in E1.11). Duplicate IDs fail with exit 2 naming
both files, since links can't be rendered unambiguously. `index.html` lists all
entities grouped by type.

**Check it yourself:** index lists USR-0001–0002, REQ-0001–0003, DES-0001,
SEV-0001–0003, OCC-0001–0003, RSK-0001, VER-0001–0002, EVD-0001–0002, ECO-0001
and DOC-0001. Copy `REQ/REQ-0001.md` to `REQ/copy.md` → exit 2 naming both
paths.

### E1.3 Schema loading, default entity pages, golden test

**Deliverables:** parse `schema/*.yaml` and `schema/enums/*.yaml` into the
Detailed Design §5 type model (`string`, `text`, `number`, `date`, `boolean`,
named enums, `link`, `list`, `calculated`, `body: true`). Map frontmatter and
body onto typed values. Only problems that prevent loading fail export
(exit 2); rule-level checks belong to `validate` (Epic 2). Built-in fallback
template → one page per entity (`_site/entities/<ID>.html`). Golden-site
snapshot of the example export in `testdata/golden/`, with
`make golden-update`.

**Check it yourself:** REQ-0001's page shows status `approved`, order `1`, and
the shall-statement from the body. Change a field's `type` in
`schema/Requirement.yaml` to `strnig` → exit 2 naming the file and field.

### E1.4 Links, reverse links and list fields

**Deliverables:** `link` fields render as hyperlinks; incoming facets are
derived from `reverse_name` and shown on target pages; unresolved targets
render with a visible *unresolved* marker (not an error — Reference Validity is
Epic 2). `list` fields render as tables.

**Check it yourself:** REQ-0001's page shows *verified by: VER-0001* even
though REQ-0001's file never mentions it (the link is declared on the protocol,
the later artefact; changed 2026-09-28, Matt). EVD-0001 shows its equipment
table. Change a link to `REQ-9999` → marked unresolved, exit 0.

### E1.5 Markdown and wikilinks

**Deliverables:** `goldmark` with a `markdown` template function (Detailed
Design §7); a wikilink extension for `[[ID]]`, `[[ID|label]]`, `[[ID#field]]`
and `![[ID]]` (High-Level Design §4.3a). Embeds render the target through its
type template. Embed cycles fail with exit 2 naming the cycle, rather than
publishing a broken document.

**Check it yourself:** DOC-0001 (still on the fallback template) shows
`[[REQ-0001]]` as a link, `[[REQ-0002#title]]` replaced by REQ-0002's title as a
link, and REQ-0003 embedded in full. Add `![[DOC-0001]]` to DOC-0001 → exit 2
with the cycle path.

### E1.6 Project templates and template functions

**Deliverables:** load `templates/<TypeName>.tmpl` (`html/template`), falling
back to the built-in template. **Site-level overrides (added 2026-09-27,
Matt):** `templates/style.css` (stylesheet), `templates/_layout.tmpl` (page
layout wrapping every page) and `templates/_index.tmpl` (the site's main page)
each replace the built-in one independently; any not provided falls back to
the built-in. The underscore prefix keeps them from colliding with a type
template, since CamelCase type names cannot start with `_`. A project template
that fails to parse or execute exits 2 naming the file and line. The layout and
index data contracts are recorded in Detailed Design §7. Template functions:
`markdown` and link helpers. **Relative heading depth (changed 2026-09-28,
Matt):** templates and Markdown write ordinary `<h1>`–`<h6>`, and the engine
shifts headings by render depth after rendering, keeping attributes (Detailed
Design §7). There is no `heading N` function. Data model per Detailed Design
§7. **Example-repo PR:** rename `Requirement.tmpl.illustrative` →
`Requirement.tmpl`, rewritten to plain `<h1>`/`<h2>` and link helpers; add a
`templates/style.css`, `templates/_layout.tmpl` and `templates/_index.tmpl` so
the example exercises every override; update `templates/README.md` and the
README's "nothing in templates is functional" note; bump the pin. The built-in
fallbacks keep unit-test coverage.

**Check it yourself:** REQ pages use the project template ("Implements"
heading); DES pages still use the fallback. REQ-0003 embedded in DOC-0001
renders its headings deeper than on its own page. Every page carries the
example's layout and stylesheet, and the index is the example's own. Delete
`templates/style.css`, re-export → the built-in stylesheet comes back while the
layout stays the example's. Break a `{{` in `templates/_layout.tmpl` → exit 2
naming the file and line.

### E1.7 Query engine spike (decision task)

**Deliverables:** timeboxed comparison of candidate Datalog engines (existing
Go libraries vs a hand-rolled semi-naive evaluator), each implementing three
fixed cases: DOC-0001's query block, one Query Assertion using `via:`, and
`MAX(failure_modes[].row_rating)` (stratified aggregation). Benchmarked on a
synthetic 10k-entity graph. High-Level Design §4.5 and the Detailed Design §5
engine-choice note updated to DECIDED.

**Check it yourself:** read the comparison in the PR and the Notion update;
`go test ./internal/query/...` runs the three cases for the chosen engine.

### E1.8 Query blocks

**Deliverables:** the Detailed Design §6 condition grammar as one shared module
(word operators, implicit AND, `any_of`, `{field: …}` values, list sub-fields),
compiled to Datalog. Fenced `query` blocks in bodies resolved live with `from`,
`where`, `order_by` and `render: full | field:<name> | id`. Errors point at the
file and block and list valid operators.

**Check it yourself:** DOC-0001's *Requirements* section lists approved
requirements ordered by `order`. Set REQ-0002 to `draft` (uncommitted) and
re-export → it disappears. Change a block's operator to `"="` → exit 2 listing
valid operators.

### E1.9 Calculated fields

**Deliverables:** Excel-style formula parser (Detailed Design §5) compiled to
Datalog built-ins: arithmetic, comparison, `AND`/`OR`/`NOT`, `IF`, `MAX`,
`MIN`, `SUM`, `COUNT`, `AVG`, `ISBLANK`, dot access across a `link`, `[]` list
extraction, `calculated` sub-fields inside `list` rows.

**Check it yourself:** RSK-0001 shows `row_rating` for each failure mode and
`overall_risk_rating` equal to the largest — check by hand from the linked
SEV/OCC scores. Change an OCC score → both update.

### E1.9a Multi-type query blocks (added 2026-10-03, Matt)

**Deliverables:** a query block's `from` takes one type or a list of types
(Requirements Spec §4a), and results of every type sort together by
`order_by`, then ID. Decide how a field present on only some of the selected
types behaves and record it in Detailed Design §6. Proposal: a name used in
`where`, `order_by` or `render: field:` must exist on at least one selected
type, with the same field type wherever it is declared (otherwise exit 2
naming the types); on an entity whose type lacks it, the field is empty,
exactly like an unset field (`exists` fails, `not_equals` holds, it sorts
last, `render: field:` marks it *empty*). Embeds, heading shifts and cycle
detection are unchanged. **Example-repo PR:** add DOC-0002 *Risk Management
Summary* with a block `from: [Risk, Requirement]` ordered by `order`; bump
the pin. **Website:** the query-block page documents the list form.

**Check it yourself:** DOC-0002 interleaves RSK-0001 and the requirements by
`order` (ties by ID). Add `where: {field: hazard, operator: exists}` → only
RSK-0001, since Requirement has no `hazard`. Misspell it `hazrd` → exit 2
naming Risk and Requirement.

### E1.9b Per-query templates (added 2026-10-03, Matt)

**Deliverables:** named presentation templates (Requirements Spec §7).
Proposal, to record in Detailed Design §7: `templates/<name>.tmpl` where
`<name>` is lower-case kebab-case (`requirement-checklist`), so it cannot
collide with a CamelCase type template or an `_`-prefixed site override. A
named template receives the same entity data as a type template and works
the same way (functions, heading shift, embeds), and is parsed up front like
every template. Query-block keys: `template: <name>` for every result, and
`templates: {<Type>: <name>, …}` per type, unlisted types keeping their
default. Exit 2 at the block's file and line for an unknown template, a
`templates` key that is not one of the block's `from` types, `template` and
`templates` together, or either with `render: id` or `field:`. A named
template that fails to execute is reported at its own file and line, as type
templates are. Choosing a template for `![[ID]]` embeds is out of scope;
raise it if the example needs it. **Example-repo PR:** add
`templates/requirement-checklist.tmpl` and `templates/risk-summary.tmpl`,
used by DOC-0002 through `templates:`; DOC-0001 keeps the default
Requirement template; bump the pin. **Website:** publish the query-block
page's *Custom templates* section and the `template`/`templates` keys
(drafted by Matt, 2026-10-03), checked against what shipped.

**Check it yourself:** DOC-0002 shows requirements as checklist rows and
RSK-0001 as a summary, while DOC-0001 still shows full requirements. Rename
`templates/risk-summary.tmpl` → exit 2 naming DOC-0002's line and the
missing template. Remove `templates:` → every result uses its type's
default.

### E1.10 `--scope` and Document export

**Deliverables:** scope resolution (Detailed Design §2): an entity file
resolves via the dependency walk (itself plus everything pulled in by query
blocks and embeds); a Document scope renders that Document through its own
template as the site's main page; a standalone `from`/`where` YAML file scopes
to its result set. References to out-of-scope entities render as the plain
citable ID (Requirements Spec §7). The dependency walk is a reusable
function — E1.11's `last_changed_sha` needs it. **Example-repo PR:** add
`scopes/approved-requirements.yaml`.

**Check it yourself:**

```bash
prov export website --scope DOC/DOC-0001.md --out _doc          # _doc/index.html is the SRS
prov export website --scope REQ/REQ-0001.md --out _req          # only REQ-0001; "USR-0001" as plain text
prov export website --scope scopes/approved-requirements.yaml --out _q
```

### E1.11 Git context and content hash

**Deliverables:** git access via `go-git` (Detailed Design §2). **Content
hash**: the exact byte set, file ordering and path encoding written into
Detailed Design §4 as a precise algorithm (this is the audit-critical
definition and the one `verify content` will check); `-dirty` suffix when the
working tree differs from HEAD. **`last_changed_sha`** per Detailed Design §4's
three rules, using the E1.10 walk. **Revision history** from git log and tags
(Requirements Spec §7). Template context gets `git_sha`, `content_hash`,
`last_changed_sha`; the fallback template shows them in a footer. Also wires
`verify content` (same function — a small piece of the `verify` epic pulled
forward so the footer can be cross-checked).

**Check it yourself:**

```bash
prov export website && prov verify content    # footer hash == printed hash
echo " " >> DES/DES-0001.md                   # uncommitted edit
prov verify content                           # ...-dirty
```

Then commit an edit to REQ-0001 → DOC-0001's `last_changed_sha` moves; commit
an edit to DES-0001 (not pulled into DOC-0001) → it doesn't.

### E1.12 Captions, cross-reference numbering and images

**Deliverables:** two-pass render — number captions in document order per
render, then resolve inline references to them as "Figure N". **Captions at
the point of use (changed 2026-10-04, Matt, from review):** a caption belongs
to where a figure, table or equation is used, not to the asset, so it is a
` ```caption ` block (kind, id, Markdown text) right after the image, table,
embed or fenced block it captions, referred to with `[[#id]]`.
**Caption kinds fixed (decided 2026-10-04, Matt, from review):** figure,
table (captioned above) and equation, with no configuration file; how a
caption looks is the stylesheet's concern (Requirements Spec §7). **Images (added
2026-10-04, Matt):** image files (SVG, PNG, JPEG, GIF, WebP) referenced from
Markdown are copied into the site; a URL or a missing file exits 2.
Equations render with E1.13's bundled renderers. **Example-repo PR:** images
in each format in `assets/`, two captioned figures and a captioned table in
DOC-0001, a captioned photo in EVD-0001.

**Check it yourself:** DOC-0001 shows "Figure 1" under the control loop and
in the sentence referencing it, "Table 1" above the limits table. Caption a
second image above the first → numbers move, text follows.

### E1.13 Bundled Mermaid and offline guarantee

**Deferred (2026-10-04, Matt):** picked up when
diagrams are needed. Until then `mermaid` blocks export as their marked
source (validate's BlockLanguage rule reports them), and E1.14 does not wait
for it.

**Deliverables:** Mermaid JS checked in under an embedded asset folder with a
pinned version and SHA-256 verified at build; copied into the site; `mermaid`
fences render client-side. Acceptance step fails if any `src`/`href` in the
output loads an asset from a remote host. Reproducible-build job stays green
(the asset is a checked-in file, not a bundler output). draw.io stays deferred
until the example repo has a draw.io asset. **Example-repo PR:** add a Mermaid
diagram to DES-0001.

**Check it yourself:** turn networking off, open
`_site/entities/DES-0001.html` → diagram renders.

### E1.13a Reference lists (added 2026-10-04, Matt; revised 2026-10-05)

**Deliverables:** a document lists every reference it makes, internal and
external (`provenance-ddf` USR-0022, REQ-0127..0134, DES-0046, DES-0047),
without its author adding anything to the Markdown: like revision history,
the list is the template's. External sources are ordinary entities of a
project-defined type (e.g. `Reference`, `REF-0001`), cited with `[[ID]]`;
there is no new link syntax.

- **Citations** are the `[[ID]]`, `[[ID|label]]` and `[[ID#field]]`
  wikilinks in Markdown rendered on the page, including inside embeds and
  query results rendered in full. Not citations: `![[ID]]` embeds, link
  fields rendered with `link`, and `[[#id]]` or a reference to a captioned
  entity numbered on the page. An entity cited twice is listed once.
- **Two renders per page.** The first collects the citations; the second
  renders with them known. Output is the second render's.
- **`.Citations`** on the page's own entity, in its type template: the cited
  entities as entity maps, in order of first citation, each with
  `.CitationIndex` (1-based position among all) and `.TypeCitationIndex`
  (position among cited entities of its type). It is empty when the entity
  is embedded or shown by a query, so a list in `Document.tmpl` appears once
  per page. The layout receives the same `.Citations` (empty on the site
  index, which is about no entity), so a project may list references on every page from
  `_layout.tmpl` instead; a unit test covers this, the example does not use
  it. Built-in templates show no list, so existing output is unchanged.
- Out of scope: the plain ID, with title. Missing: marked *unresolved*.
  Neither fails export (Reference Validity is `validate`'s job).

**Example-repo PR:** `schema/Reference.yaml` (title, author, publisher, year,
identifier, url, accessed); `REF/` with ISO 14971:2019, IEC 60601-1 and one
journal paper; cite them in DOC-0001's prose and in REQ-0003 (which DOC-0001
embeds, so collection through embeds is exercised); a new
`templates/Document.tmpl` (the built-in page plus a *References* section at
the end: *Internal* lists cited Documents, *External* cited `Reference`
entities, each only when it has entries (changed 2026-10-06, Matt); DOC-0001
cites DOC-0002); bump the pin. **DDF:** add the
section to its own `Document.tmpl` once this ships.

**Check it yourself:** DOC-0001 ends with *References* split into
*Internal* (DOC-0002) and *External* (the three
`REF-` entities), each in first-citation order, with nothing added to its
Markdown; the citation inside the embedded REQ-0003 is listed. Cite REF-0002
again earlier in DOC-0001 → it moves up. REQ-0003's own page has no list
(Requirement.tmpl doesn't add one). Export with `--scope` on DOC-0001 →
out-of-scope entries show as plain IDs.

### E1.13b Citation templates (added 2026-10-04, Matt; revised 2026-10-07)

**Deliverables:** how an inline citation renders is the project's too
(REQ-0132). `templates/_cite.tmpl` (site override) renders every `[[ID]]`
and `[[ID|label]]`, receiving the cited entity as in `.Citations`, with
`.CitationIndex`, `.TypeCitationIndex` and `.CitationLabel` (the label, or
empty), from E1.13a's first render, so it can render by type or by field.
`.CitationLabel`, not `.Label`: entity maps carry a key per field, and the
example's SeverityLevel and OccurrenceLevel already declare `label`; the
three keys are reserved (decided 2026-10-07, Matt). `[[ID#field]]` and
`[[#id]]` keep their current rendering. The file's final line break is not
part of the citation. The site index has no citations and renders them as
today. `_cite.tmpl` is an input of every page that cites anything, and with
it the cited entities are too. Without `_cite.tmpl`, citations render
exactly as today, so existing golden output is unchanged. Record in DES-0046
and the template files design (DES-0031).

**Example-repo PR:** `templates/_cite.tmpl` rendering a `Reference` as a
bracketed number linked to its own page, not to its list entry, so it works
on pages without a list (decided 2026-10-07, Matt), with a label as a
locator (`[[REF-0001|clause 7]]` → `[3, clause 7]`), and anything else as
its linked ID or label; DOC-0001 cites ISO 14971 by clause; bump the pin.

**Check it yourself:** DOC-0001 shows `[1]`, `[2]`, `[3, clause 7]` where it
cites the `REF-` entities, matching the *External* list, and IDs everywhere
else. REQ-0003's own page shows `[1]` for REF-0002. Delete `_cite.tmpl` →
citations show IDs again, output otherwise unchanged.

### E1.14 Epic close-out: docs and first binary release

**Deliverables:** website `export` reference page complete (usage, scope
files, template authoring (type templates, named presentation templates,
citation templates (`_cite.tmpl`, `.CitationLabel`) and `.Citations`, and the stylesheet, layout and index
overrides) and template functions, context
variables);
architecture page updated where implementation refined the design; example-repo
README updated for what now works. GitHub release `v0.1.0-alpha` built by CI
with a `SHA256SUMS` manifest (format recorded in DES-0025 — it's what
`verify artifact` will check later).

**Check it yourself:** download the release binary on a second machine,
`sha256sum` matches `SHA256SUMS`, and `export website` on the example repo
matches the golden site.

## Epic 2 — `validate`

> **Status: decisions agreed with Matt 2026-10-11.** Nothing below is started
> until the plan PR is merged.

**Goal:** `provenance validate` is the merge gate (REQ-0054). It loads the
repository, runs the built-in checks and every rule in `rules/`, and reports
every violation (REQ-0038) with its file and line, in text, JSON, JUnit or
SARIF (REQ-0059), or through a project's own report template. It exits `1`
while any error-severity violation exists and `0` otherwise, warnings
included (REQ-0055). The example repository runs it as a required check on
its pull requests, from a released binary (REQ-0060).

**A rule is a query.** A rule file is a query block's `from` and `where`
with a `severity` and a `message`; every entity, row or link the query
matches is a violation. There are no hard-coded rule types: what the
library of rule types did is done by extending the shared condition grammar
(DES-0015), so query blocks, `--scope` files and rules all gain it, and a
rule's violations are exactly what its query shows when embedded in a
document.

**Why next:** it is the product's reason to exist at the merge gate, and it
reuses almost all of Epic 1: entity loading, the schema, the typed model,
the condition grammar and Datalog engine, and the dependency walk that
already finds every wikilink, query block, caption and image.

**Standing acceptance for every E2 task:**

- `validate` on the clean pinned example exits `0`, and its text report is
  byte-identical to `testdata/validate/clean.txt`. The warnings it prints
  are part of that file, so a new warning shows in the PR diff.
- Each new check is proven in `scripts/acceptance.sh`. The step breaks one
  example entity in the checkout, expects exit `1` and the exact report
  line, then restores the file. This is the pattern E1.8 used for query
  errors.
- Running it twice gives byte-identical reports. Violations are sorted by
  file, line, rule ID and message, never by map order.

### Decisions (agreed with Matt 2026-10-11)

| # | Decision | Agreed |
| --- | --- | --- |
| D1 | A field's `default:` (OPEN since Detailed Design §5) | **Removed from the schema DSL.** The file is the whole record: an omitted field is blank everywhere (export, queries, rules, editor). A field that must have a value says `required: true`. `default:` becomes an unknown schema key. The example's ten defaults go; the fields they were on become `required: true` (every example entity already sets them). |
| D2 | Load errors (unparsable frontmatter, missing `id`/`type`, a duplicate ID, an unknown type, a broken schema or rule file) | **Violations, not tool errors:** reported with file and line, exit `1`. Exit `2` is kept for the tool and its invocation (bad flags, unreadable repository, I/O). A broken schema skips every rule, because rules are checked against it, and the report says so; a broken rule file skips only itself. `export` keeps exiting `2` on the same problems. |
| D3 | Checks that need no rule file | **Built in, IDs under `schema/…`, `content/…` and `links/…`, not configurable.** Errors: schema conformance, ID format, everything that would stop `export`, a block this version can only show as source (DES-0036), and link integrity (unresolved links and inline references, a target type outside the field's `target:`, `[[ID#field]]` naming a field the target lacks), which replaces the Reference Validity rule type. Warnings, against clutter: an unused enum, record or entity type (no entities and no link field targeting it), an unused named template, a `_`-prefixed template that is no site override, a type template naming no declared type. |
| D4 | Rules are queries (replaces the fixed rule-type library; REQ-0035 reworded) | **A rule file is `severity`, `message`, `from` and `where`; every match is a violation.** The grammar gains (D5): sub-queries on list and link fields, `value_of` and outer references, `missing`, `not_in`, sub-queries on `from` (*revised 2026-10-15: replaces `duplicated`*), `aggregation`, transitive links, and a derived `code_blocks` list. REQ-0035 becomes "the rule language shall be built into the binary; projects configure rules in it and cannot extend it." REQ-0041–0047, REQ-0049 and REQ-0052 are reworded from rule types to what the language can express; REQ-0050 moves to the built-in checks (D3). Blanks never match a comparison, so a rule that must catch missing data says so with `missing`. |
| D5 | The condition grammar (DES-0015) | A test is `field`, `operator` and `value` (a literal) or `value_of` (a field). Inside a sub-query, names are the item's; `^name` is the enclosing entity's field (`^^` two levels out). A list or link field may instead take `where:` (tested on each row or linked entity; `transitive: true` follows a link to any depth) and optionally `aggregation: {type: count\|sum\|min\|max\|avg, field: …}`, whose single value `operator` and `value`/`value_of` then test, with the formula language's blank semantics (DES-0012). `{list, subfield}` and `{via, field}` are removed in favour of sub-queries; conditions inside one sub-query are about one item. Nested `any_of` is allowed everywhere. New operators: `missing`, `not_in`. A sub-query may take `from: <types>` instead of `field`, holding when some entity of those types matches; with `^` it compares an entity with others no link reaches (*revised 2026-10-15: replaces `duplicated`, whose comparison set the query now states*). `code_blocks` is a derived list on every entity with `language` (`none` for a bare fence) and its line. |
| D6 | A rule file's name and ID | **The file name is the ID:** `rules/<id>.yaml`, lower-case kebab-case; a rule file has no `id:` key. |
| D7 | Rules this version can't check (Signature Presence, Content Frozen After Release) | **Removed from the example and the DDF until the `sign` and `release tag` epics,** which define the facts (signatures, released content) they query. REQ-0048 and REQ-0051 stay as they are. |
| D8 | Where a violation is placed | The matched field's line; a `missing` field or absent aggregated field at the entity's `id:` line; a matched list row at the row; a matched link target at the line in the *source* entity where that link value is written; an `aggregation` at its field; a sub-query on `from` at the entity's `id:` line, naming the entities found (*revised 2026-10-15*); a cycle on each member's link field; an `any_of` at its first matching test in file order; a rule-file problem at its line in the rule file. A rule's violations are always in the matched entity's own file. |
| D9 | Report formats | **text:** `path:line: error\|warning [rule-id] message (ENTITY-ID)`, a line for skipped rules, then `N errors, M warnings`. **json:** `version: 1`, `violations[]` (`rule`, `severity`, `message`, `entity`, `path`, `line`), `summary` (`errors`, `warnings`, `rules_skipped`). **junit:** one test case per rule and built-in check; errors fail it, warnings go to `system-out`. **sarif:** 2.1.0, one run, a rules entry per rule and built-in check, one result per violation, repository-relative locations. No timestamps, no absolute paths. Field names are recorded in the DDF; only the exit code is guaranteed (DES-0018). |
| D10 | Custom reports | **`validate --output <name>`** renders `templates/reports/<name>.tmpl` (`html/template` when the name ends `.html`, otherwise `text/template`) with `.Violations`, `.Rules` (every rule and built-in check, with its violations, so passing ones can be listed), `.Summary` and the git stamps. The exit code is unchanged. |
| D11 | Where reports go | **`--output <name>[=<file>]`, repeatable**, names a format (`text`, `json`, `junit`, `sarif`) or a report template (D10) and writes it to the file, creating its folders and never emptying one, or to standard output when no file is given. One run writes every report: at most one goes to standard output, no file twice, and nothing is written unless all render. With no `--output`, text goes to standard output. A report template can't take a format's name. A report goes into the exported site by running `export` first (it empties its folder) and then `validate --output <name>=_site/…`. (*Revised 2026-10-25: replaces `--format`, `--report` and `--out`, which took several runs for several reports.*) |

Not in this epic: composed-graph validation across components (REQ-0025,
the `component` epic); live feedback in the editor (REQ-0053, `serve`);
`fmt --check` at the gate (the `fmt` epic); a validation page built by
`export` itself; stricter reviewers for schema and rules (REQ-0034, a host
setting, documented in E2.11).

**Order:** the built-in checks first (E2.2–E2.4), then the grammar
(E2.5–E2.7), then rule files (E2.8), so the gate never reads a rule written
in grammar it doesn't have. Until E2.8 the example's rule files are not read;
each grammar task proves itself through query blocks in a review document
the example gains in E2.5.

### E2.1 Validate design decisions

**Deliverables:** the decisions above written into `provenance-ddf`, each
marked DECIDED 2026-10-11:

- DES-0010: no `default:`.
- DES-0015: the grammar (D5).
- DES-0016: rule files as queries (D4, D6), with one worked rule per former
  rule type.
- DES-0018: the load-error exit code (D2).
- REQ-0035, REQ-0041–0047, REQ-0049, REQ-0050, REQ-0052 reworded (D4).
- A new design element for the built-in checks (D3), the violation
  placement (D8), the report formats (D9) and report templates (D10, D11).
- The DDF's own `rules/` rewritten in the new form: the two Signature
  Presence rules and `released-requirement-frozen` removed (D7),
  `cross-references-resolve` removed (D3).

`internal/schema`'s `Default` field is removed (`default:` is then an
unknown key; reporting it is E2.3's). No `validate` behaviour yet: like S0.4,
this task is design only.

**Website PR:** the schema page loses `default`; the query and condition
pages describe the grammar as decided, marked as arriving in E2.5–E2.7.

**Example-repo PR:**
- Remove every `default:`; add `required: true` where one was.
- Rewrite every rule file in the new form, without `id:`.
- Remove `requirement-approval-signed`, `content-frozen-requirement` and
  `cross-references-resolve`.
- Bump the pin.

**Check it yourself:** read the DDF diff, then
`prov export website && diff -r _site ../provenance/testdata/golden` shows
only the commit and content-hash stamps.

### E2.2 `validate` command, report model and load errors

**Deliverables:**
- `validate` is implemented. Its stub annotation is removed and `make docs`
  is run.
- An `internal/validate` package: `Violation{Rule, Severity, Message,
  Entity, Path, Line}`, sorting, the text report (D9) and the exit code.
  Loading collects every problem instead of stopping at the first (D2):
  - every entity file that fails to parse;
  - every duplicate ID, with both files;
  - every unknown type;
  - every schema file problem.

  A schema problem that leaves the graph unusable skips the rules, with a
  report line saying so. `export` keeps its current behaviour and messages.
- `--path` and `--out` (D11) work.
- The clean example's report is checked in as `testdata/validate/clean.txt`.
  At this stage it holds only the summary line.

**Check it yourself:** `prov validate` → `0 errors, 0 warnings`, exit 0.
Break two entities' frontmatter and give a third a duplicate ID →
`prov validate` lists all three with file and line, exit 1.
`prov validate --out v/report.txt` writes the same report there.
`prov validate --format xml` → exit 2.

### E2.3 Schema conformance (built-in)

**Deliverables:** the built-in checks on schema and entity files (D3), each
with its own rule ID:
- `schema/unknown-key`: a schema key the DSL doesn't have, `default:`
  included (D1).
- `schema/unknown-field`: a frontmatter key the type doesn't declare.
- `schema/invalid-value`: the model's existing *Invalid* values, such as
  "not a date".
- `schema/enum-value`: a value its enum doesn't list, with the allowed
  values.
- `schema/required`: `required: true` not set.
- `schema/id-format`: an ID that isn't `<id_prefix>-NNNN` for its type
  (REQ-0013).
- `schema/calculated-set`: a calculated field written in the file.
- `schema/list-row`: a list row with an unknown sub-field.
- Warnings: `schema/unused-enum`, `schema/unused-record`,
  `schema/unused-type` (no entities and no link field targeting it).

**Example-repo PR:** none needed; the example is conformant, which the
clean report proves.

**Check it yourself:** in the example:
- add `colour: red` to REQ-0001 → `REQ/REQ-0001.md:<line>: error
  [schema/unknown-field] …`;
- set its `status: aproved` → `[schema/enum-value]` listing the allowed
  values;
- delete its `title:` → `[schema/required]`;
- add `default: draft` back to a schema field → `[schema/unknown-key]`;
- add `schema/enums/Unused.yaml` → `warning [schema/unused-enum]`, exit 0.

Each other step exits 1.

### E2.4 Content and links (built-in)

**Deliverables:** `validate` reports, per file and line, everything that
makes `export website` exit 2, so passing the gate means the site can be
built, plus link integrity and template clutter (D3):
- `content/query`: a query block that can't be run.
- `content/caption`: a misplaced or malformed caption, or a duplicate
  caption ID in a document.
- `content/image`: a missing image, a URL, or a path outside the repository.
- `content/embed-cycle`: an entity that embeds itself.
- `content/template`: a template that doesn't parse, a field name collision,
  or a query naming a template that doesn't exist.
- `content/unrendered-block`: a block in a language this version renders
  only as source (`mermaid`, `drawio`, while E1.13 is deferred; DES-0036),
  so a diagram is never published as its source text.
- `links/unresolved`: a link field value, `[[ID]]`, `[[ID|…]]`,
  `[[ID#field]]` or `![[ID]]` naming no entity, including inside query
  results.
- `links/target-type`: a link whose target's type isn't in the field's
  `target:` list.
- `links/unknown-field`: `[[ID#field]]` naming a field the target's type
  lacks.
- Warnings: `content/template-name` (a type template naming no declared
  type), `content/unused-template` (a named template no query block
  chooses), `content/unknown-override` (a `_`-prefixed template that is no
  site override).

The checks reuse the export's own code paths (the dependency walk and
template parsing), not a second implementation. Every problem is reported,
not just the first; export stays fail-fast.

**Check it yourself:** in DOC-0001:
- change a query operator to `"="`,
- point an image at `../assets/missing.png`, and
- change a `[[REQ-0002]]` to `[[REQ-0999]]`, and
- add a `mermaid` block.

`prov validate` lists all four with their lines, and `prov export website`
still stops at the first. Set REQ-0001's `implements: [REQ-0002]` →
`[links/target-type]` (a Requirement isn't a UserNeed).

### E2.5 Grammar: sub-queries, `value_of`, `missing`, `not_in`

**Deliverables:** the core of D5 in `internal/query`, for query blocks and
`--scope` files alike:
- `where:` on a list field (each row) and a link field (each linked entity,
  forward or incoming facet), nested to any depth; `^name` for an
  enclosing entity's field.
- `value_of` in place of `value: {field: …}`.
- `missing` and `not_in`.
- `from: any`, every entity type (D5).
- `{list, subfield}`, `{via, field}` and the implicit same-row rule
  (`condition.go`'s "tests on the same list within one all-of group read
  the same row") removed. Nested `any_of` allowed everywhere.
- Every match carries what matched it: the entity, and the row or link
  value with its line, ready for D8.

**Example-repo PR:**
- The existing query blocks that use the old forms are rewritten.
- A new review document, DOC-0003 *Design review checklist*, with one query
  block per check the example's rules make, each expected to show no
  results today. This task adds the ones the grammar can now express:
  calibration not current (per row), evidence of a stale protocol version
  (across `tests`), a Risk that needs a risk-benefit analysis, a change
  type outside the approved set, a requirement implementing a deprecated
  need, and a superseded requirement not deprecated.
- Bump the pin and the golden site.

**Check it yourself:**
- Move one equipment row's `calibration_due_date` before `execution_date`
  in EVD-0001 → `prov export website` shows that evidence in DOC-0003's
  calibration table.
- Set EVD-0002's `protocol_version` below VER-0002's `current_version` → it
  appears in the stale-evidence table.

### E2.6 Grammar: `aggregation` and sub-queries on `from`

**Deliverables:**
- `aggregation: {type: count|sum|min|max|avg, field: …}` on a list or link
  sub-query, sharing the calculated-field aggregates and their blank
  semantics (DES-0012).
- `{from: <types>, where: …}`: some entity of those types (or `any`)
  matches, with `^` reading the entity being tested; aggregation counts
  them. *Revised 2026-10-15 (Matt): replaces `duplicated`.* Duplicate
  titles are another requirement with `title` equal to `^title` and a
  different `id`.

**Example-repo PR:** DOC-0003 gains unverified requirements (`count` of
`verified_by` below 1), protocols verifying no requirement, and duplicate
requirement titles. Bump the pin.

**Check it yourself:**
- Remove VER-0002's `verifies:` → the protocol and the requirement it
  verified both appear in DOC-0003.
- Give REQ-0003 REQ-0002's title → both appear in the duplicates table.

### E2.7 Grammar: transitive links and `code_blocks`

**Deliverables:**
- `transitive: true` on a link sub-query: the link followed to any depth,
  terminating on cycles (REQ-0040).
- `code_blocks`: a derived list on every entity, `language` (`none` for a
  bare fence) and the block's line, from the same Markdown walk export uses.

**Example-repo PR:** DOC-0003 gains decomposition cycles and blocks in a
language the project doesn't use. Bump the pin.

**Check it yourself:**
- Set REQ-0002's `parent_requirement: REQ-0003` (REQ-0003 already refines
  REQ-0002) → both appear in the cycles table.
- Add a `python` block to DES-0001 → it appears in the block-language table.

### E2.8 Rule files

**Deliverables:**
- Discovery and checking of `rules/*.yaml` (D4, D6): the file name is the
  ID, `severity` is `error` or `warning` (REQ-0037), `message`, `from` and
  `where` parsed by the same code as a query block, against the schema.
  Problems are `rules/…` violations at the rule file's line; a broken rule
  file skips only itself.
- Evaluation: every match is a violation, placed per D8.
- The clean report is unchanged: the example's rules find nothing.

**Check it yourself:** every break from E2.5–E2.7, now through the gate:
- Remove RSK-0001's `risk_benefit_analysis` → `error
  [risk-benefit-required]` at its `id:` line.
- Move one equipment row's `calibration_due_date` before `execution_date`
  → `[equipment-calibration-current]` at that row.
- Remove VER-0002's `verifies:` → `[protocol-verifies-requirement]` on
  VER-0002 and `warning [requirement-verified]` on the requirement. Exit 1,
  because one of the two is an error.
- Set REQ-0002's `parent_requirement: REQ-0003` → a cycle on both.
- Set a rule's `severity: fatal` → `rules/requirement-verified.yaml:<line>:
  error [rules/severity] …`.
- Add `id: requirement-verified` to that file → `[rules/unknown-key]`.

### E2.9 Report formats

**Deliverables:** `--format json|junit|sarif` per D9.
- Golden files for each format, from the clean example and from one broken
  example.
- The SARIF output is checked against the 2.1.0 JSON schema in a unit
  test. The schema is checked in, so the test needs no network.
- The JUnit output is checked against the XSD that GitHub's and GitLab's
  test reporters accept.

**Check it yourself:** break two entities, then
`prov validate --format sarif --out v.sarif` and upload it with
`gh api … code-scanning/sarifs`, or open it in the SARIF viewer. Both
violations appear at their lines.

### E2.10 Report templates

**Deliverables:** `validate --output <name>` per D10 and D11: the template data,
`html/template` or `text/template` by name, a report template that doesn't
parse or names a missing field reported as `content/template`, and
`templates/reports/` excluded from the unused-template warning. `--output`
replaces E2.9's `--format` and `--out`, so one run writes every report.

**Example-repo PR:** two report templates: `pr-summary.md` (Markdown, for a
pull-request comment) and `validation.html` (a page listing every rule,
passed or not, with its violations). Bump the pin.

**Check it yourself:**
`prov export website && prov validate --output
validation.html=_site/validation.html` → open it beside the site: every rule
listed as passed. Break VER-0002's `verifies:` and rerun → the two
violations, linked to their entities; exit 1. `prov validate --output text
--output sarif=v.sarif --output pr-summary.md=summary.md` writes all three
from one run.

### E2.11 Epic close-out: gate in CI, docs, release

**Deliverables:**
- **Website:** the `validate` reference page gets the built-in checks, the
  rule file format with the example's rules as worked examples (one per
  former rule type), the report formats and report templates, and how to
  require the check on GitHub and GitLab. That includes branch protection
  on `schema/` and `rules/` for stricter review (REQ-0034).
- **Release:** `v0.2.0-alpha`, published with E1.14's workflow.
- **Example-repo PR:** the *Validate* workflow (added in E2.10, built
  from source) downloads the pinned release binary instead and checks it
  against `SHA256SUMS`. It runs `validate --output text --output sarif=…
  --output pr-summary.md="$GITHUB_STEP_SUMMARY"`, one run for the log,
  code scanning and the run's summary. This is the thin wrapper
  (REQ-0060), with no host API beyond uploading the report. It becomes a
  required check.
- **DDF PR:** the same gate on `provenance-ddf`, whose own rules then run on
  every change. Any violations this finds are fixed or listed in the PR.
  (*Decided 2026-10-25:* they are listed, and the gate is not required.
  All 175 come from the DDF's own assurance rules, since only 11 of its 135
  requirements have an assurance activity and none has a record. Writing
  that assurance is later work; until then the check shows red.)
- **Website:** the built-in checks are generated into the `validate`
  reference page by `tools/gendocs`, from the same list the reports use.

**Order:** close-out PRs into the provenance and website epics; their
epics merge into `main`; tag `v0.2.0-alpha`, whose release is the first one
published (the `v0.1.0-alpha` tag predates the release workflow); then the
example and DDF PRs pin that release and their epics merge; then the
example's Validate check is made required on `main`.

**Check it yourself:** open a pull request on `provenance-example` that
deletes VER-0002's `verifies:` → the check fails and shows the violation
on the changed file. Revert → it passes.

## Epic 3 — Architecture groundwork

> **Status: decisions G1–G6 agreed with Matt 2026-10-25** (plan PR #49).

**Goal:** loading, validation and history stop assuming that an entity is
a Markdown file, so the CSV-backed types epic, and every command after it
(`fmt`, `report`, `diff`, `rename`, the editor), add a source of entities
instead of patching each layer. The query layer already copes: the typed
model, the Datalog facts and the template data treat a row like anything
else. What doesn't, checked against the code at the end of Epic 2:

- **Two loading pipelines.** `export` loads in `cli/export.go` (discover,
  schema, model, graph, stopping at the first problem); `validate.Run`
  loads again its own way (`entity.Scan`, `schema.Read`, its own
  unknown-type pass, then `model.Build` and the graph, collecting every
  problem). Discovery runs before the schema in both, so a type the
  schema backs with a file can't be found. `history` walks the files a
  third time (`contentFiles`) for the dirty check, and re-parses every
  commit's `.md` blobs to learn which types it touched (`changes`).
- **Values and positions come from YAML nodes.** `model.Build` decodes
  every value from the frontmatter's `yaml.Node`s by their YAML tags
  (`model.value`), and line numbers come from the same nodes:
  `entity.IDLine`, `model.TextFileLine`, validate's conformance checks
  (`validate/schema.go`), its unknown-type check. A row has no nodes.
- **A path stands for an entity.** `validate` finds the entity of an
  export problem by its path (`byPath` in `validate.go`); page inputs
  and `history.Inputs` are paths and types; a commit records paths. Every
  row of a CSV shares one path.

Rule files, query blocks and the schema are YAML whatever the entity
source, so their YAML positions stay as they are.

**Standing acceptance for every E3 task:** output doesn't change. The
golden site, `testdata/validate/*` and every acceptance step pass as they
are, with no example-repo change and no pin bump. Each task adds unit
tests, including through the test source (G6), and redraws the DDF's
architecture diagrams (DES-0050 packages, DES-0051 loading pipeline,
from `tools/diagrams.py`) where it changes what they show.

**Decisions (for Matt):**

| # | Decision | Proposal | Why |
| --- | --- | --- | --- |
| G1 | How problems are reported | **The loader collects every problem, as `validate` does today; `export` reports the first,** in today's order and wording. | One pipeline. Export's messages are what users and the acceptance script see, so they must not move. |
| G2 | Where it lives | **A package `internal/load`.** `load.Repo(root)` returns the repository, schema, typed entities, graph and the problems, each named by the built-in check it is reported under, with its path, line and entity (*revised 2026-10-25: not a closed list of kinds that `validate` maps, so a new problem needs no change elsewhere*). `export.Input` is built from the result. | Callers stop sequencing packages themselves, and the CLI shrinks to flags and output. |
| G3 | What a source is | **An interface the loader runs after the schema:** it lists the files it reads (the content files for the dirty check), loads them into entities with problems, and parses one file's bytes (for a commit's blobs). The first is the Markdown walker, which ignores the schema and still runs when the schema is broken, so load errors keep being reported then. | A CSV source finds its files from the schema, so the schema has to come first. One place answers "is this file content?" and "what entities does it hold?". |
| G4 | Who decodes values and positions | **The source.** Each entity carries a record, an interface with its value for a field and the position of the entity, a field, a list row or a cell. The Markdown record keeps the YAML nodes (which `fmt` needs) and the YAML-tag typing that `model.value` does today; a CSV record would type a cell by its field (C5). `model.Build` and validate's conformance checks use only the interface, and the shared typing rules (date form, enum values, link IDs) stay in `model`. | The alternative, every source producing YAML nodes, would make a CSV cell pretend to be YAML: its tags would be guesses, and conformance would report YAML problems for a file that isn't YAML. |
| G5 | What history records | **Each commit records the entity IDs it changed beside its paths,** found by parsing both sides of each changed file through its source and comparing entities by ID. `history.Inputs` gains `Entities`; a page's inputs name the entities it shows, and paths only for files that aren't entities (schema, templates, assets, `.component`). An export problem and a violation carry their entity directly. A file moved without changing still counts as changed, as today. | For a Markdown file an entity and its path change together, so output doesn't change; a CSV dump re-exported every month then changes only the rows that differ (C7). Whether a move is a change is the CSV epic's to revisit with DES-0023. |
| G6 | How the seam is proved | **A test-only second source** that builds entities from a Go table, with positions of its own, run through the loader, `validate`, `export` and history in unit tests. No CSV code in this epic. | Shows the layers no longer assume Markdown before the CSV epic depends on it, without designing CSV ahead of its decisions. |
| G7 | Who reads history (*proposed 2026-10-25, for Matt*) | **`load`, after the sources, when first asked:** `Result.History()` reads the entity-level log (G5), the dirty state and the content hash once, and everything that stamps output uses it, so `cli` stops opening git itself (`gitContext` for export, `stamps` for validate's reports). A shallow clone is an error, exit 2, when something needs commits the clone cut off; today export on a shallow clone stops with `export: object not found`, which doesn't say why. The graph gets no history facts yet: what a rule can ask of history is the History-in-rules epic's (below). | REQ-0051 and REQ-0115 already ask for rules over history, and REQ-0039 lets a rule depend only on the entity graph, so history has to reach the graph, and that has to go through the loader, which alone knows the sources that parse a commit's files. Reading it only when asked keeps `validate` at the gate, which checks out one commit, working until a rule needs history. |
| G8 | Problems and checks after loading (*agreed 2026-10-25*, from the DES-0050 diagram) | **One problem type, and the exporter's check through its interface.** `website.Problem` becomes `load.Problem` with its entity, so `validate.go`'s `byPath` goes. `Check` joins the `export.Exporter` interface and `validate` runs it on every registered exporter, not on `website.New()`. An exporter always collects its problems and `export` stops at the first, as with loading (G1), so the `check == nil` branches in `website.run` go. The graph is built only by `load` (`Input.Graph` is required). What templates see of an entity (`EntityData`, `UnresolvedEntity`, `ReportFuncs`) moves out of `export/website` into a package both the site and validate's reports import. The Markdown parse that finds a text value's embeds, query blocks, images and citations comes through the record (G4), so `query` stops importing `markdown` and `assets`. | `validate` imports the website exporter four ways, so a second exporter (PDF) would not be checked at the gate, and report templates depend on the site's package. The dependency walk assuming every text value is Markdown would be wrong for a CSV cell, which renders without wikilinks or embeds (C3). |

Not in this epic: the page pipeline (the first task of Section and item
references) and one Datalog program for derived values (the first task
of Query fields), written in those epics; stamps in the golden site that
change with every pin bump; replacing `validate`'s format `switch` with a
table.

**Order:** the loader first (E3.1), since the other tasks change what it
calls; then sources (E3.2), then values and positions (E3.3), then
history (E3.4), which needs both, then problems and checks (E3.5), which
needs E3.4's entity on every problem.

### E3.1 One loader

**Deliverables:**
- `internal/load` per G1–G2: repository, schema, entities, model, graph,
  collecting every problem as `validate` does now.
- `export` (`cli/export.go`'s `load`) and `validate.Run` use it. `validate`
  reports each problem under its check; the website check (`Check`)
  receives the loaded input as it does now.
- `load` has unit tests for each problem and for its order.

**Check it yourself:** `make ci` passes with no golden or acceptance
change. `git grep -n 'entity.Discover\|entity.Scan\|schema.Load\|schema.Read' internal/cli internal/validate`
→ nothing.

### E3.2 Sources and content files

**Deliverables:**
- The `Source` interface per G3, and the Markdown source: today's walker,
  moved behind it.
- The Markdown source returns `load.Problem`s, each with its check,
  message and the error export reports, so `entity.Scanned`'s `Problems`
  (an `[]error` of two concrete types) and `Partial` go, with the type
  switch in `load` that tells them apart.
- The schema loads before sources; the Markdown source runs with a broken
  schema too.
- The dirty check lists content files through the loader (the sources'
  files, `.component` and the configuration folders), replacing
  `history.contentFiles`. A commit's types come from parsing its blobs
  through the source of each path, replacing the `.md` test in
  `changes`.
- The test source (G6), with tests that its entities load, are selected
  by queries and rules, and are listed as content.

**Check it yourself:** `make ci` passes unchanged. `go test ./internal/load
-run Source -v` → the test source's entities load beside Markdown ones.
In the example checkout, edit a schema file → `export` marks pages
`-dirty`, as before.

### E3.3 Values and positions from the source

**Deliverables:**
- The record interface per G4. `entity.Entity` keeps ID, type and path,
  and its record; `Front`, `Body` and `BodyLine` move into the Markdown
  record.
- `model.Build` decodes values through the record; `model.TextFileLine`,
  `entity.IDLine` and validate's conformance and unknown-type checks use
  its positions.
- `git grep yamlnode` outside the Markdown source, the schema, rule files
  and query blocks → nothing.
- The record parses a text value for what it refers to (G8), and the
  dependency walk uses that: `query` no longer imports `markdown` or
  `assets`. The test source's text values refer to nothing.
- Unit tests: every conformance problem in `testdata/violations` is placed
  as before, and the test source's problems are placed at its own
  positions.

**Check it yourself:** `make ci` passes unchanged: every violation keeps
its line. `go test ./internal/validate -run TestSource -v` → a test-source
entity with a wrong value is reported at its record's position.

### E3.4 Entity-level history

**Deliverables:**
- History per G5: each commit's changed entity IDs, `Inputs.Entities`, and
  page inputs naming entities.
- If G7 is agreed: `Result.History()`; export's stamps and validate's
  report stamps come from it, and `gitContext` and `stamps` leave `cli`.
  A shallow clone fails with exit 2 when history it cut off is needed,
  naming `fetch-depth: 0`.
- DES-0023 says a page's inputs are entities and files, if its wording
  needs it (a clarification: last-changed commits don't change).
- Unit tests on an in-memory git history: a file holding two test-source
  entities, where a commit changing one changes only that entity's
  last-changed commit.

**Check it yourself:** `make ci` passes unchanged: every page keeps its
last-changed commit and revision history. `go test ./internal/history -run
Entities -v` → only the edited entity's last-changed commit moves.
`git clone --depth 1 file://$PWD/.cache/example /tmp/shallow && cd /tmp/shallow && provenance export website`
→ exit 2, naming `fetch-depth: 0`.

### E3.5 Problems and checks

**Deliverables:**
- Per G8: one problem type, with its entity, and `byPath` gone; `Check`
  on the `export.Exporter` interface, run by `validate` on every
  registered exporter; the website exporter always collecting, and its
  graph fallback gone; template data and functions in their own package.
- Unit tests: an exporter registered in a test is checked by `validate`;
  a test-source entity's export problem names the entity.

**Check it yourself:** `make ci` passes unchanged.
`go list -deps ./internal/validate | grep export/website` → nothing.

### E3.6 Epic close-out

**Deliverables:** `CLAUDE.md`'s *Where things are* lists `internal/load`
and the sources; the CSV-backed types epic is revised against what
landed (its tasks 3 and 6 build on E3.2–E3.5); DES-0050 and DES-0051
match the code. No release of its own: it
ships with the next epic's.

**Check it yourself:** read the CSV epic's rough shape; each task names
the groundwork it builds on.

## Later epics (outline)

Detailed task breakdowns are written when the preceding epic closes. Proposed
order after Epic 2, with the reason it comes where it does:

1. **Architecture groundwork** — Epic 3, detailed above.
2. **`fmt`** — canonical serialization; `fmt --check` joins the gate. Needed
   before the editor can write files.
3. **`init`** — medical-device starter template embedded in the binary; a fresh
   `init` must pass `validate` and `export`.
4. **CSV-backed entity types** — rows of a CSV file loaded as entities of a
   type the schema declares (see below). After `init`, so the starter
   template can carry an example, and before `report` and `diff`, so both
   cover imported rows from the start.
5. **Section and item references** — a heading or list item is tagged,
   and a reference to the tag shows the number it has where it is
   rendered (see below). Its first task replaces per-page placeholder
   resolution with a page pipeline. Before `diff` and `rename`, which must
   both understand tagged references.
6. **Query fields** — a field whose value is a query; the template gets
   its results (see below). Its first task evaluates calculated fields and
   query fields as one Datalog program. Before `report`, which can then
   count over a plan's scope, and before `rename`, which must rewrite IDs
   inside them.
7. **`report`** — coverage metrics over the shared query engine.
8. **`diff`** — rendered/semantic diff between refs.
9. **History in rules** — rules ask what changed when, such as evidence
   older than what it verifies (see below). Before `sign` and
   `release tag`, whose rules (REQ-0115, REQ-0051) are built on it.
10. **`rename`** — ID rewrite across links, wikilinks, tagged references,
   query filters and query fields.
11. **`verify artifact`** — against the E1.14 manifest; includes deciding how
   the manifest itself is signed.
12. **`sign` / `sign verify`** — ledger format, then certificate provider, then
   OIDC device flow; signatures become queryable facts, and the Signature
   Presence rules removed in E2.1 return as queries.
13. **`component add/update/remove`** — submodules and composed-graph
   validation.
14. **`release tag`** — gate plus bill-of-materials; released content becomes
   queryable, and Content Frozen After Release returns as a query.
15. **`serve` + HTTP API + editor** — starting with the TipTap round-trip
   fidelity spike deferred from High-Level Design §4.7 (now DES-0037). Last because it's a
   thin client over everything above.
16. **`plugin`**.

### CSV-backed entity types (proposed 2026-10-11, for Matt's review)

**Goal:** a project can load entities from a CSV file. The schema declares
a type backed by a file and maps the file's columns to the type's fields;
each row is then an entity like any Markdown one: it has an ID, can be
linked to and cited, is selected by queries and rules, has a page in the
export, and counts in the content hash. The use case: a periodic dump of
complaint records from the complaint-handling system, which Risk entities
link to so that each failure mode's occurrence rating is justified by the
complaints behind it, counted by a calculated field or a rule.

```yaml
# schema/Complaint.yaml
type: Complaint
id_prefix: CMP
source:
  csv: data/complaints.csv       # repository path
  system: Complaint handling (QMS-CH)   # where every dump comes from (C9)
  id: complaint_no               # the column holding each row's ID
fields:
  - name: received
    type: date
    column: Date Received        # the CSV header, when it differs
  - name: product
    type: string
  - name: failure_mode
    type: string
  - name: harm_occurred
    type: boolean
  - name: summary
    type: text
```

Each dump comes with a sidecar saying when it was exported (C9):

```yaml
# data/complaints.csv.yaml — rewritten with each dump
exported: 2026-09-30
sha256: 3b5c7e…                  # sha256sum data/complaints.csv
```

A Risk then links to its complaints (`complaints: [CMP-2026-0141, …]`, a
link field targeting Complaint, reverse `cited_by_risks`), and
`COUNT(complaints)` or a rule over the facet does the rest.

**Why it needs its own epic:** a row is an entity without a file of its
own, so every part of the product that assumes one file per entity
(discovery, line numbers, last-changed commit, page inputs, the editor,
`fmt`, `rename`, signing) has to say what it does with a row.

**Decisions (for Matt, before the epic is detailed):**

| # | Decision | Proposal | Why |
| --- | --- | --- | --- |
| C1 | Built in, or an import plugin (REQ-0070 allows import plugins) | **Built in.** The schema declares the source; there is no conversion step and no generated Markdown. Plugins remain for formats that need code (an eQMS API, XLSX). | CSV is the common denominator every system exports, and a built-in reader keeps the data's validation identity the binary's. Generating Markdown from the dump would create thousands of files no one reviews and that drift from the source. |
| C2 | Row IDs | **The `id` column's value, used as written,** and checked to be unique within the file and across the repository. The `<id_prefix>-NNNN` form (DES-0007) applies to authored entities only; a CSV type's `id_prefix` is still required, and its IDs must start with it, so a link's type is still readable. | Complaint numbers are the identity the source system and the complaint handlers use; renumbering them would break traceability back to that system. |
| C3 | Read-only | **Rows are read-only in Provenance.** `fmt`, `rename`, the editor and `sign` never write them; the file is replaced by the next dump. A row can't have a Markdown body; `text` columns render as Markdown **without wikilinks, embeds or rendered fences** (`[[…]]`, `![[…]]` and a ```` ```query ```` block are shown as written), so a cell can't link out (C4), pull in content or run a query. | The source system is the record; Provenance holds a dated copy. |
| C4 | Which way links go | **CSV rows can't link out** in the first version; authored entities link to them (Risk → Complaint), and the facet gives the reverse. A column holding IDs as a link is a later addition. | Links are stored on the later artefact (DES-0011): the risk analysis is written after, and about, the complaints. |
| C5 | File format | **RFC 4180, UTF-8 (a BOM is ignored), a header row, comma-separated** by default with `delimiter:` to override, CRLF or LF. Values are typed by the field: dates `YYYY-MM-DD` only, unless the field gives `format:`; booleans `true/false`, `yes/no`, `1/0`; an empty cell is an absent value. Columns the schema doesn't name are ignored, and reported as a warning so a renamed column is noticed. | What Excel and most systems export, with types as strict as for authored entities. |
| C6 | Where a row's problems are placed | **The file and the row's line** (`data/complaints.csv:142`), for validate, export errors and SARIF. | The same contract as every other violation (D8/DES-0048). |
| C7 | History | A row's last-changed commit is the **last commit that changed that row's line content**, not the file's; the file's commit and dump date are shown on every row's page. Page inputs list the file. | Re-importing the whole file every month mustn't make every complaint look changed. |
| C8 | Pages | **A page per row by default,** with `pages: false` on the source for large dumps; rows are still listed by queries and cited by `[[ID]]` without a page, as an entity outside the scope is. | Thousands of near-identical pages make a site slow to build and browse. |
| C9 | Provenance of the dump | Provenance can't tell where a copied file came from, so it records what it knows and asks for the rest. **Git** knows when the copy entered the design record: the commit that last changed the file, with its date and author. **The schema** declares `system:`, the system the dumps come from, which doesn't change between dumps. **A sidecar beside each dump** (`data/complaints.csv.yaml`) holds what only the person exporting knows: `exported:`, the dump's date, and `sha256:`, the hash of the CSV it describes (`sha256sum` prints it). `validate` fails when the sidecar is missing or its hash isn't the file's, so a new dump copied in without saying when it was exported is stopped at the merge gate. All of it is shown on each row's page and available to templates; the CSV and the sidecar are in the content hash like any other file. | ISO 13485 §8.2.2, which the QMSR incorporates: complaint records are kept by the complaint-handling process, and the copy must be traceable to it. A per-dump date in the schema would be edited on every dump and could silently go stale; the hash ties the sidecar's date to this file, not to whichever file was there when it was last typed. |

**Rough shape:**

1. **Design:** a user need, requirements (CSV-backed types, row IDs,
   read-only, provenance of the dump) and a design element in the DDF,
   with C1–C9 as decided.
2. **Example repo:** a `Complaint` type with `data/complaints.csv` (a dozen
   synthetic rows) and its sidecar, RSK-0001's failure modes linking to them, and
   `occurrence` justified by a `COUNT` over them.
3. **Loading:** the CSV reader as a second source of the loader (E3.2),
   `source:` in the schema, rows as entities in the model and the graph,
   each with its row's positions (E3.3); load errors at file and row.
4. **validate:** schema conformance for CSV types and rows (C2, C5, C6),
   unknown columns as warnings, and the sidecar: present, and its hash the
   file's (C9).
5. **export:** row pages, `pages: false`, citations and queries over rows,
   the dump's provenance on each page: system, export date, and the
   commit that brought it in.
6. **History:** per-row last-changed (C7) on E3.4's entity-level log,
   which diffs the CSV by row ID; page inputs, the dirty check, content
   hash.
7. **Close-out:** docs (a "Importing data" concept page), release.

### Section and item references (proposed 2026-10-18, for Matt's review)

**Goal:** a document can point at a section or a list item of itself or
of another entity, and the reference shows the number that section or
item has where it is rendered: "see Section 3.2", "step 4 of
[[VER-0001]]". Today only captions can be referred to (`[[#id]]`,
DES-0034), and headings and list items have no numbers Provenance
knows, since list numbers are the browser's and the step tables'
numbers are a CSS counter.

```markdown
## Device Under Test {#dut}

1. Connect the current probe.
2. Set the amplitude to 3 mA and record one pulse. {#one-pulse}

The device is listed in [[#dut]]; the pulse is recorded at
[[#one-pulse]], and its limits are in [[REQ-0001#limits]].
```

renders, on a page where the heading is the third top-level section, as
"The device is listed in Section 3; the pulse is recorded at item 2,
and its limits are in Section 2.1 of REQ-0001".

**Why "where it is rendered":** an entity's sections are numbered
differently on its own page and inside a document that embeds it (its
headings shift, DES-0035, and are numbered within the document). A
reference resolves on the page being rendered, after the page is
complete, as a caption reference does (DES-0034's two passes): to the
tag's number on this page when the tagged entity is embedded here,
otherwise to its number on the tagged entity's own page, with the
entity's ID ("Section 2.1 of REQ-0001").

**Decisions (for Matt, before the epic is detailed):**

| # | Decision | Proposal | Why |
| --- | --- | --- | --- |
| X1 | How a tag is written | **`{#tag}` at the end of a heading's line or a list item's first paragraph,** the attribute syntax goldmark and Pandoc already use for headings. Tags are lower-case, `[a-z0-9-]`, and unique within the entity, sharing one namespace with caption IDs. | Plain text that reads well unrendered and survives other Markdown tools; one namespace means `[[#x]]` never has to say which kind it means. |
| X2 | How a reference is written | **`[[#tag]]` within the same entity, `[[ID#tag]]` to another,** with `\|text` to replace the shown text and keep the link. Two existing meanings collide with this, and both are settled the same way: (a) `[[ID#field]]` already shows a field's value (DES-0034), so **a tag may not be the name of a field or facet of its entity's type** (a validate error, X7), and `[[ID#name]]` is a field when the type has one by that name, otherwise a tag; (b) `[[#id]]` today means a caption anywhere on the page being rendered, embedded entities included, so **`[[#x]]` becomes the writing entity's own tag or caption, wherever it is rendered**, and another entity's caption is `[[ID#x]]`, shown as its number on this page when that entity is embedded here. Every `[[#…]]` in `provenance-example` already refers to its own entity's caption, so none changes. | Extends the wikilink and caption syntax already in use (DES-0034) rather than adding a third. A reference that means the same thing on the entity's own page and inside any document embedding it is what lets one entity be embedded in several documents. |
| X3 | Which headings are numbered | **Every heading of a page is numbered by the renderer** (`1`, `1.2`, `1.2.3`), after embeds are shifted, and the number is in the HTML in `<span class="section-number">`. The stylesheet decides whether it is shown; a reference shows it regardless. A page's title heading (the H1) is not numbered. | The number a reference shows must be the one the reader can find. Numbering in Go, not CSS, is what lets a reference know it. |
| X4 | List items | **An ordered list's items are numbered in Go too** (the number written in the Markdown, as CommonMark keeps it, then counting up); nested items read `2(b)`, then `2(b)(iii)`. A tag on an unordered list's item is an error: it has no number to show. | A protocol's numbered steps are the case that needs this. |
| X5 | What the reference shows | **"Section 3.2" for a heading, "item 4" for a list item, followed by "of ID" when it resolves to another page**; fixed, as caption kinds are (DES-0034), and styled by `section-ref` and `item-ref`. | Same as captions: no configuration, appearance in the stylesheet. |
| X6 | Rows of list fields | **Not in the first version.** A step in a `steps:` list field is numbered by its template, so the renderer can't see it. A later version can give templates an `anchor` function that registers a row's number and tag. | Keeps the first version to Markdown, which the renderer already walks. |
| X7 | validate | **Errors:** a tag used twice in one entity, a tag on something other than a heading or an ordered list item, a malformed tag, a tag named like a field or facet of its entity's type (X2), and `[[#tag]]` or `[[ID#tag]]` whose tag doesn't exist in that entity (DES-0048's existing check for `[[#id]]`, widened). | References break silently when a heading is deleted; the merge gate is where that is caught. |

**Rough shape:**

1. **Design:** update DES-0034 (captions and references become one
   numbering pass) and DES-0035 (heading numbering after the shift); a
   requirement for section and item references; X1–X7 as decided.
2. **Example repo:** DOC-0004 and VER-0001 tag sections and steps (VER-0001's
   procedure written as an ordered list), and DOC-0005 refers to them,
   including one tag that renders differently embedded and on its own
   page.
3. **Page pipeline.** Today each page is finished on its own, by string
   passes over its HTML: embeds are spliced and their headings shifted
   (`headings.go`), then caption placeholders, delimited by private-use
   characters, are resolved by regular expression (`captions.go`). A
   reference to another page's section needs that page's numbering, so
   the exporter becomes two-phase: render every page, including the own
   page of each entity a reference can fall back to, whether exported or
   not; then finish them all. Finishing a page parses its HTML once
   (`golang.org/x/net/html`, already used) and runs ordered passes over
   the tree: splice and shift embeds, number headings, ordered lists and
   captions, then resolve references from the anchor tables of every
   page. Placeholders become elements with `data-` attributes naming the
   owning entity, not private-use characters. The shape is Pandoc's
   (parse, filters, write); the tool is not used, since the renderer
   must be in the binary (REQ-0062).
   Captions move onto it first, with byte-identical output; the golden
   test is the check. Numbering the tree rather than the Markdown also
   numbers an `<ol>` a template writes, which may bring X6 into the
   first version.
4. **Parsing:** `{#tag}` on headings and list items in `internal/markdown`;
   `[[ID#tag]]` in wikilinks, with X2's field-or-tag lookup.
5. **Numbering and resolution:** headings and ordered items join the
   pipeline's numbering pass, and references resolve after it.
6. **validate:** X7.
7. **Close-out:** docs (the Markdown reference page), release.

### Query fields (proposed 2026-10-18, for Matt's review)

**Goal:** a field's value can be a query, written in the entity's
frontmatter, and its template gets the entities it selects. A Design
Verification Plan then says which design items it covers, and its
template gathers the devices under test and the equipment from the
protocols that verify them, without a query block per table. Query
blocks (DES-0032) stay for the body: each result is rendered on its own,
so they can't aggregate. A query field gives the template every result
at once.

```yaml
# schema/VerificationPlan.yaml
type: VerificationPlan
id_prefix: DVP
fields:
  - name: title
    type: string
    required: true
  - name: scope
    type: query
    of: [Requirement]        # the types the query may select
    required: true
```

```yaml
# DVP/DVP-0001.md frontmatter
scope:
  from: Requirement
  where:
    field: status
    operator: equals
    value: approved
  order_by: order
```

```gotemplate
{{/* templates/VerificationPlan.tmpl */}}
<h2>Device Under Test</h2>
{{range collect .Scope "VerifiedBy" "DeviceUnderTest"}}…{{end}}
<h2>Test equipment</h2>
{{range collect .Scope "VerifiedBy" "EquipmentNeeded"}}…{{end}}
```

**Decisions (for Matt, before the epic is detailed):**

| # | Decision | Proposal | Why |
| --- | --- | --- | --- |
| Q1 | What the value is | **A query block's YAML** (`from`, `where`, `order_by`) under the field's name, validated by the same grammar, with `of:` in the schema limiting `from:`. `render` and `template` aren't allowed: how results are shown is the template's job. | One grammar for blocks, rules and fields (the general mechanism, not a special case). |
| Q2 | What the template and conditions see | **The results, as a many-link to the selected entities,** in query order: `.Scope` is a list of entities, as a link field's is. Conditions and rules treat it as a link (`field: scope, where: …`), and calculated fields can `COUNT(scope)`. The query's text is available as `.Scope.Query` for showing what the plan selected and why. | The template and the grammar already know what a many-link is; nothing new to learn. |
| Q3 | Reverse facets, and loops | **A query field may declare `reverse_name` like a link field, and loops are found by the Datalog engine's stratification, not ruled out by hand.** Query fields compile into the same program as calculated fields (task 3), so a query field reading a calculated field, a calculated field counting a query field, and a query field selecting by a facet another query field feeds are all ordered by the engine. A loop through positive conditions only has one answer (the least fixpoint) and is allowed; a loop through `missing`, `not_in` or an aggregation has none, and is a schema error placed at the query fields in the loop. | One general mechanism instead of a restriction that the next feature would have to lift. A Requirement can find the plans that cover it through the facet rather than a query block over VerificationPlan. |
| Q4 | Gathering across results | **One new template function, `collect`:** `collect LIST "Field" "Field" …` follows each named field from every entity in the list, flattens the result, keeps each entity once, and orders by ID. | Templates can't de-duplicate today, which is why DOC-0004 needs entity types such as TestEquipment and a query block per table. A path of fields keeps it general: it works on any link, facet or query field. |
| Q5 | `^` inside the query | **`^field` reads the entity holding the query field,** as in a sub-query, so a plan can select by its own values (`value_of: ^component`). | The same meaning `^` has everywhere else. |
| Q6 | Page inputs and history | A query field's results change when other entities change, so **the page inputs and the "changed since" check include every entity the query read,** through the dependency walk query blocks already use (E1.11). | The page must be marked changed when its scope grows, though the plan's file didn't change. |
| Q7 | Signing | **A signature covers the query and the IDs it returned at that commit,** both stored in the ledger entry. `sign verify` reports a plan whose current results differ from the signed ones. | Approving a plan approves its scope; a scope that grows after approval mustn't look approved. Settled properly in the `sign` epic, but the ledger format must leave room. |

**Rough shape:**

1. **Design:** a field type in DES-0010, the grammar's use of it in
   DES-0032, `collect` in the template functions (DES-0030), Q1–Q7 as
   decided.
2. **Example repo:** a VerificationPlan type and DVP-0001 whose template
   gathers devices and equipment with `collect`; DOC-0004's per-table
   query blocks and the joined-rows CSS go, unless kept to exercise
   query blocks.
3. **One program for derived values.** Today `query.NewGraph` builds the
   facts, evaluates calculated fields as one Datalog program and writes
   their values back as facts, and each query block is evaluated
   separately afterwards. Query fields sit between the two in both
   directions (Q3), so the graph's derived values become one program:
   calculated fields first, with the same results (the golden test is
   the check), then query fields compiled into it as derived `link`
   facts. Needs E2.5's correlated sub-queries for `^` (Q5).
4. **Schema and model:** `type: query` and `of:`; the query parsed and
   checked at load; results resolved as a many-link.
5. **Grammar:** query fields in conditions, rules and calculated fields;
   `^` (Q5); loops reported by stratification (Q3).
6. **export:** `.Scope` in templates, `collect`, page inputs (Q6).
7. **validate:** a malformed query, a `from:` outside `of:`, `render` or
   `template` given, and a loop with no answer (Q3).
8. **Close-out:** docs (the schema reference and the template functions),
   release.

### History in rules (proposed 2026-10-25, for Matt's review)

**Goal:** a rule can ask what changed when. The first case: an assurance
record is evidence for the activity and requirement it verified as they
were, so a later change to either leaves the evidence stale, and the
gate should say so. `sign` (a signature goes stale on a new commit,
REQ-0115) and `release tag` (content frozen after release, REQ-0051)
are the same question about other entities.

**What it builds on:** E3.4 records which entities each commit changed,
and G7 has `load` read history once, when asked. This epic puts history
in the graph as facts and lets conditions use them.

**Decisions (for Matt, before the epic is detailed):**

| # | Decision | Proposal | Why |
| --- | --- | --- | --- |
| H1 | How a rule says it | **A condition operator comparing when two entities last changed,** with `^` naming the entity the rule is about, as in a sub-query: on AssuranceRecord, `{field: activity, where: {changed_since: ^}}` finds records whose activity changed after them. History is Datalog facts (an entity changed at a commit, commits ordered), so it composes with the rest of the grammar, and no entity gets implicit fields. | One general mechanism over the graph, and nothing in the files that the author didn't write. |
| H2 | What "after" means | **Order along the first-parent history of the commit being validated, never commit dates;** a change not yet committed is the newest. | Dates can be set to anything and rebases rewrite them; first-parent order is what export's last-changed commit already uses (DES-0023). |
| H3 | Squash and rebase merges | **Order is worked out from history when validating; no rule compares a commit hash written in a file.** A rule then gives the same answer whichever merge strategy the host uses. Signatures bind to a commit hash (DES-0040), which a squash merge discards: the `sign` epic must settle this, either by requiring merge commits or by binding a signature to the entity's content. | A record on a pull request branch names commits that a squash merge drops, so a hash-based check would report every squashed record stale. |
| H4 | Shallow clones | **`validate` fails with exit 2 when a rule needs history the clone cut off,** naming `fetch-depth: 0`; the example's and the DDF's Validate workflows fetch full history from this epic on. | `actions/checkout` fetches one commit by default, and history read from it would make every entity look new: a gate that silently passes or fails is worse than one that stops. |
| H5 | What counts as a change | **A change to any of the entity's values,** found by comparing the entity on both sides of a commit through its source (E3.4), so reformatting a file doesn't count; REQ-0051's "content fields" may narrow it to some fields per type. | Re-saving or reformatting a protocol must not make every record that verified it stale. |

**Rough shape:**

1. **Design:** DES elements for history facts and the operator; H1–H5 as
   decided; REQ-0051 and REQ-0115 linked to them.
2. **Example repo:** a verification record older than a change to its
   protocol, reported by a rule.
3. **Facts:** history in the graph, read only when a rule uses it (G7).
4. **Grammar:** `changed_since`, with `^`.
5. **validate:** the shallow-clone error (H4); both Validate workflows
   fetch full history.
6. **Close-out:** docs (the rules reference), release.
