#!/usr/bin/env bash
set -euo pipefail
root=$(cd "$(dirname "$0")" && pwd)
source=$(realpath "$1"); output=$(realpath -m "$2"); stack=${3:-default}
revision=$(git -C "$source" rev-parse HEAD)
mkdir -p "$(dirname "$output")"
modfile="$output.mod"
cat > "$modfile" <<MOD
module github.com/wasmbench/wasmbench/adapters/wago

go 1.26.0
require (
 github.com/wago-org/wago v0.0.0
 github.com/wago-org/wasi v0.3.1
 github.com/wago-org/component-model v0.1.6
 github.com/wasmbench/wasmbench v0.0.0
 golang.org/x/sys v0.44.0
)
replace github.com/wago-org/wago => "$source"
replace github.com/wasmbench/wasmbench => "$root/wasm-bench"
MOD
cp "$root/adapter-dependencies.sum" "${output}.sum"
# Require Go 1.27.1 and patched TinyGo 0.42.0 on PATH.
export GOTOOLCHAIN=local GOMAXPROCS=2
export GOFLAGS="-buildvcs=false -mod=mod -modfile=$modfile"
extra=(); if [ "$stack" != default ]; then extra+=("-stack-size=$stack"); fi
cd "$root/wasm-bench/adapters/wago"
printf '%s\n' "source=$source revision=$revision scheduler=tasks gc=conservative opt=2 stack=$stack" > "$output.receipt"
tinygo build -scheduler=tasks -gc=conservative -no-debug -opt=2 "${extra[@]}" -ldflags="-X main.sourceRevision=$revision" -o "$output" .
sha256sum "$output" >> "$output.receipt"
cp "$output.receipt" "$root/$(basename "$output").receipt"
cp "$modfile" "$root/$(basename "$output").mod.receipt"
cp "${output}.sum" "$root/$(basename "$output").sum.receipt"
