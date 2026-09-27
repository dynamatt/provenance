#!/usr/bin/env bash
# Acceptance steps: run the built binary against the pinned provenance-example
# checkout. Every task adds steps for the behaviour it introduces.
#
# Run via `make acceptance`, which builds the binary, prepares the checkout and
# sets PROV and EXAMPLE_DIR.
set -euo pipefail

PROV=${PROV:?PROV must point at the provenance binary}
EXAMPLE_DIR=${EXAMPLE_DIR:?EXAMPLE_DIR must point at the provenance-example checkout}

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

# Standing E1 acceptance: exporting twice gives an identical site.
"$PROV" export website --out "$WORK/a" >/dev/null
"$PROV" export website --out "$WORK/b" >/dev/null
expect 0 ''                               diff -r "$WORK/a" "$WORK/b"

if [ "$failures" -ne 0 ]; then
	echo "$failures acceptance step(s) failed"
	exit 1
fi
echo "all acceptance steps passed"
