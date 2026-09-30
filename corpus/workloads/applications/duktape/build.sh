#!/usr/bin/env bash
set -euo pipefail

# Requires WASI SDK 34, CMake, and WABT wasm-strip. The source is saghul/wasi-lab
# at 05d2c175afeed626187f792c9dd1a8142e11f95a; its embedded Duktape
# source reports upstream commit 44ca54f726bfa651a7ab59286dd5c371dba2ddfc.
source_dir="${1:?pass a clean wasi-lab checkout at the pinned revision}"
build_dir="${2:?pass an empty build directory outside the checkout}"
sdk="${WASI_SDK_PATH:?set WASI_SDK_PATH to wasi-sdk-34.0}"
output="$(cd "$(dirname "$0")" && pwd)/duk.wasm"
if [[ "$(git -C "$source_dir" rev-parse HEAD)" != 05d2c175afeed626187f792c9dd1a8142e11f95a ]]; then
  echo 'wasi-lab checkout is not at the pinned revision' >&2
  exit 1
fi
if [[ -n "$(git -C "$source_dir" status --porcelain)" ]]; then
  echo 'wasi-lab checkout must be clean' >&2
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
cmake -S "$source_dir/wasiduk" -B "$build_dir" \
  -DCMAKE_TOOLCHAIN_FILE="$sdk/share/cmake/wasi-sdk-p1.cmake" \
  -DCMAKE_POLICY_VERSION_MINIMUM=3.5 \
  -DCMAKE_EXE_LINKER_FLAGS='-Wl,-z,stack-size=1048576' \
  -DCMAKE_BUILD_TYPE=Release
cmake --build "$build_dir" --parallel 8
cp "$build_dir/duk" "$output"
wasm-strip "$output"
shasum -a 256 "$output"
