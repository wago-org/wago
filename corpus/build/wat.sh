#!/usr/bin/env sh
# Rebuild the synthetic and hand-written compute workloads from their WAT. The .wasm files are
# checked in so the benchmark suite is stable and needs no toolchain at run time;
# rerun this only when changing the corpus.
set -eu

here=$(cd "$(dirname "$0")" && pwd)
corpus=$(cd "$here/.." && pwd)

if ! command -v wat2wasm >/dev/null 2>&1; then
	printf 'corpus: wat2wasm (wabt) not on PATH\n' >&2
	exit 1
fi

printf 'corpus: compiling .wat -> .wasm...\n'
for wat in "$corpus"/sources/wat/*.wat; do
	name=$(basename "$wat" .wat)
	out="$corpus/workloads/synthetic/$name.wasm"
	[ "$name" = linked_list ] && out="$corpus/workloads/compute/$name.wasm"
	wat2wasm "$wat" -o "$out"
	printf '  %s.wasm\n' "$name"
done

printf 'corpus: done\n'
