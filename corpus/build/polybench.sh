#!/usr/bin/env sh
# Rebuild the complete PolyBench/C 4.2.1 small-dataset corpus as import-free,
# directly callable core modules with deterministic live-out checksums.
set -eu

here=$(cd "$(dirname "$0")" && pwd)
corpus=$(cd "$here/.." && pwd)
root=$(cd "$corpus/.." && pwd)

UPSTREAM_REPO=https://github.com/JamesMenetrey/webassembly-polybench-c.git
UPSTREAM_REV=5474c59fe88f4e36ba968e8f8c4ac913ee83f0d0
UPSTREAM_DIR="$root/.tmp/upstream/polybench-c"
WASI_SDK=${WASI_SDK:-/opt/wasi-sdk}

if [ ! -x "$WASI_SDK/bin/clang" ]; then
	printf 'polybench: wasi-sdk clang not found at %s/bin/clang (set WASI_SDK)\n' "$WASI_SDK" >&2
	exit 1
fi
if [ ! -d "$UPSTREAM_DIR/.git" ]; then
	git clone --filter=blob:none --no-checkout "$UPSTREAM_REPO" "$UPSTREAM_DIR"
fi
git -C "$UPSTREAM_DIR" fetch --depth=1 origin "$UPSTREAM_REV" >/dev/null 2>&1
git -C "$UPSTREAM_DIR" checkout --detach "$UPSTREAM_REV" >/dev/null 2>&1

out="$corpus/workloads/polybench"
mkdir -p "$out"
find "$out" -maxdepth 1 -type f -name '*.wasm' -delete

find "$UPSTREAM_DIR/datamining" "$UPSTREAM_DIR/linear-algebra" \
	"$UPSTREAM_DIR/medley" "$UPSTREAM_DIR/stencils" -type f -name '*.c' | sort |
while IFS= read -r src; do
	name=$(basename "$src" .c)
	printf 'polybench: %s\n' "$name"
	"$WASI_SDK/bin/clang" --target=wasm32-wasip1 -O3 -nostartfiles \
		-DSMALL_DATASET -DPOLYBENCH_NO_FLUSH_CACHE -Wno-unknown-pragmas \
		-I"$UPSTREAM_DIR/utilities" -I"$(dirname "$src")" \
		-DPOLYBENCH_SOURCE=\"$src\" \
		"$corpus/sources/adapters/polybench-checksum.c" \
		-lm \
		-Wl,--no-entry -Wl,--export=polybench_run -Wl,--export-memory \
		-Wl,--strip-all -o "$out/$name.wasm"
	chmod 644 "$out/$name.wasm"
done
