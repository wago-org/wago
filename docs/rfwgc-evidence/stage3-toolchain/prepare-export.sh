#!/usr/bin/env bash
# Invoke from the assigned worktree. Destination must not already exist.
set -euo pipefail
revision=$1
destination=$2
fixture_source=${3:-/home/dev/Projects/wago/tests/conformance/spec-v3}
mkdir -p "$(dirname -- "$destination")"
mkdir "$destination"
git archive "$revision" | tar -x -C "$destination"
git clone --no-checkout "$fixture_source" "$destination/tests/conformance/spec-v3"
git -C "$destination/tests/conformance/spec-v3" checkout --detach 9d36019973201a19f9c9ebb0f10828b2fe2374aa
export PATH="${WAGO_OCAML_ROOT:-$HOME/.local/share/wago-rfwgc-opam}/rfwgc/bin:$PATH"
export DUNEJOBS=2
(cd "$destination" && sh scripts/bootstrap-spec-interpreter.sh --verify && sh scripts/bootstrap-wabt.sh --verify)
