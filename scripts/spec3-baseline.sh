#!/bin/sh
set -u

repo=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
out=${1:-"$repo/tests/spec-v3-baseline.json"}
mkdir -p "$repo/.validation"
log="$repo/.validation/spec3-baseline.log"

set +e
make -C "$repo" spec3 >"$log" 2>&1
status=$?
set -e

python3 "$repo/scripts/spec3-baseline.py" "$log" "$out" --exit-code "$status"
cat "$log"
echo "spec3-baseline: wrote $out (make spec3 exit $status)" >&2
exit "$status"
