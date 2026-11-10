#!/usr/bin/env sh
# Run the GitHub Actions CI workflow (.github/workflows/ci.yml) locally with
# `act` (https://github.com/nektos/act), so you can reproduce a CI failure
# without pushing. Runner image and arch come from .actrc at the repo root.
#
# Usage:
#   scripts/ci-local.sh            # run all jobs for the pull_request event
#   scripts/ci-local.sh lint       # run only the lint job
#   scripts/ci-local.sh test       # run only the test job
#   scripts/ci-local.sh test -v    # extra args after the job pass through to act
set -eu

git rev-parse --git-dir >/dev/null 2>&1 || {
	printf '%s\n' "wago: not inside a git repository" >&2
	exit 1
}

command -v act >/dev/null 2>&1 || {
	printf '%s\n' "wago: 'act' not found (install: https://github.com/nektos/act)" >&2
	exit 1
}

docker info >/dev/null 2>&1 || {
	printf '%s\n' "wago: docker is not running; act needs a working Docker daemon" >&2
	exit 1
}

root=$(git rev-parse --show-toplevel)
cd "$root"

# A first positional arg that isn't a flag selects a single job (lint|test).
job=""
if [ "$#" -gt 0 ] && [ "${1#-}" = "$1" ]; then
	job="$1"
	shift
fi

set -- pull_request ${job:+-j "$job"} "$@"
printf '%s\n' "wago: act $*"
exec act "$@"
