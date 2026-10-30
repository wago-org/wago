#!/usr/bin/env bash
# Run from the Wago checkout. No worktree, source edits, or shared fixture writes.
set -eu
baseline=9713878ab452848f7eb7549a90be94b938a898be
qualification_tmp=$(mktemp -d /tmp/wago-baseline-repro.XXXXXX)
trap 'rm -rf -- "$qualification_tmp"' EXIT
mkdir -p "$qualification_tmp/source" "$qualification_tmp/cache" "$qualification_tmp/tmp"
git archive "$baseline" | tar -x -C "$qualification_tmp/source"
cd "$qualification_tmp/source"
for fixture in spec-v1 spec-v2 spec-v3; do
  case "$fixture" in
    spec-v1) revision=a8bcbafe6d2fb191ce0188de0e18fdc107fa2598; url=https://github.com/WebAssembly/testsuite ;;
    spec-v2) revision=05ca4182176763112561ae20153975c12bd689e4; url=https://github.com/WebAssembly/spec ;;
    spec-v3) revision=9d36019973201a19f9c9ebb0f10828b2fe2374aa; url=https://github.com/WebAssembly/spec ;;
  esac
  git init "tests/conformance/$fixture"
  timeout --kill-after=5s 180s git -C "tests/conformance/$fixture" fetch --depth=1 "$url" "$revision"
  git -C "tests/conformance/$fixture" checkout --detach "$revision"
done
unset GOWORK
export DOTNET_SYSTEM_GLOBALIZATION_INVARIANT=1 GOMAXPROCS=8 GOFLAGS=-p=8
export MISE_TRUSTED_CONFIG_PATHS="$PWD" GOCACHE="$qualification_tmp/cache" GOTMPDIR="$qualification_tmp/tmp"
ulimit -c 0
# Preserve the status of each bounded test; failure does not prevent the next run.
set +e
timeout --kill-after=15s 900s go test -count=1 -timeout=5m ./... > /tmp/wago-baseline-repro-ordinary.log 2>&1
printf 'ordinary exit=%s\n' "$?"
timeout --kill-after=15s 900s go test -count=1 -timeout=5m -tags=wago_regalloccheck ./... > /tmp/wago-baseline-repro-regalloccheck.log 2>&1
printf 'regalloccheck exit=%s\n' "$?"
set -e
# Only repeat staged tests after correcting the WABT version.
timeout --kill-after=5s 120s sh scripts/bootstrap-wabt.sh --verify
export PATH="$PWD/.tools/wabt-1.0.41-linux-x64/bin:$PATH"
set +e
timeout --kill-after=15s 300s go test -count=1 -timeout=4m -run '^TestStaged' ./src/wago > /tmp/wago-baseline-repro-staged.log 2>&1
printf 'staged exit=%s\n' "$?"
timeout --kill-after=15s 300s go test -count=1 -timeout=4m -tags=wago_regalloccheck -run '^TestStaged' ./src/wago > /tmp/wago-baseline-repro-staged-checked.log 2>&1
printf 'staged checked exit=%s\n' "$?"
# This currently stops with missing dune; do not install a system toolchain.
timeout --kill-after=5s 60s sh scripts/bootstrap-spec-interpreter.sh --verify > /tmp/wago-baseline-repro-interpreter.log 2>&1
printf 'interpreter bootstrap exit=%s\n' "$?"
