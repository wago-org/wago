#!/usr/bin/env sh
# Regenerate the benchmark corpus: emit the synthetic .wat sources, then compile
# every src/*.wat to a committed .wasm next to this script. The .wasm files are
# checked in so the benchmark suite is stable and needs no toolchain at run time;
# rerun this only when changing the corpus.
set -eu

here=$(cd "$(dirname "$0")" && pwd) # bench/corpus
cd "$here"

if ! command -v wat2wasm >/dev/null 2>&1; then
	printf 'corpus: wat2wasm (wabt) not on PATH\n' >&2
	exit 1
fi

printf 'corpus: generating synthetic sources...\n'
(cd "$here/.." && go run ./corpus/gen -out corpus/src)

printf 'corpus: compiling .wat -> .wasm...\n'
for wat in src/*.wat; do
	name=$(basename "$wat" .wat)
	wat2wasm "$wat" -o "$name.wasm"
	printf '  %s.wasm\n' "$name"
done
printf 'corpus: done\n'
