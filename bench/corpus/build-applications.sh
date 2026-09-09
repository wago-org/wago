#!/usr/bin/env bash
# Rebuild the pinned third-party application corpus into a temporary directory,
# validate it, and compare it with the committed artifacts. Nothing in corpus/
# is overwritten. Requires WASI SDK 34, Binaryen 130, WABT, git, curl, and jq.
set -euo pipefail

here=$(cd "$(dirname "$0")" && pwd)
sdk=${WASI_SDK_PATH:?set WASI_SDK_PATH to wasi-sdk-34.0}
cc="$sdk/bin/clang"
cxx="$sdk/bin/clang++"

for tool in "$cc" "$cxx" git curl jq wasm-as wasm-merge wasm-opt wasm-validate; do
  if ! command -v "$tool" >/dev/null 2>&1; then
    printf 'build-applications: missing %s\n' "$tool" >&2
    exit 1
  fi
done
if ! "$cc" --version | head -1 | grep -q '23.1.0-wasi-sdk'; then
  printf 'build-applications: WASI SDK 34 (clang 23.1.0-wasi-sdk) required\n' >&2
  exit 1
fi
if [ "$(wasm-opt --version)" != "wasm-opt version 130" ]; then
  printf 'build-applications: Binaryen 130 required\n' >&2
  exit 1
fi

tmp=$(mktemp -d "${TMPDIR:-/tmp}/wago-app-corpus.XXXXXX")
trap 'rm -rf "$tmp"' EXIT
src="$tmp/src"
out="$tmp/out"
mkdir -p "$src" "$out/inputs"

checkout() {
  repo=$1
  ref=$2
  dest=$3
  git clone -q --filter=blob:none --no-checkout "$repo" "$dest"
  git -C "$dest" fetch -q --depth 1 origin "$ref"
  git -C "$dest" checkout -q --detach FETCH_HEAD
}

polybench_ref=5474c59fe88f4e36ba968e8f8c4ac913ee83f0d0
sightglass_ref=9ce88522d75b2d155e358f576e7d88ed26d14de8
r3_ref=576f499476060026f6c18cf6edd255ffcbeb5e4f
wabench_ref=ed7ea81ed6f8f3bbe39972a7c38edecfd7d249b8
embench_ref=09c2ed8c3b7008c95d08b038de4a3f6dc103ed70
tacle_ref=c6a0d73e47bbd2bc86e34637156fb26dd4d5cf08

checkout https://github.com/JamesMenetrey/webassembly-polybench-c.git "$polybench_ref" "$src/polybench"
(
  cd "$src/polybench"
  while IFS= read -r file; do
    name=$(basename "$file" .c)
    "$cc" --target=wasm32-wasip1 -O3 -fno-vectorize -fno-slp-vectorize \
      -DSMALL_DATASET -DPOLYBENCH_NO_FLUSH_CACHE -I utilities -I "$(dirname "$file")" \
      utilities/polybench.c "$file" -lm -Wl,-z,stack-size=1048576 \
      -o "$out/polybench-$name.wasm"
  done < utilities/benchmark_list
)

checkout https://github.com/embench/embench-iot.git "$embench_ref" "$src/embench"
(
  cd "$src/embench"
  for dir in src/*; do
    [ -d "$dir" ] || continue
    set -- "$dir"/*.c
    [ -f "$1" ] || continue
    name=$(basename "$dir")
    "$cc" --target=wasm32-wasip1 -O3 -fno-vectorize -fno-slp-vectorize \
      -DCPU_MHZ=1 -DGLOBAL_SCALE_FACTOR=1 -I support -I "$dir" \
      "$here/adapters/embench-main.c" support/beebsc.c "$dir"/*.c -lm \
      -Wl,-z,stack-size=1048576 -o "$out/embench-$name.wasm"
  done
)

checkout https://github.com/bytecodealliance/sightglass.git "$sightglass_ref" "$src/sightglass"
wasm-as "$here/adapters/bench-hooks.wat" -o "$tmp/bench-hooks.wasm"
while read -r name path; do
  wasm-merge "$src/sightglass/benchmarks/$path" workload "$tmp/bench-hooks.wasm" bench \
    --all-features -o "$tmp/$name.merged.wasm"
  wasm-opt "$tmp/$name.merged.wasm" --all-features --remove-unused-module-elements \
    -o "$out/$name.wasm"
done <<'EOF'
sightglass-bz2 bz2/benchmark.wasm
sightglass-shootout-base64 shootout/shootout-base64.wasm
sightglass-blake3-scalar blake3-scalar/benchmark.wasm
sightglass-blake3-simd blake3-simd/benchmark.wasm
sightglass-libsodium-hash libsodium/libsodium-hash.wasm
sightglass-rust-json rust-json/benchmark.wasm
sightglass-regex regex/benchmark.wasm
sightglass-html-rewriter rust-html-rewriter/benchmark.wasm
sightglass-pulldown-cmark pulldown-cmark/benchmark.wasm
sightglass-rust-protobuf rust-protobuf/benchmark.wasm
sightglass-meshoptimizer meshoptimizer/benchmark.wasm
sightglass-sqlite3 sqlite3/sqlite3.wasm
sightglass-spidermonkey-json spidermonkey/spidermonkey-json.wasm
EOF

copy_input() {
  name=$1
  shift
  mkdir -p "$out/inputs/$name"
  cp "$@" "$out/inputs/$name/"
}
sg="$src/sightglass/benchmarks"
copy_input sightglass-bz2 "$sg/bz2/default.input"
copy_input sightglass-shootout-base64 "$sg/shootout"/shootout-base64.*.input
copy_input sightglass-blake3-scalar "$sg/blake3-scalar/default.input"
copy_input sightglass-blake3-simd "$sg/blake3-simd/default.input"
copy_input sightglass-libsodium-hash "$sg/libsodium/libsodium-hash.input"
copy_input sightglass-rust-json "$sg/rust-json/default.input"
copy_input sightglass-regex "$sg/regex/default.input"
copy_input sightglass-html-rewriter "$sg/rust-html-rewriter/default.input"
copy_input sightglass-pulldown-cmark "$sg/pulldown-cmark/default.input.md"
copy_input sightglass-rust-protobuf "$sg/rust-protobuf/default.input"
copy_input sightglass-meshoptimizer "$sg/meshoptimizer/default.input" "$sg/meshoptimizer/indices.input"
copy_input sightglass-sqlite3 "$sg/sqlite3/default.input"
copy_input sightglass-spidermonkey-json "$sg/spidermonkey/spidermonkey-json.input"

for name in bullet parquet boa sandspiel sqlgui pathfinding; do
  curl -fsSL --retry 3 \
    "https://raw.githubusercontent.com/doehyunbaek/wasm-benchmarks/$r3_ref/wasm-r3-bench/$name.wasm" \
    -o "$out/r3-$name.wasm"
done

checkout https://github.com/wabench/wabench.git "$wabench_ref" "$src/wabench"
cp "$here/adapters/wabench-isatty.c" "$src/wabench/Whole_Applications/bzip2/src/wago_isatty.c"
make -s -C "$src/wabench/Whole_Applications/bzip2" \
  "WASMCC=$cc --target=wasm32-wasip1" \
  'OPT=-O3 -D_WASI_EMULATED_SIGNAL -D_WASI_EMULATED_PROCESS_CLOCKS -Disatty=wago_isatty' bzip2.wasm
make -s -C "$src/wabench/Whole_Applications/snappy" \
  "WASMCC=$cxx --target=wasm32-wasip1" OPT=-O3 snappy.wasm
make -s -C "$src/wabench/Whole_Applications/gnuchess" \
  "WASMCC=$cc --target=wasm32-wasip1" 'OPT=-O3 -D_WASI_EMULATED_SIGNAL' gnuchess.wasm
cp "$src/wabench/Whole_Applications/bzip2/bzip2.wasm" "$out/wabench-bzip2.wasm"
cp "$src/wabench/Whole_Applications/snappy/snappy.wasm" "$out/wabench-snappy.wasm"
cp "$src/wabench/Whole_Applications/gnuchess/gnuchess.wasm" "$out/wabench-gnuchess.wasm"
copy_input wabench-bzip2 "$sg/bz2/default.input"
copy_input wabench-gnuchess "$src/wabench/Whole_Applications/gnuchess/input"

checkout https://github.com/tacle/tacle-bench.git "$tacle_ref" "$src/tacle"
for name in bsort binarysearch; do
  "$cc" --target=wasm32-unknown-unknown -O3 -nostdlib -Wno-unknown-pragmas \
    -Dmain=bench_run -Wl,--no-entry -Wl,--export=bench_run \
    "$src/tacle/bench/kernel/$name/$name.c" -o "$out/tacle-$name.wasm"
done

for file in "$out"/*.wasm; do
  wasm-validate "$file"
done

status=0
while IFS= read -r file; do
  if ! cmp -s "$out/$file" "$here/$file"; then
    printf 'build-applications: artifact differs: %s\n' "$file" >&2
    status=1
  fi
done < <(jq -r '.modules[].file' "$here/application-manifest.json")
if ! diff -qr "$out/inputs" "$here/inputs"; then
  status=1
fi
if [ "$status" -ne 0 ]; then
  exit "$status"
fi
printf 'build-applications: 73 artifacts and inputs match\n'
