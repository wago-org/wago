#!/usr/bin/env sh
# Build the real Rust/WASI application corpus and copy the .wasm into corpus/.
# Needs: rustup target add wasm32-wasip1. Re-run to refresh the committed binaries.
set -eu
here=$(cd "$(dirname "$0")" && pwd)
cd "$here"
cargo build --release --target wasm32-wasip1
out="target/wasm32-wasip1/release"
for b in markdown crcsum blake3sum base64x jsonproc script; do
	cp "$out/$b.wasm" "$here/../$b.wasm"
	printf 'corpus: %s.wasm (%s bytes)\n' "$b" "$(wc -c < "$here/../$b.wasm")"
done
