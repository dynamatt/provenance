#!/usr/bin/env bash
# golden-stamps.sh <dir>: replaces the commit and content hash every page of
# an exported site in <dir> is stamped with by placeholders, in place, so
# the golden site (testdata/golden) changes only where a page does, not on
# every move of the example pin. Which commit a page last changed in is
# kept: it is the page's own. The real stamps are checked by their own
# acceptance steps (E1.11).
set -euo pipefail
dir=${1:?usage: golden-stamps.sh <dir>}
find "$dir" -name '*.html' -exec sed -i -E \
	's#Generated from commit <code>[0-9a-f]+</code> · content hash <code>sha256:[0-9a-f]{64}</code>#Generated from commit <code>HEAD</code> · content hash <code>sha256:CONTENT</code>#' {} +
