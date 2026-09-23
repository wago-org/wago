#!/usr/bin/env bash
set -euo pipefail

# jq 1.8.2 release archive: jq-1.8.2.tar.gz
# SHA-256: 71b8d6e8f5fe81f6c6d0d110e3892251f6ce76ed095abd315e26e6e1193af3af
source_dir="${1:?pass unpacked jq-1.8.2 source directory}"
build_dir="${2:?pass a new build directory outside the source tree}"
sdk="${WASI_SDK_PATH:?set WASI_SDK_PATH to wasi-sdk-34.0}"
output="$(cd "$(dirname "$0")" && pwd)/jq.wasm"
if [[ "$(shasum -a 256 "$source_dir/configure" | cut -d ' ' -f 1)" != 62a3c16a23b44794b3188a85960a01961fb46c074993c2a9761aa2585780308d ]] ||
   [[ "$(shasum -a 256 "$source_dir/src/jq.h" | cut -d ' ' -f 1)" != 976689e8c3dc0b4b7d7bba50dc560277c638694681a356be61ae93787b335f8d ]]; then
  echo 'jq source does not match 1.8.2' >&2
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
mkdir -p "$build_dir"
(
  cd "$build_dir"
  CC="$sdk/bin/clang" AR="$sdk/bin/ar" RANLIB="$sdk/bin/ranlib" \
    CFLAGS='-O2 -D_WASI_EMULATED_SIGNAL' \
    LDFLAGS='-lwasi-emulated-signal -Wl,-z,stack-size=1048576' \
    "$source_dir/configure" --host=wasm32-wasi --disable-docs \
      --disable-valgrind --disable-maintainer-mode --with-oniguruma=builtin
  make --jobs=8
)
cp "$build_dir/jq" "$output"
wasm-strip "$output"
shasum -a 256 "$output"
