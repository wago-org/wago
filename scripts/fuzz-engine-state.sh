#!/bin/sh
set -eu

repo_dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
starshine_wasm=${STARSHINE_FFI_WASM:-"$repo_dir/tests/enginefuzz/starshine-ffi.wasm"}
worker_dir="$repo_dir/.tmp/engine-state"
worker="$worker_dir/railshot-worker"

if [ ! -f "$starshine_wasm" ]; then
	echo "engine-state fuzz: Starshine FFI not found: $starshine_wasm" >&2
	echo "Restore the tracked artifact or set STARSHINE_FFI_WASM to another build" >&2
	exit 1
fi

mkdir -p "$worker_dir"
cd "$repo_dir"
go build -o "$worker" ./tests/enginefuzz/worker
exec node scripts/fuzz-engine-state.mjs \
	--worker "$worker" \
	--starshine "$starshine_wasm" \
	"$@"
