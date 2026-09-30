#!/usr/bin/env bash
set -euo pipefail

# Brotli v1.2.0, commit 028fb5a23661f123017c060daa546b55cf4bde29.
source_dir="${1:?pass a clean Brotli checkout at the pinned revision}"
build_dir="${2:?pass a new build directory outside the source tree}"
sdk="${WASI_SDK_PATH:?set WASI_SDK_PATH to wasi-sdk-34.0}"
output_dir="$(cd "$(dirname "$0")" && pwd)"
if [[ "$(git -C "$source_dir" rev-parse HEAD)" != 028fb5a23661f123017c060daa546b55cf4bde29 ]]; then
  echo 'Brotli checkout is not at the pinned revision' >&2
  exit 1
fi
if [[ -n "$(git -C "$source_dir" status --porcelain)" ]]; then
  echo 'Brotli checkout must be clean' >&2
  exit 1
fi
if [[ -e "$build_dir" ]]; then
  echo 'build directory already exists' >&2
  exit 1
fi
if ! "$sdk/bin/clang" --version | head -1 | grep -q '23.1.0-wasi-sdk'; then
  echo 'build with WASI SDK 34' >&2
  exit 1
fi
cmake -S "$source_dir" -B "$build_dir" \
  -DCMAKE_TOOLCHAIN_FILE="$sdk/share/cmake/wasi-sdk-p1.cmake" \
  -DCMAKE_BUILD_TYPE=Release -DBUILD_SHARED_LIBS=OFF \
  -DCMAKE_C_FLAGS="-D_WASI_EMULATED_PROCESS_CLOCKS -include $output_dir/wasi_compat.h" \
  -DCMAKE_EXE_LINKER_FLAGS='-lwasi-emulated-process-clocks'
cmake --build "$build_dir" --parallel 8 --target brotli
cp "$build_dir/brotli" "$output_dir/brotli.wasm"
wasm-strip "$output_dir/brotli.wasm"
shasum -a 256 "$output_dir/brotli.wasm"
