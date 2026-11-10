#!/usr/bin/env bash
set -euo pipefail

# Source: https://www.sqlite.org/2026/sqlite-amalgamation-3530400.zip
# Archive SHA-256: 1e71ddf93849c6a6ecf58b827c0692073d2dd7ee40196158068f7b29f422e87d
# Upstream SHA3-256: 628a44cfe82c66aed1ccbbe85a562d2e33ebe64b3288981ed76285612227934e
source_dir="${1:?pass unpacked sqlite-amalgamation-3530400 directory}"
sdk="${WASI_SDK_PATH:?set WASI_SDK_PATH to wasi-sdk-34.0}"
output="$(cd "$(dirname "$0")" && pwd)/sqlite3.wasm"
if [[ "$(shasum -a 256 "$source_dir/sqlite3.c" | cut -d ' ' -f 1)" != b1dd5d74ec7f29055a6684fa06fb3c2f6821c87dd38f9a458dfd2e8a1db28189 ]] ||
   [[ "$(shasum -a 256 "$source_dir/shell.c" | cut -d ' ' -f 1)" != 8011ed018aa12969f93573b7bb1eae2d939d64d0f451b297ff847a0211c85179 ]]; then
  echo 'SQLite amalgamation source hashes do not match 3.53.4' >&2
  exit 1
fi
if ! "$sdk/bin/clang" --version | head -1 | grep -q '23.1.0-wasi-sdk'; then
  echo 'build with WASI SDK 34' >&2
  exit 1
fi
"$sdk/bin/clang" -O2 \
  -D_WASI_EMULATED_SIGNAL -D_WASI_EMULATED_PROCESS_CLOCKS -D_WASI_EMULATED_GETPID \
  -DSQLITE_OMIT_LOAD_EXTENSION -DSQLITE_THREADSAFE=0 \
  -Wl,-z,stack-size=1048576 \
  "$source_dir/shell.c" "$source_dir/sqlite3.c" "$(dirname "$output")/system_stub.c" \
  -lwasi-emulated-signal -lwasi-emulated-process-clocks -lwasi-emulated-getpid -lm \
  -o "$output"
wasm-strip "$output"
shasum -a 256 "$output"
