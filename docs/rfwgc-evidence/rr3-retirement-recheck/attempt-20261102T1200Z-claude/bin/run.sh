#!/bin/bash
# usage: run.sh <label> <srcdir-name> <tags|-> <race:0|1> <timeout> <run-regex>
# Sources: immutable `git archive <pin> -- . ':!docs'` trees under $SRCROOT (hashes in ../source-archives.txt).
set -u
B=$(dirname "$(dirname "$(readlink -f "$0")")")
SRCROOT=/home/dev/.cache/rr3-retirement-recheck-src
label=$1 src=$2 tags=$3 race=$4 to=$5 re=$6
log=$B/logs/$label.log
[ -e "$log" ] && { echo "refuse overwrite $log" >&2; exit 99; }
export GOROOT=/home/dev/.local/share/mise/installs/go/1.27.1 GOTOOLCHAIN=local GOPROXY=off GOFLAGS= GOENV=off
export PATH=$GOROOT/bin:/usr/bin:/bin
args=(test -count=1 -timeout "$to" -v -run "$re")
[ "$tags" != "-" ] && args+=(-tags="$tags")
[ "$race" = 1 ] && args+=(-race)
cd "$SRCROOT/$src" || exit 98
{ echo "# label=$label src=$src tags=$tags race=$race"; echo "# $(go version) GOROOT=$(go env GOROOT)"; echo "# overlay: $(ls src/wago/rr3x_*_test.go 2>/dev/null | xargs -r sha256sum | tr '\n' ' ')"; echo "# go ${args[*]} ./src/wago"; } > "$log"
go "${args[@]}" ./src/wago >> "$log" 2>&1
rc=$?
printf '%s\t%s\t%s\t%s\t%s\n' "$label" "$src" "$tags" "$race" "$rc" >> "$B/logs/exits.tsv"
echo "$label rc=$rc pass=$(grep -c -- '^--- PASS' "$log") fail=$(grep -c -- '--- FAIL' "$log") skip=$(grep -c -- '--- SKIP' "$log") race=$(grep -c 'DATA RACE' "$log")"
