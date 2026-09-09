#!/usr/bin/env sh
# Refresh the retained large compile-only workload.
set -eu

here=$(cd "$(dirname "$0")" && pwd)
corpus=$(cd "$here/.." && pwd)
curl -fsSL "https://cdn.jsdelivr.net/npm/esbuild-wasm@0.21.5/esbuild.wasm" \
	-o "$corpus/workloads/compile/esbuild.wasm"
printf 'corpus: refreshed esbuild.wasm; update catalog.json only after review\n'
