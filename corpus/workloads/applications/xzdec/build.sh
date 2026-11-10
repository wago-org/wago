#!/usr/bin/env bash
set -euo pipefail

# Source: XZ Utils 5.8.4, release archive xz-5.8.4.tar.xz.
# SHA-256: 4ce24038fd4221e0d13bc1a2de7a4db56e90b92b3bf75321f6c14be73f65de4b
source_dir="${1:?pass unpacked xz-5.8.4 source directory}"
build_dir="${2:?pass a new build directory outside the source tree}"
sdk="${WASI_SDK_PATH:?set WASI_SDK_PATH to wasi-sdk-34.0}"
output="$(cd "$(dirname "$0")" && pwd)/xzdec.wasm"
if [[ "$(shasum -a 256 "$source_dir/src/xzdec/xzdec.c" | cut -d ' ' -f 1)" != c0bd8f5dbd5b5569ef2e934bfe902b71f002ba831e5fc03fc4073b77c16deaab ]]; then
  echo 'XZ Utils source does not match 5.8.4' >&2
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
  -DXZ_THREADS=no -DXZ_SANDBOX=no -DXZ_NLS=OFF \
  -DXZ_TOOL_XZ=OFF -DXZ_TOOL_XZDEC=ON -DXZ_TOOL_LZMADEC=OFF -DXZ_TOOL_LZMAINFO=OFF
cmake --build "$build_dir" --parallel 8 --target xzdec
cp "$build_dir/xzdec" "$output"
wasm-strip "$output"
shasum -a 256 "$output"
