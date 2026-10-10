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
- [ ] E2.1 Validate design decisions
- [ ] E2.2 `validate` command, report model and load errors
- [ ] E2.3 Schema conformance (built-in)
- [ ] E2.4 Content that would stop export (built-in)
- [ ] E2.5 Rule files
- [ ] E2.6 Required Field and Field Value In Set
- [ ] E2.7 Reference Count
- [ ] E2.8 Reference Validity
- [ ] E2.9 Field comparisons
- [ ] E2.10 Uniqueness and No Cycles
- [ ] E2.11 Query Assertion and Block Language
- [ ] E2.12 Report formats
- [ ] E2.13 Epic close-out: gate in CI, docs, release

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

1. **One task = one PR**, on branch `task/<id>-<slug>`. Don't start the next
   task until the previous one is merged.
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

> **Status: proposed 2026-10-10, for Matt's review.** Nothing below is
> started until the plan PR is merged. The decisions table needs a yes, a no
> or a change on each row.

**Goal:** `provenance validate` is the merge gate (REQ-0054). It loads the
repository, runs every rule instance in `rules/` and the schema's own
checks, and reports every violation (REQ-0038) with its file and line, in
text, JSON, JUnit or SARIF (REQ-0059). It exits `1` while any error-severity
violation exists and `0` otherwise, warnings included (REQ-0055). The
example repository runs it as a required check on its pull requests, from a
released binary (REQ-0060).

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

### Decisions (for Matt)

| # | Decision | Proposal | Why | Needed by |
| --- | --- | --- | --- | --- |
| D1 | What a field's `default:` means (OPEN since Detailed Design §5; deferred 2026-09-28) | **Creation-time.** A default is the value a new entity is given when it is created (`init`, the editor, a future `new`) and written into its file. When reading, an omitted field is absent, as export already treats it. `required: true` is satisfied only by a value in the file. | The file is the record: what a reviewer sees in the diff is the whole truth. With read-time defaults, changing a default in `schema/` would silently change the value of every entity that omits the field, such as a Risk's `residual_acceptability`, without any entity file changing. No example entity omits a defaulted field, so nothing in the fixture moves. | E2.1 |
| D2 | Load errors (unparsable frontmatter, missing `id`/`type`, a duplicate ID, an unknown type, a broken schema or rule file) | **Violations, not tool errors:** reported with file and line, exit `1`. Exit `2` is kept for the tool and its invocation (bad flags, unreadable repository, I/O). Rules that need the graph are skipped when it can't be built, and the report says so. `export` keeps exiting `2` on the same problems. | The gate's job is to tell an author what is wrong with their change, and a broken file is the most common mistake. SARIF and JUnit can only show it as a violation. | E2.2 |
| D3 | Checks that need no rule file | **Built in, always error severity, with IDs under `schema/…` and `content/…`** (listed in E2.3 and E2.4): conformance to the schema, ID format, and everything that would stop `export`. Rule instances can't turn them off. | Without them a repository could pass the gate and still fail to export. The fixed library (REQ-0035) is for project policy; well-formedness isn't policy. | E2.3 |
| D4 | `required: true` versus the Required Field rule | `required: true` is the unconditional built-in check (D3). The Required Field rule is for conditional requirements, with a `where`. A Required Field instance without `where` is allowed but redundant: it is reported once as a warning on the rule file. | The two overlap today, and REQ-0042 makes the filter the rule's point. | E2.5 |
| D5 | A rule file's name | **`rules/<id>.yaml`, enforced** (DES-0016 already says so). The example's `risk-benefit-required.yaml` (id `risk-benefit-required-if-unacceptable`) is renamed in E2.1's example PR. | One place to find a rule from its report line. | E2.1 |
| D6 | The source filter of Reference Count (DDF rules mark its key an open point in DES-0016) | **`source_where`**, as the DDF's rules already write it, plus **`target_where`** to filter the counted targets. Both use the shared condition grammar (DES-0015). | It is already in use, and it matches the `<side>_type` naming. | E2.7 |
| D7 | Field Comparison Within Entity's parameters | **One condition test that must hold:** `entity_type`, `field`, `operator`, `value` (with `value: {field: …}` for the other field), which reuses the condition parser. On a list sub-field it is checked, and reported, per row (REQ-0045). The example's two calibration rules, written today as Required Field with only a `where`, are rewritten to this form in E2.1's example PR. | A Required Field without a field to require has no clear meaning. This form is the comparison the comments already describe. | E2.1 |
| D8 | Reference Validity's `link` parameter | `link: any` covers every link field and every inline reference (`[[ID]]`, `[[ID\|…]]`, `[[ID#field]]`, `![[ID]]`); `link: <field>` covers only that field; `link: inline` covers only inline references. "Disallowed" (REQ-0050) means a link field whose target has a type outside the field's `target:` list. `[[ID#field]]` naming a field the target's type lacks is reported too. | Matches the rule's comment in the example ("both are just links at the graph level") and gives a way to apply the check to one or the other. | E2.8 |
| D9 | Rule types this version can't check (Signature Presence until `sign`, Content Frozen After Release until `release tag`) | The rule file is fully parameter-checked, then reported as a **warning on the rule file: "not checked by this version"**. It never fails the gate, but it is always visible. | A gate that silently skips a configured rule would be worse than one that says it can't check it. Both the example and the DDF have instances of each. | E2.5 |
| D10 | Where a violation is placed | The line of the offending field, row or block when there is one. For an absence (a missing required field, a Reference Count below `min`), the line of the entity's `id:`. For a rule-file problem, the line in the rule file. | Every format needs a file and line; SARIF and JUnit show them inline in the pull request. | E2.2 |
| D11 | Report formats | **text:** `path:line: error\|warning [rule-id] message (ENTITY-ID)`, then a one-line summary. **json:** one object with `version: 1`, `violations[]` (`rule`, `severity`, `message`, `entity`, `path`, `line`) and `summary`. **junit:** one test case per rule instance and built-in check, each failing with its violations. **sarif:** SARIF 2.1.0, one run, one result per violation, the rule's `message` as its short description. Field names are recorded in the DDF; only the exit code is guaranteed (DES-0018). | Each is the smallest form its consumers (CI summaries, GitHub code scanning, scripts) read. | E2.12 |

Not in this epic: composed-graph validation across components (REQ-0025,
the `component` epic); live feedback in the editor (REQ-0053, `serve`);
`fmt --check` at the gate (the `fmt` epic); stricter reviewers for schema
and rules (REQ-0034, a host setting, documented in E2.13).

### E2.1 Validate design decisions

**Deliverables:** the decisions above, as approved, written into
`provenance-ddf`, each marked DECIDED with a date:

- DES-0010: `default:`.
- DES-0016: rule files, their parameters per rule type, `source_where`,
  `target_where`, the Field Comparison Within Entity form, Reference
  Validity's `link`, and how unavailable rule types are reported.
- DES-0018: the load-error exit code.
- A new design element for the built-in checks and the report formats.

`internal/schema`'s note on `Default` is updated to the decision. No
`validate` behaviour yet: like S0.4, this task is design only.

**Example-repo PR:**
- Rename `rules/risk-benefit-required.yaml` to match its ID.
- Rewrite `equipment-calibration-current.yaml` and
  `validation-equipment-calibration-current.yaml` as Field Comparison Within
  Entity.
- Bump the pin.

**Check it yourself:** read the DDF diff, then
`prov export website && diff -r _site ../provenance/testdata/golden` shows
only the commit and content-hash stamps.

### E2.2 `validate` command, report model and load errors

**Deliverables:**
- `validate` is implemented. Its stub annotation is removed and `make docs`
  is run.
- An `internal/validate` package: `Violation{Rule, Severity, Message,
  Entity, Path, Line}`, sorting, the text report and the exit code. Loading
  collects every problem instead of stopping at the first (D2):
  - every entity file that fails to parse;
  - every duplicate ID, with both files;
  - every unknown type;
  - every schema file problem.

  A schema or rule-file problem that leaves the graph unusable skips the
  rules, with a report line saying so. `export` keeps its current behaviour
  and messages.
- `--path` works.
- The clean example's report is checked in as `testdata/validate/clean.txt`.
  At this stage it holds only the summary line.

**Check it yourself:** `prov validate` → `0 errors, 0 warnings`, exit 0.
Break two entities' frontmatter and give a third a duplicate ID →
`prov validate` lists all three with file and line, exit 1.
`prov validate --format xml` → exit 2.

### E2.3 Schema conformance (built-in)

**Deliverables:** the built-in checks on entity files (D3), each with its
own rule ID:
- `schema/unknown-field`: a frontmatter key the type doesn't declare.
- `schema/invalid-value`: the model's existing *Invalid* values, such as
  "not a date".
- `schema/enum-value`: a value its enum doesn't list, with the allowed
  values.
- `schema/required`: `required: true` not set (D1, D4).
- `schema/id-format`: an ID that isn't `<id_prefix>-NNNN` for its type
  (REQ-0013).
- `schema/calculated-set`: a calculated field written in the file.
- `schema/list-row`: a list row with an unknown sub-field.

**Example-repo PR:** none needed; the example is conformant, which the
clean report proves.

**Check it yourself:** in the example:
- add `colour: red` to REQ-0001 → `REQ/REQ-0001.md:<line>: error
  [schema/unknown-field] …`;
- set its `status: aproved` → `[schema/enum-value]` listing the allowed
  values;
- delete its `title:` → `[schema/required]`.

Each exits 1.

### E2.4 Content that would stop export (built-in)

**Deliverables:** `validate` reports, per file and line, everything that
makes `export website` exit 2, so passing the gate means the site can be
built:
- `content/query`: a query block that can't be run.
- `content/caption`: a misplaced or malformed caption, or a duplicate
  caption ID in a document.
- `content/image`: a missing image, a URL, or a path outside the repository.
- `content/embed-cycle`: an entity that embeds itself.
- `content/template`: a template that doesn't parse, a field name collision,
  or a query naming a template that doesn't exist.
- `content/template-name`, a warning: a type template naming no declared
  type.

The checks reuse the export's own code paths (the dependency walk and
template parsing), not a second implementation. Every problem is reported,
not just the first; export stays fail-fast.

**Check it yourself:** in DOC-0001:
- change a query operator to `"="`, and
- point an image at `../assets/missing.png`.

`prov validate` lists both with their lines, and `prov export website`
still stops at the first.

### E2.5 Rule files

**Deliverables:** discovery and parameter checking of `rules/*.yaml`
(DES-0016):
- The common keys: `id` matching the file name (D5), `rule` from the fixed
  library (REQ-0035), `severity` of `error` or `warning` (REQ-0037), and
  `message`.
- Each rule type's parameters, checked against the schema: types, fields,
  links and facets must exist, and conditions parse.
- The redundant Required Field warning (D4).
- Unavailable rule types reported (D9).

Problems are reported at the rule file's line, as `rules/<problem>`
violations. No rule is evaluated yet, so the clean report gains only the
two "not checked by this version" warnings (Signature Presence and Content
Frozen After Release).

**Check it yourself:**
- Set a rule's `rule: ReferenceCounts` → `rules/requirement-verified.yaml:6:
  error [rules/unknown-type]` listing the library.
- Set `link: verifed_by` → an error naming the type's links and facets.

### E2.6 Required Field and Field Value In Set

**Deliverables:** the two entity-level rule types (REQ-0042, REQ-0043),
each with its `where` filter (DES-0015). Each one is compiled to the shared
Datalog engine, so the filter means exactly what it means in a query
block.

**Check it yourself:**
- Remove RSK-0001's `risk_benefit_analysis` (its
  `residual_acceptability` is `unacceptable`) → `[risk-benefit-required-if-unacceptable]` at
  its `id:` line.
- Set ECO-0001's `change_type: cosmetic` →
  `[eco-change-type-valid]` at that line.

### E2.7 Reference Count

**Deliverables:** REQ-0041 with `source_type`, `link` (a field or an
incoming facet, DES-0011), `target_type`, `min`, `max`, `source_where` and
`target_where` (D6). The message gives the count found against the bounds.

**Example-repo PR:** none needed. `protocol-verifies-requirement` and
`requirement-verified` cover both directions.

**Check it yourself:** remove VER-0002's `verifies:` →
`[protocol-verifies-requirement]` on VER-0002 and
`warning [requirement-verified]` on the requirement it verified. Exit 1,
because one of the two is an error.

### E2.8 Reference Validity

**Deliverables:** REQ-0050 per D8: unresolved and disallowed link targets,
and unresolved inline references, including those inside query results.
They are found by the same Markdown walk export uses, at the line of the
reference.

**Check it yourself:**
- Change a `[[REQ-0002]]` in DOC-0001 to `[[REQ-0999]]` → reported at that
  line.
- Set REQ-0001's `implements: [REQ-0002]` → disallowed (a Requirement
  isn't a UserNeed).

### E2.9 Field comparisons

**Deliverables:**
- Field Comparison Across Link (REQ-0044): `source_type`, `link`,
  `target_type`, `source_field`, `operator`, `target_field`. One violation
  per failing source-target pair.
- Field Comparison Within Entity (REQ-0045) per D7, per row for list
  sub-fields. Each row's violation is placed at its row.

**Check it yourself:**
- Set EVD-0002's `protocol_version` below VER-0002's `current_version` →
  `[evidence-verified-latest-version]`.
- Move one equipment row's `calibration_due_date` before
  `execution_date` → `[equipment-calibration-current]` at that row.

### E2.10 Uniqueness and No Cycles

**Deliverables:**
- Uniqueness (REQ-0047): `entity_type` and `fields` (one or more), compared
  as typed values. Entities missing any of the fields never collide. Each
  group of duplicates is reported once, on every member, naming the others.
- No Cycles (REQ-0046) over one link field, of either cardinality. Each
  cycle is reported once, from its smallest ID, listing the path.

**Check it yourself:**
- Give REQ-0003 REQ-0002's title → `warning [requirement-title-unique]` on
  both, exit 0, because the rule is a warning.
- Set REQ-0002's `parent_requirement: REQ-0003` (REQ-0003 already refines
  REQ-0002) → `REQ-0002 → REQ-0003 → REQ-0002`, exit 1.

### E2.11 Query Assertion and Block Language

**Deliverables:**
- Query Assertion (REQ-0049): `from` and `where` exactly as in a query
  block, one violation per match.
- Block Language (REQ-0052): `entity_type` and `allowed` (`none` for a
  block without a language). A block in a language this version renders
  only as source (`mermaid`, `drawio`, while E1.13 is deferred) is reported
  even when it is allowed (DES-0036).

**Check it yourself:**
- Set USR-0001 to `deprecated` → `warning [no-implements-deprecated-need]`
  on each requirement implementing it.
- Add a `mermaid` block to DES-0001 → `[block-languages]` saying this
  version can't render it.

### E2.12 Report formats

**Deliverables:** `--format json|junit|sarif` per D11.
- Golden files for each format, from the clean example and from one broken
  example.
- The SARIF output is checked against the 2.1.0 JSON schema in a unit
  test. The schema is checked in, so the test needs no network.
- The JUnit output is checked against the XSD that GitHub's and GitLab's
  test reporters accept.

**Check it yourself:** break two entities, then
`prov validate --format sarif > v.sarif` and upload it with
`gh api … code-scanning/sarifs`, or open it in the SARIF viewer. Both
violations appear at their lines.

### E2.13 Epic close-out: gate in CI, docs, release

**Deliverables:**
- **Website:** the `validate` reference page gets the built-in checks, the
  rule file format and every rule type's parameters (with the example's
  instances), the report formats, and how to require the check on GitHub
  and GitLab. That includes branch protection on `schema/` and `rules/`
  for stricter review (REQ-0034).
- **Release:** `v0.2.0-alpha`, published with E1.14's workflow.
- **Example-repo PR:** a `validate` workflow that downloads the pinned
  release binary, checks it against `SHA256SUMS` and runs `validate --format
  sarif`. This is the thin wrapper (REQ-0060), with no host API beyond
  uploading the report. It becomes a required check.
- **DDF PR:** the same gate on `provenance-ddf`, whose own rules then run on
  every change. Any violations this finds are fixed or listed in the PR.

**Check it yourself:** open a pull request on `provenance-example` that
deletes VER-0002's `verifies:` → the check fails and shows the violation
on the changed file. Revert → it passes.

## Later epics (outline)

Detailed task breakdowns are written when the preceding epic closes. Proposed
order after Epic 2, with the reason it comes where it does:

1. **`fmt`** — canonical serialization; `fmt --check` joins the gate. Needed
   before the editor can write files.
2. **`init`** — medical-device starter template embedded in the binary; a fresh
   `init` must pass `validate` and `export`.
3. **`report`** — coverage metrics over the shared query engine.
4. **`diff`** — rendered/semantic diff between refs.
5. **`rename`** — ID rewrite across links, wikilinks and query filters.
6. **`verify artifact`** — against the E1.14 manifest; includes deciding how
   the manifest itself is signed.
7. **`sign` / `sign verify`** — ledger format, then certificate provider, then
   OIDC device flow.
8. **`component add/update/remove`** — submodules and composed-graph
   validation.
9. **`release tag`** — gate plus bill-of-materials; unblocks Content Frozen
   After Release.
10. **`serve` + HTTP API + editor** — starting with the TipTap round-trip
   fidelity spike deferred from High-Level Design §4.7 (now DES-0037). Last because it's a
   thin client over everything above.
11. **`plugin`**.
