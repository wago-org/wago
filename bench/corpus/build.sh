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

# Real C compute kernels (geometry / mandelbrot / hashing). Compiled freestanding
# to wasm32 — no libc, no malloc, static memory capped to one page — so they stay
# inside the opcode/memory limits wago can fully compile and execute. The .wasm
# are committed; this step only matters when regenerating. Best-effort: skip if
# the wasm toolchain is unavailable.
compile_c() {
	command -v clang >/dev/null 2>&1 || { printf 'corpus: clang absent; keeping committed C kernels\n'; return; }
	# clang invokes wasm-ld-<ver>; find any wasm-ld and expose it under that name.
	wld=$(command -v wasm-ld-18 || command -v wasm-ld || true)
	if [ -z "$wld" ]; then
		for d in "$HOME"/.rustup/toolchains/*/lib/rustlib/*/bin/gcc-ld; do
			[ -x "$d/wasm-ld" ] && wld="$d/wasm-ld" && break
		done
	fi
	[ -n "$wld" ] || { printf 'corpus: no wasm-ld found; keeping committed C kernels\n'; return; }
	shim=$(mktemp -d)
	ln -sf "$wld" "$shim/wasm-ld-18"
	for c in csrc/*.c; do
		name=$(basename "$c" .c)
		PATH="$shim:$PATH" clang --target=wasm32 -O2 -nostdlib \
			-Wl,--no-entry -Wl,--export-all -Wl,-z,stack-size=16384 \
			-o "$name.wasm" "$c"
		printf '  %s.wasm (C)\n' "$name"
	done
	rm -rf "$shim"
}
compile_c
printf 'corpus: done\n'
