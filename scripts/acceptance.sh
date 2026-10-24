#!/usr/bin/env bash
# Acceptance steps: run the built binary against the pinned provenance-example
# checkout. Every task adds steps for the behaviour it introduces.
#
# Run via `make acceptance`, which builds the binary, prepares the checkout and
# sets PROV and EXAMPLE_DIR.
set -euo pipefail

PROV=${PROV:?PROV must point at the provenance binary}
EXAMPLE_DIR=${EXAMPLE_DIR:?EXAMPLE_DIR must point at the provenance-example checkout}
GOLDEN_DIR=${GOLDEN_DIR:?GOLDEN_DIR must point at testdata/golden}
VALIDATE_DIR=${VALIDATE_DIR:?VALIDATE_DIR must point at testdata/validate}
VIOLATIONS_DIR=${VIOLATIONS_DIR:?VIOLATIONS_DIR must point at testdata/violations}

failures=0

# The JUnit reports are checked against Jenkins' junit-10.xsd with xmllint
# (libxml2-utils on Debian and Ubuntu).
if ! command -v xmllint >/dev/null; then
	echo "acceptance needs xmllint: install libxml2-utils (apt) or libxml2 (brew)" >&2
	exit 2
fi
SCHEMAS_DIR=$(cd "$(dirname "$0")/../testdata/schemas" && pwd)

# sarif_dev replaces the binary's version in a SARIF report with dev, as
# the golden reports have it (make golden-update).
sarif_dev() {
	sed -E '/"name": "provenance",/{n;s/"version": "[^"]*"/"version": "dev"/;}' "$1"
}

# expect <exit-code> <extended-regex> <command...>
# Runs the command, then checks its exit code and that its combined
# stdout/stderr matches the regex (an empty regex skips the output check).
expect() {
	local want_code=$1 pattern=$2
	shift 2
	local output code=0
	output=$("$@" 2>&1) || code=$?

	local problem=""
	if [ "$code" -ne "$want_code" ]; then
		problem="exit code $code, want $want_code"
	elif [ -n "$pattern" ] && ! grep -Eq -- "$pattern" <<<"$output"; then
		problem="output does not match /$pattern/"
	fi

	local shown=${*/#$PROV/prov}
	if [ -z "$problem" ]; then
		echo "ok    $shown"
	else
		echo "FAIL  $shown: $problem"
		sed 's/^/      | /' <<<"$output"
		failures=$((failures + 1))
	fi
}

WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT

cd "$EXAMPLE_DIR"

# S0.2: the binary runs inside the example repository.
expect 0 '^  export +'                    "$PROV" --help
expect 0 '^provenance '                   "$PROV" version
expect 0 '^commit: [0-9a-f]{40}$'         "$PROV" version
expect 0 '^go: go[0-9]'                   "$PROV" --version

# E1.1: exporter registry and output folder.
expect 0 '^exported website to _site$'    "$PROV" export website
expect 0 '<h1>NeuroPulse Implantable Stimulator System \(NEURO\)</h1>' cat _site/index.html
expect 0 '^exported website to _site$'    "$PROV" export website
expect 2 '^export: pdf is not available in this release$'  "$PROV" export pdf
expect 2 '^export: docx is not available in this release$' "$PROV" export docx
expect 2 '^export: unknown format "latex"' "$PROV" export latex
mkdir -p "$WORK/notmine" && touch "$WORK/notmine/x"
expect 2 'not empty and has no \.provenance-export marker' "$PROV" export website --out "$WORK/notmine"
expect 0 '^x$'                            ls "$WORK/notmine"
cd REQ  # repository root is found from a subfolder; --out is relative to the working directory
expect 0 '^exported website to \.\./_site$' "$PROV" export website --out ../_site
cd ..

# E1.2: entity discovery and parsing.
"$PROV" export website >/dev/null
for id in USR-0001 USR-0002 REQ-0001 REQ-0002 REQ-0003 DES-0001 SEV-0001 SEV-0002 SEV-0003 \
	OCC-0001 OCC-0002 OCC-0003 RSK-0001 VER-0001 VER-0002 EVD-0001 EVD-0002 ECO-0001 DOC-0001 DOC-0002; do
	expect 0 ">$id<"                      cat _site/index.html
done
expect 0 '<h2>VerificationEvidence</h2>' cat _site/index.html
cp REQ/REQ-0001.md REQ/copy.md
expect 2 '^export: duplicate entity ID REQ-0001 in REQ/REQ-0001\.md and REQ/copy\.md$' "$PROV" export website
rm REQ/copy.md

# E1.3: schema loading and default entity pages. (Requirement has a project
# template since E1.6, so the built-in page is checked on a Design.)
"$PROV" export website >/dev/null
expect 0 '<tr><th>Status</th><td>approved</td></tr>'  cat _site/entities/DES-0001.html
expect 0 '<tr><th>Order</th><td>1</td></tr>'          cat _site/entities/DES-0001.html
expect 0 'A control loop running on the implant'       cat _site/entities/DES-0001.html
expect 0 'The system shall automatically adjust'       cat _site/entities/REQ-0001.html
expect 0 'href="entities/REQ-0001.html"'               cat _site/index.html
sed -i.orig '0,/type: string/s//type: strnig/' schema/Requirement.yaml
expect 2 '^export: schema/Requirement\.yaml:[0-9]+: field "title": unknown type "strnig"' "$PROV" export website
mv schema/Requirement.yaml.orig schema/Requirement.yaml

# E1.4: links, reverse links and list fields.
"$PROV" export website >/dev/null
expect 1 ''                               grep -q '^verified_by:' REQ/REQ-0001.md
# REQ-0001's file never names VER-0001, so any link to it on the page comes
# from the derived verified_by facet (whichever template renders the page).
expect 0 'href="VER-0001\.html"'            cat _site/entities/REQ-0001.html
expect 0 'verifies <a class="ref" href="REQ-0001\.html"' cat _site/entities/VER-0001.html
expect 0 '<th>Implemented by</th><td><a class="id" href="REQ-0001\.html"' cat _site/entities/USR-0001.html
expect 0 '<td>BES-2201</td>'                cat _site/entities/EVD-0001.html
expect 0 '<td>PCP-0087</td>'                cat _site/entities/EVD-0001.html
sed -i.orig 's/^implements: \[REQ-0001, REQ-0002\]/implements: [REQ-9999, REQ-0002]/' DES/DES-0001.md
expect 0 '^exported website to _site$'    "$PROV" export website
expect 0 'REQ-9999</span> <span class="unresolved">unresolved</span>' cat _site/entities/DES-0001.html
mv DES/DES-0001.md.orig DES/DES-0001.md

# E1.5: Markdown and wikilinks.
"$PROV" export website >/dev/null
expect 0 'Editing <a class="ref" href="REQ-0001\.html"[^>]*>REQ-0001</a> anywhere' cat _site/entities/DOC-0001.html
expect 0 'ceiling is <a class="ref" href="REQ-0002\.html"[^>]*>Single-fault amplitude ceiling</a>' cat _site/entities/DOC-0001.html
expect 0 '<section class="embed" data-entity="REQ-0003">' cat _site/entities/DOC-0001.html
expect 0 'The pulse-generator ASIC shall enforce' cat _site/entities/DOC-0001.html
expect 0 '<h2>Scope</h2>'                   cat _site/entities/DOC-0001.html
cp DOC/DOC-0001.md "$WORK/DOC-0001.md"
printf '\n![[DOC-0001]]\n' >> DOC/DOC-0001.md
expect 2 '^export: DOC/DOC-0001\.md: embed cycle DOC-0001 → DOC-0001$' "$PROV" export website
cp "$WORK/DOC-0001.md" DOC/DOC-0001.md

# E1.6: project templates and site-level overrides.
"$PROV" export website >/dev/null
expect 0 '<article class="requirement" id="REQ-0001">' cat _site/entities/REQ-0001.html
expect 0 '<h2>Implements</h2>'              cat _site/entities/REQ-0001.html
expect 0 '<h1 class="requirement-title">'   cat _site/entities/REQ-0003.html
expect 0 '<table class="fields">'           cat _site/entities/DES-0001.html
expect 0 '<h3 class="requirement-title"><span class="req-id">REQ-0003</span>' cat _site/entities/DOC-0001.html
expect 0 '<header class="dhf-header">'      cat _site/entities/DES-0001.html
expect 0 'class="dhf-intro"'                cat _site/index.html
expect 0 ''                                 sh -c "tr -d '\r' < templates/style.css | cmp - _site/style.css"
mv templates/style.css "$WORK/style.css"
expect 0 '^exported website to _site$'      "$PROV" export website
expect 1 ''                                 cmp -s _site/style.css "$WORK/style.css"
expect 0 '<header class="dhf-header">'      cat _site/index.html
mv "$WORK/style.css" templates/style.css
cp templates/_layout.tmpl "$WORK/_layout.tmpl"
sed -i 's/{{template "content" .}}/{{template "content" .}/' templates/_layout.tmpl
expect 2 '^export: templates/_layout\.tmpl:[0-9]+: ' "$PROV" export website
cp "$WORK/_layout.tmpl" templates/_layout.tmpl

# Lists naming their type with of: — a built-in type, an enum or a record.
# The Equipment record gives rows to both kinds of evidence.
"$PROV" export website >/dev/null
expect 0 '<li>IEC 62304</li>'               cat _site/entities/DES-0001.html
expect 0 'test, analysis'                   cat _site/entities/REQ-0002.html
expect 0 '<th>Serial number</th>'           cat _site/entities/EVD-0001.html
expect 0 '<td>BES-2201</td>'                cat _site/entities/EVD-0001.html
expect 0 '<th>Serial number</th>'           cat _site/entities/VAL-0001.html
expect 0 '<td>CER-0310</td>'                cat _site/entities/VAL-0001.html
expect 0 '<th>Cause</th>'                   cat _site/entities/RSK-0001.html
sed -i.orig 's/of: Equipment/of: Equipmnet/' schema/VerificationEvidence.yaml
expect 2 '^export: schema/VerificationEvidence\.yaml:[0-9]+: field "equipment_used": unknown list item type "Equipmnet"' "$PROV" export website
mv schema/VerificationEvidence.yaml.orig schema/VerificationEvidence.yaml

# E1.8: query blocks. DOC-0001's Requirements section embeds the approved
# requirements in order; the result is live, so an uncommitted status change
# shows on the next export; a bad block fails at its file and line.
"$PROV" export website >/dev/null
DOC_FLAT="tr -d '\\n' < _site/entities/DOC-0001.html"
expect 0 '<h2>Requirements</h2><p>[^<]*</p><div class="query"><section class="embed" data-entity="REQ-0001">.*</section><section class="embed" data-entity="REQ-0002">' sh -c "$DOC_FLAT"
expect 0 '^1$' sh -c "grep -o 'data-entity=\"REQ-0003\"' _site/entities/DOC-0001.html | wc -l"  # only the ![[REQ-0003]] embed
sed -i.orig 's/^status: approved/status: draft/' REQ/REQ-0002.md
"$PROV" export website >/dev/null
expect 1 '' grep -q 'data-entity="REQ-0002"' _site/entities/DOC-0001.html
mv REQ/REQ-0002.md.orig REQ/REQ-0002.md
sed -i.orig 's/operator: equals/operator: "="/' DOC/DOC-0001.md
expect 2 '^export: DOC/DOC-0001\.md:[0-9]+: query block: unknown operator "=" \(valid operators: equals, not_equals, greater_or_equal, less_or_equal, greater_than, less_than, exists, missing, not_in\)$' "$PROV" export website
mv DOC/DOC-0001.md.orig DOC/DOC-0001.md

# E1.9: calculated fields. RSK-0001's row_rating is severity × occurrence
# per failure mode (SEV-0003 = 5; OCC-0001 = 1, OCC-0002 = 3) and
# overall_risk_rating their maximum; both follow a changed score.
"$PROV" export website >/dev/null
RSK_FLAT="tr -d '\\n' < _site/entities/RSK-0001.html"
expect 0 'OCC-0001</a></td><td>5</td></tr>.*OCC-0002</a></td><td>15</td></tr>' sh -c "$RSK_FLAT"
expect 0 '<tr><th>Overall risk rating</th><td>15</td></tr>' sh -c "$RSK_FLAT"
sed -i.orig 's/^score: 3/score: 4/' OCC/OCC-0002.md
"$PROV" export website >/dev/null
expect 0 'OCC-0002</a></td><td>20</td></tr>' sh -c "$RSK_FLAT"
expect 0 '<tr><th>Overall risk rating</th><td>20</td></tr>' sh -c "$RSK_FLAT"
mv OCC/OCC-0002.md.orig OCC/OCC-0002.md

# E1.9a: multi-type query blocks. DOC-0002 selects Risk and Requirement in
# one block, ordered together by order (ties by ID); a field only Risk has
# is empty on requirements; a field neither has fails at its line.
"$PROV" export website >/dev/null
DOC2_ORDER="grep -o 'data-entity=\"[A-Z]*-[0-9]*\"' _site/entities/DOC-0002.html | tr -d '\\n'"
expect 0 '^data-entity="REQ-0001"data-entity="RSK-0001"data-entity="REQ-0002"data-entity="REQ-0003"$' sh -c "$DOC2_ORDER"
sed -i.orig 's/^order_by: order/where: {field: hazard, operator: exists}\norder_by: order/' DOC/DOC-0002.md
"$PROV" export website >/dev/null
expect 0 '^data-entity="RSK-0001"$' sh -c "$DOC2_ORDER"
sed -i 's/field: hazard,/field: hazrd,/' DOC/DOC-0002.md
expect 2 '^export: DOC/DOC-0002\.md:[0-9]+: query block: neither Risk nor Requirement has a field "hazrd"$' "$PROV" export website
mv DOC/DOC-0002.md.orig DOC/DOC-0002.md

# E1.9b: per-query templates. DOC-0002 shows requirements through the named
# requirement-checklist template and RSK-0001 through risk-summary, while
# DOC-0001 keeps the full Requirement template; a missing template fails at
# the block's line; without templates: every result uses its default.
"$PROV" export website >/dev/null
DOC2_FLAT="tr -d '\\n' < _site/entities/DOC-0002.html"
expect 0 '<section class="embed" data-entity="REQ-0001"><div class="checklist-item" id="REQ-0001">' sh -c "$DOC2_FLAT"
expect 0 '<section class="embed" data-entity="RSK-0001"><article class="risk-summary" id="RSK-0001">' sh -c "$DOC2_FLAT"
expect 0 '<h3 class="requirement-title"><span class="req-id">REQ-0001</span>' cat _site/entities/DOC-0001.html
mv templates/risk-summary.tmpl "$WORK/risk-summary.tmpl"
expect 2 '^export: DOC/DOC-0002\.md:[0-9]+: query block: unknown template "risk-summary": there is no templates/risk-summary\.tmpl' "$PROV" export website
mv "$WORK/risk-summary.tmpl" templates/risk-summary.tmpl
sed -i.orig '/^templates:/d; /^  Requirement: requirement-checklist/d; /^  Risk: risk-summary/d' DOC/DOC-0002.md
"$PROV" export website >/dev/null
expect 1 '' grep -q 'checklist-item' _site/entities/DOC-0002.html
expect 0 '<h3 class="requirement-title"><span class="req-id">REQ-0001</span>' cat _site/entities/DOC-0002.html
mv DOC/DOC-0002.md.orig DOC/DOC-0002.md

# E1.10: --scope. A Document scope renders that Document as the main page
# with only what it pulls in; an entity with nothing to pull in is alone, its
# links outside the scope plain IDs; a query file scopes to its results.
expect 0 "^exported website to $WORK/_doc\$" "$PROV" export website --scope DOC/DOC-0001.md --out "$WORK/_doc"
expect 0 '<h1>System Requirements Specification</h1>' cat "$WORK/_doc/index.html"
expect 0 '^DOC-0001\.html REQ-0001\.html REQ-0002\.html REQ-0003\.html $' sh -c "ls '$WORK/_doc/entities' | tr '\\n' ' '"
expect 0 '' "$PROV" export website --scope REQ/REQ-0001.md --out "$WORK/_req"
expect 0 '^REQ-0001\.html$' ls "$WORK/_req/entities"
expect 0 '<span class="ref out-of-scope">USR-0001</span>' cat "$WORK/_req/index.html"
expect 0 '' "$PROV" export website --scope scopes/approved-requirements.yaml --out "$WORK/_q"
expect 0 '^REQ-0001\.html REQ-0002\.html $' sh -c "ls '$WORK/_q/entities' | tr '\\n' ' '"
expect 2 '^export: --scope nope\.yaml: no such file$' "$PROV" export website --scope nope.yaml --out "$WORK/_x"

# E1.11: git context and content hash. The footer's content hash is the one
# verify content prints; an uncommitted edit marks it -dirty. Committing an
# edit to a requirement DOC-0001 shows moves DOC-0001's last-changed commit;
# committing one to ECO-0001, which it neither shows nor cites, does not.
# (DES-0001 is cited, and DOC-0001 lists its citations since E1.13a.) The
# commits are made on a detached HEAD and dropped afterwards.
"$PROV" export website >/dev/null
HASH=$("$PROV" verify content)
expect 0 '^sha256:[0-9a-f]{64}$' echo "$HASH"
expect 0 "content hash <code>$HASH</code>" cat _site/entities/DOC-0001.html
expect 0 '' "$PROV" verify content --expected "$HASH"
expect 1 'does not match the expected sha256:0$' "$PROV" verify content --expected sha256:0
echo " " >> DES/DES-0001.md
expect 0 "^$HASH-dirty\$" "$PROV" verify content
git checkout -q DES/DES-0001.md
PIN=$(git rev-parse HEAD)
last_changed() { "$PROV" export website >/dev/null && grep -o 'last changed in <code>[0-9a-f]*' _site/entities/DOC-0001.html; }
BEFORE=$(last_changed)
gitc() { git -c user.name=Acceptance -c user.email=acceptance@example.com -c commit.gpgsign=false "$@"; }
sed -i 's/^order: 1/order: 1 /' ECO/ECO-0001.md && gitc commit -qam "Edit ECO-0001"
expect 0 "^$BEFORE\$" last_changed
sed -i 's/^order: 1/order: 1 /' REQ/REQ-0001.md && gitc commit -qam "Edit REQ-0001"
expect 0 "^last changed in <code>$(git rev-parse --short=7 HEAD)\$" last_changed
expect 0 '<td>Edit REQ-0001</td>' cat _site/entities/DOC-0001.html
# Only what a page shows counts: the stylesheet is a separate file; a
# severity score shows on RSK-0001 through its calculated ratings.
AFTER=$(last_changed)
echo "/* tweak */" >> templates/style.css && gitc commit -qam "Restyle"
expect 0 "^$AFTER\$" last_changed
rsk_changed() { "$PROV" export website >/dev/null && grep -o 'last changed in <code>[0-9a-f]*' _site/entities/RSK-0001.html; }
sed -i 's/^label: Critical/label: Critical /' SEV/SEV-0003.md && gitc commit -qam "Edit SEV-0003"
expect 0 "^last changed in <code>$(git rev-parse --short=7 HEAD)\$" rsk_changed
echo "<!-- layout -->" >> templates/_layout.tmpl && gitc commit -qam "Edit layout"
expect 0 "^last changed in <code>$(git rev-parse --short=7 HEAD)\$" last_changed
git checkout -q --detach "$PIN"

# E1.12: captions and images. DOC-0001 captions two figures and a table
# where it uses them and refers to them with [[#id]]; the images are copied
# into the site. A figure captioned above the first takes number 1, and the
# references follow.
"$PROV" export website >/dev/null
expect 0 '<a class="ref xref" href="#caption-control-loop">Figure 1</a>' cat _site/entities/DOC-0001.html
expect 0 '<a class="ref xref" href="#caption-ecap-response">Figure 2</a>' cat _site/entities/DOC-0001.html
expect 0 '<span class="caption-number">Table 1</span> Stimulation amplitude limits by level\.' cat _site/entities/DOC-0001.html
expect 0 '<img src="\.\./assets/control-loop\.svg"' cat _site/entities/DOC-0001.html
expect 0 '<a class="ref xref" href="#caption-bench-setup">Figure 3</a>' cat _site/entities/EVD-0001.html
expect 0 '^bench-setup\.jpg control-loop\.svg ecap-response\.png evd-0001-profile-1\.svg evd-0001-pulse\.svg evd-0002-lead-open\.svg $' sh -c "ls _site/assets | tr '\\n' ' '"
expect 0 '' cmp assets/ecap-response.png _site/assets/ecap-response.png
cp DOC/DOC-0001.md "$WORK/DOC-0001.md"
python3 - <<'PY'
p = "DOC/DOC-0001.md"
s = open(p, newline="").read()
nl = "\r\n" if "\r\n" in s else "\n"
first = "![Closed-loop amplitude control]"
s = s.replace(first, nl.join(["![Bench](../assets/bench-setup.jpg)", "", "```caption", "kind: figure", "```", "", first]), 1)
open(p, "w", newline="").write(s)
PY
"$PROV" export website >/dev/null
expect 0 '<a class="ref xref" href="#caption-control-loop">Figure 2</a>' cat _site/entities/DOC-0001.html
expect 0 '<a class="ref xref" href="#caption-ecap-response">Figure 3</a>' cat _site/entities/DOC-0001.html
sed -i 's/^kind: table/kind: chart/' DOC/DOC-0001.md
expect 2 '^export: DOC/DOC-0001\.md:[0-9]+: unknown caption kind "chart" \(kinds: equation, figure, table\)$' "$PROV" export website
cp "$WORK/DOC-0001.md" DOC/DOC-0001.md
sed -i.orig 's|(\.\./assets/control-loop\.svg)|(../assets/missing.svg)|' DOC/DOC-0001.md
expect 2 '^export: DOC/DOC-0001\.md:[0-9]+: image assets/missing\.svg does not exist$' "$PROV" export website
mv DOC/DOC-0001.md.orig DOC/DOC-0001.md

# E1.13a: reference lists. DOC-0001's Document template lists what the page
# cites, with nothing added to its Markdown: internal entities, then the
# Reference entities in first-citation order, REF-0002 being cited only inside
# the embedded REQ-0003. REQ-0003's own page (Requirement template) lists
# nothing. Citing REF-0002 earlier moves it up. With DOC-0001 as the scope the
# REF- entities are outside it, listed as plain IDs. Without a _cite.tmpl
# (E1.13b), a cited entity is an input only of pages that list citations:
# editing REF-0002 moves DOC-0001's last-changed commit, not REQ-0003's.
"$PROV" export website >/dev/null
refs() { grep -o 'href="REF-000[0-9]\.html" title="[^"]*">REF-000[0-9]</a>)</li>' _site/entities/DOC-0001.html | grep -o 'REF-000[0-9]<' | tr -d '<' | tr '\n' ' '; }
expect 0 '^REF-0003 REF-0002 REF-0001 $' refs
expect 0 '<h3>Internal</h3>' cat _site/entities/DOC-0001.html
expect 1 '' grep -q 'class="references"' _site/entities/REQ-0003.html
sed -i.orig 's/^repository updates what/repository (or [[REF-0002]]) updates what/' DOC/DOC-0001.md
"$PROV" export website >/dev/null
expect 0 '^REF-0002 REF-0003 REF-0001 $' refs
mv DOC/DOC-0001.md.orig DOC/DOC-0001.md
"$PROV" export website --scope DOC/DOC-0001.md --out "$WORK/doc" >/dev/null
expect 0 '<span class="ref out-of-scope">REF-0003</span>\)</li>' cat "$WORK/doc/index.html"
PIN=$(git rev-parse HEAD)
gitc rm -q templates/_cite.tmpl && gitc commit -qm "Drop _cite.tmpl"
req3_changed() { "$PROV" export website >/dev/null && grep -o 'last changed in <code>[0-9a-f]*' _site/entities/REQ-0003.html; }
BEFORE=$(req3_changed)
sed -i 's/^year: 2020/year: 2020 /' REF/REF-0002.md && gitc commit -qam "Edit REF-0002"
expect 0 "^last changed in <code>$(git rev-parse --short=7 HEAD)\$" last_changed
expect 0 "^$BEFORE\$" req3_changed
git checkout -q --detach "$PIN"

# E1.13b: citation templates. The example's _cite.tmpl shows each Reference
# as its number in DOC-0001's External list, linked to its page, and a label
# as a locator; other citations and [[ID#field]] render as without it.
# REQ-0003's own page numbers its own citations. Without _cite.tmpl the
# citations show their IDs again. _cite.tmpl is an input of pages that cite
# anything: editing it moves DOC-0001's last-changed commit, not REQ-0001's.
# With it, a cited entity is an input of every page citing it: editing
# REF-0002 moves REQ-0003's too.
"$PROV" export website >/dev/null
cites() { grep -o '\[<a class="ref" href="REF-000[0-9]\.html" title="[^"]*">[0-9]*</a>[^]]*\]' "_site/entities/$1.html" | sed 's/ title="[^"]*"//; s/<a class="ref" href="\(REF-000[0-9]\)\.html">/\1:/; s|</a>||' | tr '\n' ' '; }
expect 0 '^\[REF-0003:1\] \[REF-0002:2\] \[REF-0001:3, clause 7\] $' cites DOC-0001
expect 0 '^\[REF-0002:1\] $'          cites REQ-0003
expect 0 'single-fault conditions of \[<a class="ref" href="REF-0002\.html"' cat _site/entities/DOC-0001.html
expect 0 'The system-level ceiling is <a class="ref" href="REQ-0002\.html" title="[^"]*">Single-fault amplitude ceiling</a>' cat _site/entities/DOC-0001.html
expect 0 'decomposed from the user needs in <a class="ref" href="USR-0001\.html"' cat _site/entities/DOC-0001.html
mv templates/_cite.tmpl "$WORK/_cite.tmpl"
"$PROV" export website >/dev/null
expect 0 'approach evaluated clinically in <a class="ref" href="REF-0003\.html" title="[^"]*">REF-0003</a>\.' cat _site/entities/DOC-0001.html
expect 1 '' grep -q '\[<a class="ref"' _site/entities/DOC-0001.html
mv "$WORK/_cite.tmpl" templates/_cite.tmpl
PIN=$(git rev-parse HEAD)
req1_changed() { "$PROV" export website >/dev/null && grep -o 'last changed in <code>[0-9a-f]*' _site/entities/REQ-0001.html; }
BEFORE=$(req1_changed)
sed -i 's/^\[{{link/[{{- link/' templates/_cite.tmpl && gitc commit -qam "Edit _cite.tmpl"
expect 0 "^last changed in <code>$(git rev-parse --short=7 HEAD)\$" last_changed
expect 0 "^$BEFORE\$" req1_changed
sed -i 's/^year: 2020/year: 2020 /' REF/REF-0002.md && gitc commit -qam "Edit REF-0002"
expect 0 "^last changed in <code>$(git rev-parse --short=7 HEAD)\$" req3_changed
git checkout -q --detach "$PIN"

# E1 review: a query block lists entities without citing them, so DOC-0002's
# References, listed by identifier, are not numbered and it has no reference
# list. DES-0001 numbers its equation. Exporting from a linked worktree (git
# worktree add), whose objects are in the main repository, gives the same
# site.
"$PROV" export website >/dev/null
expect 0 '<li><a class="ref" href="REF-0001\.html" title="[^"]*">ISO 14971:2019</a></li>' cat _site/entities/DOC-0002.html
expect 0 '<li><a class="ref" href="REQ-0003\.html" title="[^"]*">REQ-0003</a></li>' cat _site/entities/DOC-0002.html
expect 1 '' grep -q 'class="references"' _site/entities/DOC-0002.html
expect 0 'as <a class="ref xref" href="#caption-control-law">Equation 1</a>' cat _site/entities/DES-0001.html
git worktree add -q --detach "$WORK/worktree" HEAD
(cd "$WORK/worktree" && "$PROV" export website --out "$WORK/from-worktree" >/dev/null)
"$PROV" export website --out "$WORK/from-checkout" >/dev/null
expect 0 ''                               diff -r "$WORK/from-checkout" "$WORK/from-worktree"
git worktree remove --force "$WORK/worktree"

# E2.2: validate reports every file it cannot load, each at its file and
# line, and exits 1: unparsable frontmatter, an id without a type, every
# copy of a duplicated ID, an unknown type, a schema problem (which skips
# the rules). Tool and usage errors exit 2. --out writes the report to a
# file; --path validates another repository.
expect 0 '^0 errors, 1 warning$'          "$PROV" validate
cp REQ/REQ-0002.md REQ/copy.md
sed -i '2s/.*/id: OCC-0003: x/' OCC/OCC-0003.md
printf -- '---\nid: REQ-0099\ntitle: no type\n---\n' > REQ/half.md
expect 1 '^OCC/OCC-0003\.md:2: error \[content/frontmatter\] invalid frontmatter: mapping values are not allowed in this context$' "$PROV" validate
expect 1 '^REQ/REQ-0002\.md:2: error \[content/duplicate-id\] duplicate entity ID REQ-0002, also in REQ/copy\.md \(REQ-0002\)$' "$PROV" validate
expect 1 '^REQ/copy\.md:2: error \[content/duplicate-id\] duplicate entity ID REQ-0002, also in REQ/REQ-0002\.md \(REQ-0002\)$' "$PROV" validate
expect 1 '^REQ/half\.md:2: error \[content/frontmatter\] frontmatter has an id but no type, so the file is not read as an entity$' "$PROV" validate
expect 1 '^4 errors, 1 warning$'          "$PROV" validate
expect 1 '^validate: 4 errors, 1 warning$' "$PROV" validate --out "$WORK/reports/v.txt"
expect 0 '^4 errors, 1 warning$'          tail -n 1 "$WORK/reports/v.txt"
git checkout -q -- OCC/OCC-0003.md && rm REQ/copy.md REQ/half.md
sed -i 's/^type: Risk/type: Rsk/' RSK/RSK-0001.md
expect 1 '^RSK/RSK-0001\.md:3: error \[schema/unknown-type\] type "Rsk" is not declared in schema/ ' "$PROV" validate
git checkout -q -- RSK/RSK-0001.md
sed -i 's/type: ApprovalStatus/type: AprovalStatus/' schema/Requirement.yaml
expect 1 '^schema/Requirement\.yaml:20: error \[schema/file\] field "status": unknown type "AprovalStatus"' "$PROV" validate
expect 1 '^12 rules not run: the schema has errors$' "$PROV" validate
git checkout -q -- schema/Requirement.yaml
expect 2 '^validate: invalid --format "xml"' "$PROV" validate --format xml
expect 2 '^validate: not inside a provenance repository' "$PROV" validate --path "$WORK"
cd "$WORK"
expect 0 '^0 errors, 1 warning$'          "$PROV" validate --path "$EXAMPLE_DIR/REQ"
cd "$EXAMPLE_DIR"

# E2.3: schema conformance. Each entity is checked against its type and
# the schema against the DSL: an undeclared field, a value outside its enum,
# a required field not set, a key the DSL doesn't have (default: included),
# a value not of its type, an ID not of its type's form. Each exits 1 at its
# line; a declaration nothing uses is a warning only.
sed -i 's/^order: 1/order: 1\ncolour: red/' REQ/REQ-0001.md
expect 1 '^REQ/REQ-0001\.md:9: error \[schema/unknown-field\] Requirement has no field "colour" \(fields: ' "$PROV" validate
git checkout -q -- REQ/REQ-0001.md
sed -i 's/^status: approved/status: aproved/' REQ/REQ-0001.md
expect 1 '^REQ/REQ-0001\.md:5: error \[schema/enum-value\] status: "aproved" is not one of ApprovalStatus: draft, in_review, approved, deprecated \(REQ-0001\)$' "$PROV" validate
git checkout -q -- REQ/REQ-0001.md
sed -i '/^title:/d' REQ/REQ-0001.md
expect 1 '^REQ/REQ-0001\.md:2: error \[schema/required\] required field title is not set \(REQ-0001\)$' "$PROV" validate
git checkout -q -- REQ/REQ-0001.md
sed -i 's/^\(    type: ApprovalStatus\)/\1\n    default: draft/' schema/Design.yaml
expect 1 '^schema/Design\.yaml:[0-9]+: error \[schema/unknown-key\] field "status": unknown key "default": fields have no defaults' "$PROV" validate
git checkout -q -- schema/Design.yaml
sed -i 's/^execution_date: .*/execution_date: 2026-13-04/' VER/EVD-0001.md
expect 1 '^VER/EVD-0001\.md:8: error \[schema/invalid-value\] execution_date: not a date \(YYYY-MM-DD\), got 2026-13-04 \(EVD-0001\)$' "$PROV" validate
git checkout -q -- VER/EVD-0001.md
sed -i 's/^id: DES-0001/id: DES-01/' DES/DES-0001.md
expect 1 '^DES/DES-0001\.md:2: error \[schema/id-format\] ID DES-01 is not DES-NNNN' "$PROV" validate
git checkout -q -- DES/DES-0001.md
printf 'enum: Unused\nvalues: [a]\n' > schema/enums/Unused.yaml
expect 0 '^schema/enums/Unused\.yaml:1: warning \[schema/unused-enum\] enum Unused is not used by any field$' "$PROV" validate
expect 0 '^0 errors, 2 warnings$'              "$PROV" validate
rm schema/enums/Unused.yaml

# E2.4: content and links. validate renders the site as export does but
# reports every problem, each at its line: a query block that can't run, a
# missing image, a wikilink to no entity, a block this version can't
# render; export still stops at the first. A link field to an entity of a
# type its field doesn't allow is reported too.
sed -i '52s/operator: equals/operator: "="/' DOC/DOC-0001.md
sed -i '34s|../assets/ecap-response.png|../assets/missing.png|' DOC/DOC-0001.md
sed -i '65s/\[\[REQ-0002\]\]/[[REQ-0999]]/' DOC/DOC-0001.md
printf '\n```mermaid\ngraph TD\n```\n' >> DOC/DOC-0001.md
expect 1 '^DOC/DOC-0001\.md:34: error \[content/image\] image assets/missing\.png does not exist \(DOC-0001\)$' "$PROV" validate
expect 1 '^DOC/DOC-0001\.md:52: error \[content/query\] query block: unknown operator "="' "$PROV" validate
expect 1 '^DOC/DOC-0001\.md:65: error \[links/unresolved\] \[\[REQ-0999\]\]: no entity REQ-0999 \(DOC-0001\)$' "$PROV" validate
expect 1 '^DOC/DOC-0001\.md:[0-9]+: error \[content/unrendered-block\] mermaid block: ' "$PROV" validate
expect 1 '^4 errors, 1 warning$'           "$PROV" validate
expect 2 '^export: DOC/DOC-0001\.md:52: query block: unknown operator "="' "$PROV" export website --out "$WORK/broken"
git checkout -q -- DOC/DOC-0001.md
sed -i 's/^implements: \[USR-0001\]/implements: [REQ-0002]/' REQ/REQ-0001.md
expect 1 '^REQ/REQ-0001\.md:6: error \[links/target-type\] implements: REQ-0002 is of type Requirement, but implements links only to UserNeed \(REQ-0001\)$' "$PROV" validate
git checkout -q -- REQ/REQ-0001.md
printf 'x\n' > templates/spare.tmpl
expect 0 '^templates/spare\.tmpl: warning \[content/unused-template\] named template spare is not chosen by any query block$' "$PROV" validate
rm templates/spare.tmpl

# E2.5: the grammar's sub-queries, ^name, value_of, missing and not_in.
# DOC-0003 lists what would fail each design review check: nothing while the
# example passes, but for REQ-0003's open verification (E2.6); a stale
# calibration row, or evidence run before the build it tested, appears in
# its section. The removed forms are reported with the form that replaced them.
"$PROV" export website >/dev/null
expect 0 '^11$'                           sh -c "grep -o 'class=\"query-empty\"' _site/entities/DOC-0003.html | wc -l"
sed -i 's/calibration_due_date: 2026-09-30/calibration_due_date: 2026-08-01/' VER/EVD-0001.md
sed -i 's/^build_date: 2026-08-06/build_date: 2026-08-20/' BLD/BLD-0002.md
"$PROV" export website >/dev/null
DOC3_FLAT="tr -d '\\n' < _site/entities/DOC-0003.html"
expect 0 '>Equipment calibration</h2>.*href="EVD-0001\.html".*>Evidence before its build</h2>.*href="EVD-0002\.html".*>Verification coverage</h2>' sh -c "$DOC3_FLAT"
expect 0 '^9$'                            sh -c "grep -o 'class=\"query-empty\"' _site/entities/DOC-0003.html | wc -l"
git checkout -q -- VER BLD
sed -i 's/^  field: equipment_used/  field: {list: equipment_used, subfield: calibration_due_date}/' DOC/DOC-0003.md
expect 1 '^DOC/DOC-0003\.md:[0-9]+: error \[content/query\] query block: \{list: L, subfield: S\} is now a sub-query: \{field: L, where: \{field: S, …\}\} \(DOC-0003\)$' "$PROV" validate
git checkout -q -- DOC/DOC-0003.md

# E2.6: aggregation, and sub-queries on from. DOC-0003 lists REQ-0003 as
# unverified (a count over the verified_by reverse link); removing VER-0002's
# link adds REQ-0002 there and VER-0002 under protocols verifying nothing (a
# count over a link); a copied title puts both requirements under duplicate
# titles (another requirement, found by from, with an equal ^title).
"$PROV" export website >/dev/null
DOC3_FLAT="tr -d '\\n' < _site/entities/DOC-0003.html"
expect 0 '>Verification coverage</h2><p>[^<]*<code>[^<]*</code>[^<]*</p><ul class="query"><li><a class="ref" href="REQ-0003\.html"' sh -c "$DOC3_FLAT"
sed -i '/^verifies: \[REQ-0002\]/d' VER/VER-0002.md
sed -i 's/^title: Amplitude ceiling — subsystem allocation/title: Single-fault amplitude ceiling/' REQ/REQ-0003.md
"$PROV" export website >/dev/null
expect 0 '>Verification coverage</h2>.*href="REQ-0002\.html".*href="REQ-0003\.html".*href="VER-0002\.html".*>Risk-benefit analysis</h2>' sh -c "$DOC3_FLAT"
expect 0 '>Duplicate titles</h2>.*href="REQ-0002\.html".*href="REQ-0003\.html"' sh -c "$DOC3_FLAT"
git checkout -q -- VER REQ
sed -i 's/^  from: Requirement/  from: Requirment/' DOC/DOC-0003.md
expect 1 '^DOC/DOC-0003\.md:[0-9]+: error \[content/query\] query block: from: unknown type "Requirment" ' "$PROV" validate
git checkout -q -- DOC/DOC-0003.md

# E2.7: transitive links and code_blocks. DOC-0003's decomposition-cycle
# and block-language sections are empty; making REQ-0003, which refines
# REQ-0002, REQ-0002's parent closes a cycle that lists both (a link
# followed to any depth), and a python block lists the entity it is in.
"$PROV" export website >/dev/null
expect 0 '^11$'                           sh -c "grep -o 'class=\"query-empty\"' _site/entities/DOC-0003.html | wc -l"
sed -i 's/^order: 2/order: 2\nparent_requirement: REQ-0003/' REQ/REQ-0002.md
printf '\n```python\nprint("amplitude")\n```\n' >> DES/DES-0001.md
"$PROV" export website >/dev/null
DOC3_FLAT="tr -d '\\n' < _site/entities/DOC-0003.html"
expect 0 '>Decomposition cycles</h2>.*href="REQ-0002\.html".*href="REQ-0003\.html".*>Block languages</h2>.*href="DES-0001\.html"' sh -c "$DOC3_FLAT"
expect 0 '^9$'                            sh -c "grep -o 'class=\"query-empty\"' _site/entities/DOC-0003.html | wc -l"
git checkout -q -- REQ DES
sed -i 's/^  transitive: true/  transitive: yes/' DOC/DOC-0003.md
expect 1 '^DOC/DOC-0003\.md:[0-9]+: error \[content/query\] query block: transitive is true or false \(DOC-0003\)$' "$PROV" validate
git checkout -q -- DOC/DOC-0003.md

# E2.8: rule files. The example's rules run at the gate: REQ-0003's open
# verification is a warning, and each break from E2.5–E2.7 is a violation
# at the line D8 gives: a missing field at the id, a list row at the row,
# an aggregate over an absent reverse link at the id, a cycle at each
# member's link. A rule file with a problem is reported at its line and
# skipped, so its own violations go.
expect 0 '^REQ/REQ-0003\.md:2: warning \[requirement-verified\] requirement missing verification \(REQ-0003\)$' "$PROV" validate
sed -i '/^risk_benefit_analysis:/,/^  "unacceptable"/d' RSK/RSK-0001.md
expect 1 '^RSK/RSK-0001\.md:2: error \[risk-benefit-required\] risk-benefit analysis required when residual acceptability is unacceptable \(RSK-0001\)$' "$PROV" validate
git checkout -q -- RSK
sed -i 's/calibration_due_date: 2026-09-30/calibration_due_date: 2026-08-01/' VER/EVD-0001.md
expect 1 '^VER/EVD-0001\.md:51: error \[equipment-calibration-current\] equipment calibration was not current at time of test execution \(EVD-0001\)$' "$PROV" validate
git checkout -q -- VER
sed -i '/^verifies: \[REQ-0002\]/d' VER/VER-0002.md
expect 1 '^VER/VER-0002\.md:2: error \[protocol-verifies-requirement\] verification protocol not linked to any requirement \(VER-0002\)$' "$PROV" validate
expect 1 '^REQ/REQ-0002\.md:2: warning \[requirement-verified\] requirement missing verification \(REQ-0002\)$' "$PROV" validate
expect 1 '^1 error, 2 warnings$'           "$PROV" validate
git checkout -q -- VER
sed -i 's/^order: 2/order: 2\nparent_requirement: REQ-0003/' REQ/REQ-0002.md
expect 1 '^REQ/REQ-0002\.md:9: error \[no-requirement-decomposition-cycles\] ' "$PROV" validate
expect 1 '^REQ/REQ-0003\.md:6: error \[no-requirement-decomposition-cycles\] ' "$PROV" validate
git checkout -q -- REQ
sed -i 's/^severity: warning/severity: fatal/' rules/requirement-verified.yaml
expect 1 '^rules/requirement-verified\.yaml:4: error \[rules/severity\] severity is error or warning, not "fatal"$' "$PROV" validate
expect 1 '^1 error, 0 warnings$'           "$PROV" validate
git checkout -q -- rules
printf 'id: requirement-verified\n' >> rules/requirement-verified.yaml
expect 1 '^rules/requirement-verified\.yaml:[0-9]+: error \[rules/unknown-key\] unknown key "id": a rule.s ID is its file name, requirement-verified$' "$PROV" validate
git checkout -q -- rules

# E2.9: report formats. json, junit and sarif report the same violations
# as text, at the same lines, with the same exit code; the JUnit report
# conforms to junit-10.xsd (the SARIF report's conformance to the 2.1.0
# schema is a unit test). Breaking two entities puts both in every format.
for f in json junit sarif; do
	expect 0 ''                           "$PROV" validate --format "$f" --out "$WORK/clean.$f"
done
expect 0 'validates$'                     xmllint --noout --schema "$SCHEMAS_DIR/junit-10.xsd" "$WORK/clean.junit"
expect 0 '<testcase name="risk-benefit-required" classname="rules"></testcase>' cat "$WORK/clean.junit"
sed -i '/^risk_benefit_analysis:/,/^  "unacceptable"/d' RSK/RSK-0001.md
sed -i 's/calibration_due_date: 2026-09-30/calibration_due_date: 2026-08-01/' VER/EVD-0001.md
expect 1 '"errors": 2,'                   "$PROV" validate --format json
expect 1 ''                               "$PROV" validate --format junit --out "$WORK/broken.junit"
expect 0 'validates$'                     xmllint --noout --schema "$SCHEMAS_DIR/junit-10.xsd" "$WORK/broken.junit"
expect 0 '<failure type="error" message="risk-benefit analysis required when residual acceptability is unacceptable">RSK/RSK-0001\.md:2: ' cat "$WORK/broken.junit"
expect 1 ''                               "$PROV" validate --format sarif --out "$WORK/broken.sarif"
SARIF_FLAT="tr -d ' \n' < $WORK/broken.sarif"
expect 0 '"ruleId":"equipment-calibration-current".{0,400}"uri":"VER/EVD-0001\.md","uriBaseId":"%SRCROOT%"},"region":\{"startLine":51\}' sh -c "$SARIF_FLAT"
expect 0 '"ruleId":"risk-benefit-required".{0,400}"uri":"RSK/RSK-0001\.md","uriBaseId":"%SRCROOT%"},"region":\{"startLine":2\}' sh -c "$SARIF_FLAT"
git checkout -q -- RSK VER

# E2.10: report templates. validation.html, written into the exported
# site, lists every check and rule (all passed but the REQ-0003 warning)
# and links each violation to its entity's page; breaking VER-0002's link
# puts its two violations in it, and in the Markdown pull-request
# summary. A report template that cannot render is a violation.
"$PROV" export website >/dev/null
expect 0 ''                               "$PROV" validate --report validation.html --out _site/validation.html
expect 0 ''                               test -f _site/index.html
VALIDATION_FLAT="tr -d '\\n' < _site/validation.html"
expect 0 '<code>risk-benefit-required</code><br>[^<]*</td><td>error</td><td><span class="status status-approved">passed</span>' sh -c "$VALIDATION_FLAT"
expect 0 '<a class="ref" href="entities/REQ-0003\.html" title="[^"]*">REQ-0003</a></td><td><code>REQ/REQ-0003\.md:2</code>' sh -c "$VALIDATION_FLAT"
expect 0 '^### Design controls: passed$'           "$PROV" validate --report pr-summary.md
"$PROV" validate --report validation.html >"$WORK/r1.html"
"$PROV" validate --report validation.html >"$WORK/r2.html"
expect 0 ''                               diff "$WORK/r1.html" "$WORK/r2.html"
sed -i 's/^verifies: \[REQ-0002\]/verifies: [REQ-0099]/' VER/VER-0002.md
expect 1 'validate: 2 errors, 2 warnings$' "$PROV" validate --report validation.html --out _site/validation.html
expect 0 '^2$'                            sh -c "grep -o '<a class=\"ref\" href=\"entities/VER-0002\.html\"' _site/validation.html | wc -l"
expect 0 ''                               test -f _site/entities/VER-0002.html
expect 1 '^\| error \| `VER/VER-0002\.md:6` \| `links/unresolved` \| verifies: no entity REQ-0099 \|$' "$PROV" validate --report pr-summary.md
git checkout -q -- VER
sed -i 's/{{\.Summary\.Errors}} error/{{.Summary.Failures}} error/' templates/reports/pr-summary.md.tmpl
expect 1 '^templates/reports/pr-summary\.md\.tmpl:[0-9]+: error \[content/template\] executing .* map has no entry for key "Failures"$' "$PROV" validate
expect 1 '^validate: report pr-summary\.md: templates/reports/pr-summary\.md\.tmpl:[0-9]+: ' "$PROV" validate --report pr-summary.md
git checkout -q -- templates
expect 2 'unknown report "nope": there is no templates/reports/nope\.tmpl \(reports: pr-summary\.md, validation\.html\)' "$PROV" validate --report nope

# Standing E1 acceptance: exporting twice gives an identical site, and the
# site matches the golden snapshot (make golden-update rewrites it).
"$PROV" export website --out "$WORK/a" >/dev/null
"$PROV" export website --out "$WORK/b" >/dev/null
expect 0 ''                               diff -r "$WORK/a" "$WORK/b"
expect 0 ''                               diff -ru "$GOLDEN_DIR" "$WORK/a"

# Standing E2 acceptance: validating twice gives an identical report, and
# the clean example's report matches testdata/validate/clean.* in every
# format (make golden-update rewrites them).
"$PROV" validate >"$WORK/v1.txt"
"$PROV" validate >"$WORK/v2.txt"
expect 0 ''                               diff "$WORK/v1.txt" "$WORK/v2.txt"
expect 0 ''                               diff -u "$VALIDATE_DIR/clean.txt" "$WORK/v1.txt"
for f in json:json junit:junit.xml sarif:sarif; do
	"$PROV" validate --format "${f%%:*}" >"$WORK/clean.${f#*:}"
	[ "${f%%:*}" = sarif ] && sarif_dev "$WORK/clean.sarif" >"$WORK/clean.sarif.dev" && mv "$WORK/clean.sarif.dev" "$WORK/clean.sarif"
	expect 0 ''                           diff -u "$VALIDATE_DIR/clean.${f#*:}" "$WORK/clean.${f#*:}"
done

# Standing E2 acceptance: every failure validate catches, in
# testdata/violations, is reported in every format as
# testdata/validate/violations-* records, and exits 1 (make golden-update
# rewrites them); the JUnit reports conform to junit-10.xsd.
for r in core broken-schema; do
	for f in text:txt json:json junit:junit.xml sarif:sarif; do
		out="$WORK/violations-$r.${f#*:}"
		expect 1 '' "$PROV" validate --path "$VIOLATIONS_DIR/$r" --format "${f%%:*}" --out "$out"
		[ "${f%%:*}" = sarif ] && sarif_dev "$out" >"$out.dev" && mv "$out.dev" "$out"
		expect 0 '' diff -u "$VALIDATE_DIR/violations-$r.${f#*:}" "$out"
	done
	expect 0 'validates$' xmllint --noout --schema "$SCHEMAS_DIR/junit-10.xsd" "$WORK/violations-$r.junit.xml"
done

if [ "$failures" -ne 0 ]; then
	echo "$failures acceptance step(s) failed"
	exit 1
fi
echo "all acceptance steps passed"
