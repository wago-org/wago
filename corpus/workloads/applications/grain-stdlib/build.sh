#!/usr/bin/env bash
# Verify reproducible guests without replacing any checked-in artifact.
set -euo pipefail
here="$(cd "$(dirname "$0")" && pwd)"
compiler="${GRAIN_COMPILER:?set GRAIN_COMPILER to the pinned grain-linux-x64 binary}"
compiler="$(cd "$(dirname "$compiler")" && pwd)/$(basename "$compiler")"
sha256() {
  if command -v sha256sum >/dev/null; then
    sha256sum "$1" | awk '{print $1}'
  else
    shasum -a 256 "$1" | awk '{print $1}'
  fi
}
check() {
  if [[ "$(sha256 "$1")" != "$2" ]]; then
    echo "grain-stdlib: SHA-256 mismatch: $1" >&2
    exit 1
  fi
}
check "$compiler" 82658891d33f5431e7bd260f0c00b8e86c43eb9182c5327f41db25d60b54dadd
check "$here/source.tar.gz" 81a2b0c304e8b613a1044c26c3bee8763b09d9f6c906359e913fc166de215436
stage="$(mktemp -d "${TMPDIR:-/tmp}/wago-grain.XXXXXX")"
trap 'rm -rf "$stage"' EXIT
tar -xzf "$here/source.tar.gz" -C "$stage"
mkdir "$stage/out"
cd "$stage/grain"
# Paths remain relative so assertion diagnostics do not embed a build directory.
# Compilation is sequential. Assertions remain enabled; the documented
# no-tail-call option keeps these shared fixtures within the Core 2 profile.
for name in array string json-subset; do
  source_name="$name"
  [[ "$name" != json-subset ]] || source_name=json
  BINARYEN_CORES=1 "$compiler" compile "compiler/test/stdlib/$source_name.test.gr" \
    --release --no-wasm-tail-call -S stdlib -o "$stage/out/$name.wasm"
  if ! cmp -s "$stage/out/$name.wasm" "$here/$name.wasm"; then
    echo "grain-stdlib: $name.wasm differs; review before re-pinning" >&2
    exit 1
  fi
  printf '%s  %s.wasm\n' "$(sha256 "$stage/out/$name.wasm")" "$name"
done
