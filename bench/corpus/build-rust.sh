#!/usr/bin/env sh
# Rebuild the Rust compute-kernel corpus modules from corpus/rust/*.rs and copy
# the resulting .wasm into corpus/. These are real algorithms — the Computer
# Language Benchmarks Game classics (nbody, fannkuch, spectralnorm) plus dense
# matmul, quicksort, crc32, sha256, and a recursive raytracer — compiled to
# core-1.0 wasm the backend runs end-to-end.
#
# Each source is a self-contained #![no_std] cdylib: no imports, no heap (fixed
# stack/static arrays), a single exported entry point taking an i32 count and
# returning an i32 DCE sink, deterministic across repeated calls. panic=abort +
# fat LTO strip the formatting machinery so the .wasm stays small.
#
# The .wasm are committed so the benchmark suite needs no toolchain at run time;
# rerun this only when changing a kernel. Needs rustc with the
# wasm32-unknown-unknown target (rustup target add wasm32-unknown-unknown).
set -eu

here=$(cd "$(dirname "$0")" && pwd) # bench/corpus
cd "$here"

if ! command -v rustc >/dev/null 2>&1; then
	printf 'build-rust: rustc not on PATH\n' >&2
	exit 1
fi

target=wasm32-unknown-unknown
if ! rustc --print target-list 2>/dev/null | grep -qx "$target"; then
	printf 'build-rust: %s target unknown to rustc\n' "$target" >&2
	exit 1
fi

optimize() { # in.wasm — shrink in place with wasm-opt if available
	if command -v wasm-opt >/dev/null 2>&1; then
		wasm-opt -O3 "$1" -o "$1.opt" && mv "$1.opt" "$1"
	fi
}

for src in rust/*.rs; do
	name=$(basename "$src" .rs)
	printf 'build-rust: %s -> %s.wasm\n' "$src" "$name"
	rustc --target "$target" \
		-C opt-level=3 -C lto=fat -C panic=abort -C codegen-units=1 \
		--crate-type cdylib -o "$name.wasm" "$src"
	optimize "$name.wasm"
done

printf 'build-rust: done\n'
