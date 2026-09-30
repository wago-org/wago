#!/bin/sh
set -eu

# The published standalone compiler uses the old wasi_unstable import name.
clang_build_dir=$(mktemp -d)
trap 'rm -rf "$clang_build_dir"' EXIT
clang_output=${1:-"$(dirname "$0")/clang.wasm"}

curl --fail --location --silent --show-error \
  https://raw.githubusercontent.com/binji/wasm-clang/648c4a89997a351eef75cdaec3ef5b89d4937dec/clang \
  --output "$clang_build_dir/clang-original.wasm"
printf '%s  %s\n' \
  2a466f0e990329d3230b869d04fc20803eae96a7feb3a3f6c93e25a77b8aed1d \
  "$clang_build_dir/clang-original.wasm" | shasum -a 256 -c -

wasm2wat "$clang_build_dir/clang-original.wasm" |
  sed 's/"wasi_unstable"/"wasi_snapshot_preview1"/g' |
  wat2wasm -o "$clang_output" -
printf '%s  %s\n' \
  d817b7af8c2cc851527d5872256f27079d6905750a755668683293ec8e36b584 \
  "$clang_output" | shasum -a 256 -c -
