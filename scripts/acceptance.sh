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
	OCC-0001 OCC-0002 OCC-0003 RSK-0001 VER-0001 VER-0002 EVD-0001 EVD-0002 ECO-0001 DOC-0001; do
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
"$PROV" export website >/dev/null
expect 0 '<li>IEC 62304</li>'               cat _site/entities/DES-0001.html
expect 0 'test, analysis'                   cat _site/entities/REQ-0002.html
expect 0 '<th>Serial number</th>'           cat _site/entities/EVD-0001.html
expect 0 '<td>BES-2201</td>'                cat _site/entities/EVD-0001.html
expect 0 '<th>Cause</th>'                   cat _site/entities/RSK-0001.html
sed -i.orig 's/of: Equipment/of: Equipmnet/' schema/VerificationEvidence.yaml
expect 2 '^export: schema/VerificationEvidence\.yaml:[0-9]+: field "equipment_used": unknown list item type "Equipmnet"' "$PROV" export website
mv schema/VerificationEvidence.yaml.orig schema/VerificationEvidence.yaml

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
