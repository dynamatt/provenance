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

cd "$EXAMPLE_DIR"

# S0.2: the binary runs inside the example repository.
expect 0 '^  exportx +'                    "$PROV" --help
expect 0 '^provenance '                   "$PROV" version
expect 0 '^commit: [0-9a-f]{40}$'         "$PROV" version
expect 0 '^go: go[0-9]'                   "$PROV" --version

if [ "$failures" -ne 0 ]; then
	echo "$failures acceptance step(s) failed"
	exit 1
fi
echo "all acceptance steps passed"
