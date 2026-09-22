#!/usr/bin/env bash
set -euo pipefail

source_dir="${1:?pass a clean tree 2.2.1 checkout}"
sdk="${WASI_SDK_PATH:?set WASI_SDK_PATH to wasi-sdk-34.0}"
output_dir="$(cd "$(dirname "$0")" && pwd)"
if [[ "$(git -C "$source_dir" rev-parse HEAD)" != d501b58ff9cbfd64272c8cbcad0bda36a3fada06 ]]; then
  echo 'tree checkout is not at the pinned revision' >&2
  exit 1
fi
if [[ -n "$(git -C "$source_dir" status --porcelain)" ]]; then
  echo 'tree checkout must be clean' >&2
  exit 1
fi
if ! "$sdk/bin/clang" --version | head -1 | grep -q '23.1.0-wasi-sdk'; then
  echo 'build with WASI SDK 34' >&2
  exit 1
fi
"$sdk/bin/clang" -O2 -std=c11 -D_LARGEFILE_SOURCE -D_FILE_OFFSET_BITS=64 \
  -I "$output_dir/compat" -o "$output_dir/tree.wasm" "$source_dir"/*.c
wasm-strip "$output_dir/tree.wasm"
shasum -a 256 "$output_dir/tree.wasm"
