#!/bin/bash
# usage: run.sh <label> <pin> <tags|-> <race:0|1> <timeout> <run-regex>
set -u
B=$(dirname "$(dirname "$(readlink -f "$0")")")
label=$1 pin=$2 tags=$3 race=$4 to=$5 re=$6
log=$B/logs/$label.log
[ -e "$log" ] && { echo "refuse overwrite $log" >&2; exit 99; }
export GOROOT=/home/dev/.local/share/mise/installs/go/1.27.1 GOTOOLCHAIN=local GOPROXY=off GOFLAGS= GOENV=off
export PATH=$GOROOT/bin:/usr/bin:/bin
args=(test -count=1 -timeout "$to" -v -run "$re")
[ "$tags" != "-" ] && args+=(-tags="$tags")
[ "$race" = 1 ] && args+=(-race)
cd "$B/src/$pin" || exit 98
{ echo "# label=$label pin=$pin tags=$tags race=$race"; echo "# $(go version) GOROOT=$(go env GOROOT)"; echo "# go ${args[*]} ./src/wago"; } > "$log"
go "${args[@]}" ./src/wago >> "$log" 2>&1
rc=$?
printf '%s\t%s\t%s\t%s\t%s\n' "$label" "$pin" "$tags" "$race" "$rc" >> "$B/logs/exits.tsv"
echo "$label rc=$rc pass=$(grep -c -- '--- PASS' "$log") fail=$(grep -c -- '--- FAIL' "$log") race=$(grep -c 'DATA RACE' "$log")"
