#!/usr/bin/env bash
set -euo pipefail
root=$(git rev-parse --show-toplevel)
work=${1:?Provide an empty directory outside the Go module cache}
mkdir -p "$work"
work=$(realpath "$work")
base=6a6684d2ecd2be2d17e5792733d1d0e03b2f2c0e
git clone --bare https://github.com/wago-org/wasi "$work/wasi.git"
for name in baseline candidate; do
  mkdir "$work/wasi-$name"
  git --git-dir="$work/wasi.git" archive "$base" | tar -x -C "$work/wasi-$name"
  patch -d "$work/wasi-$name" -p1 < "$root/docs/performance/setup-cleanup/continuation/patches/wasi-construction-tests.patch"
  cat > "$work/$name.work" <<EOF
go 1.22.0
use (
 "$root"
 "$root/bench"
 "$root/cli/wago-installer"
 "$work/wasi-$name"
)
replace github.com/wago-org/wago v0.1.0-beta.9 => "$root"
EOF
done
patch -d "$work/wasi-candidate" -p1 < "$root/docs/performance/setup-cleanup/continuation/patches/wasi-construction.patch"
for name in baseline candidate; do
  GOWORK="$work/$name.work" GOMAXPROCS=16 go test -c -tags wago_guardpage -o "$work/wasi-$name.test" "$root/bench/suite"
done
