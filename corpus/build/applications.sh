#!/usr/bin/env bash
# Rebuild the retained application corpus in a temporary tree and compare it
# with the committed artifacts. Requires WASI SDK 34, Binaryen 130, and WABT.
set -euo pipefail

here=$(cd "$(dirname "$0")" && pwd)
corpus=$(cd "$here/.." && pwd)
sdk=${WASI_SDK_PATH:?set WASI_SDK_PATH to wasi-sdk-34.0}
cc="$sdk/bin/clang"

for tool in "$cc" git wasm-as wasm-merge wasm-opt wasm-validate; do
	command -v "$tool" >/dev/null 2>&1 || { printf 'build-applications: missing %s\n' "$tool" >&2; exit 1; }
done
if ! "$cc" --version | head -1 | grep -q '23.1.0-wasi-sdk'; then
	printf 'build-applications: WASI SDK 34 required\n' >&2
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
mkdir -p "$src" "$out/embench" "$out/sightglass/shootout-base64/inputs" \
	"$out/sightglass/libsodium-hash/inputs" "$out/tacle"

checkout() {
	repo=$1 ref=$2 dest=$3
	git clone -q --filter=blob:none --no-checkout "$repo" "$dest"
	git -C "$dest" fetch -q --depth 1 origin "$ref"
	git -C "$dest" checkout -q --detach FETCH_HEAD
}

checkout https://github.com/embench/embench-iot.git 09c2ed8c3b7008c95d08b038de4a3f6dc103ed70 "$src/embench"
for name in crc32 huffbench matmult-int nettle-aes nettle-sha256 qrduino; do
	"$cc" --target=wasm32-wasip1 -O3 -fno-vectorize -fno-slp-vectorize \
		-DCPU_MHZ=1 -DGLOBAL_SCALE_FACTOR=1 -I "$src/embench/support" -I "$src/embench/src/$name" \
		"$corpus/sources/adapters/embench-main.c" "$src/embench/support/beebsc.c" \
		"$src/embench/src/$name"/*.c -lm -Wl,-z,stack-size=1048576 \
		-o "$out/embench/embench-$name.wasm"
done

checkout https://github.com/bytecodealliance/sightglass.git 9ce88522d75b2d155e358f576e7d88ed26d14de8 "$src/sightglass"
wasm-as "$corpus/sources/adapters/bench-hooks.wat" -o "$tmp/bench-hooks.wasm"
convert_sightglass() {
	name=$1 source=$2
	wasm-merge "$source" workload "$tmp/bench-hooks.wasm" bench --all-features -o "$tmp/$name.merged.wasm"
	wasm-opt "$tmp/$name.merged.wasm" --all-features --remove-unused-module-elements -o "$out/sightglass/$name/module.wasm"
}
convert_sightglass shootout-base64 "$src/sightglass/benchmarks/shootout/shootout-base64.wasm"
convert_sightglass libsodium-hash "$src/sightglass/benchmarks/libsodium/libsodium-hash.wasm"
cp "$src/sightglass/benchmarks/shootout/shootout-base64.iterations.input" "$out/sightglass/shootout-base64/inputs/iterations"
cp "$src/sightglass/benchmarks/libsodium/libsodium-hash.input" "$out/sightglass/libsodium-hash/inputs/message"

checkout https://github.com/tacle/tacle-bench.git c6a0d73e47bbd2bc86e34637156fb26dd4d5cf08 "$src/tacle"
"$cc" --target=wasm32-unknown-unknown -O3 -nostdlib -Wno-unknown-pragmas \
	-Dmain=bench_run -Wl,--no-entry -Wl,--export=bench_run \
	"$src/tacle/bench/kernel/bsort/bsort.c" -o "$out/tacle/bsort.wasm"

find "$out" -name '*.wasm' -exec wasm-validate {} \;
diff -qr "$out" "$corpus/workloads/applications"
printf 'build-applications: retained artifacts and inputs match\n'
