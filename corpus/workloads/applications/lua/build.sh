#!/bin/sh
set -eu

# The npm release imports the retired wasi_unstable module name. The Preview 1
# ABI is otherwise compatible, so rebuild only that import namespace with WABT.
lua_build_dir=$(mktemp -d)
trap 'rm -rf "$lua_build_dir"' EXIT
lua_output=${1:-"$(dirname "$0")/lua.wasm"}

curl --fail --location --silent --show-error \
  https://registry.npmjs.org/@antonz/lua-wasi/-/lua-wasi-5.4.6.tgz \
  --output "$lua_build_dir/lua-wasi.tgz"
printf '%s  %s\n' \
  2b69ee6e70c4e1ed4c7b30488a7464e11303b2f54941dc54bd5d02ff1ce54a49 \
  "$lua_build_dir/lua-wasi.tgz" | shasum -a 256 -c -
tar -xzf "$lua_build_dir/lua-wasi.tgz" -C "$lua_build_dir"
printf '%s  %s\n' \
  02754c9822caf5112e9a2ccaec3dd29076bf37b51d65cb886ae1606389370c84 \
  "$lua_build_dir/package/dist/lua.wasm" | shasum -a 256 -c -

wasm2wat "$lua_build_dir/package/dist/lua.wasm" |
  sed 's/"wasi_unstable"/"wasi_snapshot_preview1"/g' |
  wat2wasm -o "$lua_output" -
printf '%s  %s\n' \
  51e8072539c5ba5f4e97e7e776ef852f9879cb5b97b4e5d0f9f1e9c3956ffbe2 \
  "$lua_output" | shasum -a 256 -c -
