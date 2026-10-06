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

failures=0

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
expect 0 '<th>Verifies</th><td><a class="id" href="REQ-0001\.html"' cat _site/entities/VER-0001.html
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
expect 2 '^export: DOC/DOC-0001\.md:[0-9]+: query block: unknown operator "=" \(valid operators: equals, not_equals, greater_or_equal, less_or_equal, greater_than, less_than, exists\)$' "$PROV" export website
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
expect 0 '<a class="ref xref" href="#caption-bench-setup">Figure 1</a>' cat _site/entities/EVD-0001.html
expect 0 '^bench-setup\.jpg control-loop\.svg ecap-response\.png $' sh -c "ls _site/assets | tr '\\n' ' '"
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
# REF- entities are outside it, listed as plain IDs. A cited entity is an
# input only of pages that list citations: editing REF-0002 moves DOC-0001's
# last-changed commit, not REQ-0003's.
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
req3_changed() { "$PROV" export website >/dev/null && grep -o 'last changed in <code>[0-9a-f]*' _site/entities/REQ-0003.html; }
BEFORE=$(req3_changed)
sed -i 's/^year: 2020/year: 2020 /' REF/REF-0002.md && gitc commit -qam "Edit REF-0002"
expect 0 "^last changed in <code>$(git rev-parse --short=7 HEAD)\$" last_changed
expect 0 "^$BEFORE\$" req3_changed
git checkout -q --detach "$PIN"

# Standing E1 acceptance: exporting twice gives an identical site, and the
# site matches the golden snapshot (make golden-update rewrites it).
"$PROV" export website --out "$WORK/a" >/dev/null
"$PROV" export website --out "$WORK/b" >/dev/null
expect 0 ''                               diff -r "$WORK/a" "$WORK/b"
expect 0 ''                               diff -ru "$GOLDEN_DIR" "$WORK/a"

if [ "$failures" -ne 0 ]; then
	echo "$failures acceptance step(s) failed"
	exit 1
fi
echo "all acceptance steps passed"
