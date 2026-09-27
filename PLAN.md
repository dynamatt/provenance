# Implementation Plan

> **Status: approved by Matt (2026-09-26).** Exported from the Notion
> [Implementation Plan](https://app.notion.com/p/3e74f3521211810ba328edefa07e8528).
> This file is Claude Code's working plan: task checkboxes are tracked here.
> The Notion page stays the reviewed baseline and changes only when the plan
> itself changes — update both together when it does.

## Progress

- [x] S0.1 Go module and command skeleton
- [x] S0.2 Build tooling and example-repo fixture
- [x] S0.3 CLAUDE.md, PLAN.md and website CLI reference
- [x] S0.4 Design reconciliation
- [ ] S1.1 GitHub Actions CI workflow
- [ ] S1.2 Reproducible-build check
- [ ] E1.1 Exporter interface and output folder
- [ ] E1.2 Entity discovery and parsing
- [ ] E1.3 Schema loading, default entity pages, golden test
- [ ] E1.4 Links, reverse links and list fields
- [ ] E1.5 Markdown and wikilinks
- [ ] E1.6 Project templates and template functions
- [ ] E1.7 Query engine spike (decision task)
- [ ] E1.8 Query blocks
- [ ] E1.9 Calculated fields
- [ ] E1.10 `--scope` and Document export
- [ ] E1.11 Git context and content hash
- [ ] E1.12 Captioned entities and cross-reference numbering
- [ ] E1.13 Bundled Mermaid and offline guarantee
- [ ] E1.14 Epic close-out: docs and first binary release

## Shape of the plan

- **Stage 0 — Skeleton CLI.** Every command from Detailed Design §2 exists as a
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
   something marked OPEN, the same PR (a) updates the relevant Notion page —
   Requirements Spec, High-Level Design or Detailed Design — marking the item
   DECIDED with a date, and (b) updates `provenance-website` where the change
   is user-visible (CLI reference, architecture page). The PR description lists
   these under *Design changes*. If the change is more than a clarification,
   stop and ask Matt before implementing.
6. **Determinism.** No map-iteration order, wall-clock timestamps or absolute
   paths in any output. Running the same command twice on the same commit
   produces byte-identical results.
7. **Exit code contract from day one** (Detailed Design §2): `0` success, `1`
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

**Check it yourself:** VER-0001's page shows *verifies: REQ-0001* even though
VER-0001's file never mentions it. EVD-0001 shows its equipment table. Change a
link to `REQ-9999` → marked unresolved, exit 0.

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
back to the built-in template. Template functions: `markdown`, `heading N` with
relative depth resolved at render time, link helpers. Data model per Detailed
Design §7. **Example-repo PR:** rename `Requirement.tmpl.illustrative` →
`Requirement.tmpl`, update `templates/README.md` and the README's "nothing in
templates is functional" note; bump the pin.

**Check it yourself:** REQ pages use the project template ("Implements"
heading); DES pages still use the fallback. REQ-0003 embedded in DOC-0001
renders its headings deeper than on its own page.

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

### E1.12 Captioned entities and cross-reference numbering

**Deliverables:** two-pass render — number captioned entities in document
order per render, then resolve inline references to in-scope captioned entities
as "Figure N". Where the numbering scheme is configured (Requirements Spec §7
calls it "a stylesheet concern") is not yet designed — propose and record it in
Detailed Design §7. **Example-repo PR:** add a captioned-figure schema, one
figure entity, and a reference to it from DOC-0001.

**Check it yourself:** DOC-0001 shows "Figure 1" under the figure and in the
sentence referencing it. Add a second figure above the first → numbers swap,
text follows.

### E1.13 Bundled Mermaid and offline guarantee

**Deliverables:** Mermaid JS checked in under an embedded asset folder with a
pinned version and SHA-256 verified at build; copied into the site; `mermaid`
fences render client-side. Acceptance step fails if any `src`/`href` in the
output loads an asset from a remote host. Reproducible-build job stays green
(the asset is a checked-in file, not a bundler output). draw.io stays deferred
until the example repo has a draw.io asset. **Example-repo PR:** add a Mermaid
diagram to DES-0001.

**Check it yourself:** turn networking off, open
`_site/entities/DES-0001.html` → diagram renders.

### E1.14 Epic close-out: docs and first binary release

**Deliverables:** website `export` reference page complete (usage, scope
files, template authoring and template functions, context variables);
architecture page updated where implementation refined the design; example-repo
README updated for what now works. GitHub release `v0.1.0-alpha` built by CI
with a `SHA256SUMS` manifest (format recorded in Detailed Design §4 — it's what
`verify artifact` will check later).

**Check it yourself:** download the release binary on a second machine,
`sha256sum` matches `SHA256SUMS`, and `export website` on the example repo
matches the golden site.

## Later epics (outline)

Detailed task breakdowns are written when the preceding epic closes. Proposed
order, with the reason it comes where it does:

1. **`validate`** — the merge gate, and it reuses Epic 1's schema and query
   core. Schema meta-validation first, then one task per rule type, each proven
   by locally breaking one example entity to trigger its existing rule file.
   `--format json|junit|sarif` (settled in S0.4). Adds a CI job in
   `provenance-example` that runs the gate. Signature Presence and Content
   Frozen After Release wait for `sign` and `release tag`, since the example's
   signature proof fields are placeholders.
2. **`fmt`** — canonical serialization; `fmt --check` joins the gate. Needed
   before the editor can write files.
3. **`init`** — medical-device starter template embedded in the binary; a fresh
   `init` must pass `validate` and `export`.
4. **`report`** — coverage metrics over the shared query engine.
5. **`diff`** — rendered/semantic diff between refs.
6. **`rename`** — ID rewrite across links, wikilinks and query filters.
7. **`verify artifact`** — against the E1.14 manifest; includes deciding how
   the manifest itself is signed.
8. **`sign` / `sign verify`** — ledger format, then certificate provider, then
   OIDC device flow.
9. **`component add/update/remove`** — submodules and composed-graph
   validation.
10. **`release tag`** — gate plus bill-of-materials; unblocks Content Frozen
    After Release.
11. **`serve` + HTTP API + editor** — starting with the TipTap round-trip
    fidelity spike deferred from High-Level Design §4.7. Last because it's a
    thin client over everything above.
12. **`plugin`**.
