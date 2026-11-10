#!/usr/bin/env sh
set -eu

git rev-parse --git-dir >/dev/null 2>&1 || {
	printf '%s\n' "wago: not inside a git repository" >&2
	exit 1
}

git config core.hooksPath .githooks
printf '%s\n' "wago: installed git hooks from .githooks"
