#!/usr/bin/env sh
set -eu

git rev-parse --git-dir >/dev/null 2>&1 || {
	printf '%s\n' "wago: not inside a git repository" >&2
	exit 1
}

root=$(git rev-parse --show-toplevel)
cd "$root"

files=$(git diff --cached --name-only --diff-filter=ACMR -- '*.go')
[ -n "$files" ] || exit 0

printf '%s\n' "wago: running gofmt on staged Go files"

changed=0
for file in $files; do
	[ -f "$file" ] || continue
	before=$(git hash-object "$file")
	gofmt -w "$file"
	after=$(git hash-object "$file")
	if [ "$before" != "$after" ]; then
		changed=1
	fi
done

if [ "$changed" -ne 0 ]; then
	printf '%s\n' "wago: gofmt updated staged files; review and commit again" >&2
	git --no-pager diff --stat -- $files >&2
	exit 1
fi

printf '%s\n' "wago: gofmt ok"
