#!/usr/bin/env sh
# Rebuild lz4.wasm from the pinned upstream revision (see ../README.md).
# lib/lz4.c and lib/lz4.h are compiled byte-for-byte from upstream; wago_lz4.c
# and the shared freestanding shim are Wago's reviewed port/runner. The rebuilt
# artifact is compared against the checked-in digest and is never overwritten
# in place.
set -eu

here=$(cd "$(dirname "$0")" && pwd)          # tests/corpora/lz4
root=$(cd "$here/../../.." && pwd)           # repository root
shared="$here/../shared"

UPSTREAM_REPO=https://github.com/lz4/lz4.git
UPSTREAM_REV=0774d05537f9762f838f7ab541b7765f1a729cb5
UPSTREAM_DIR="$root/.tmp/upstream/lz4"

WASI_SDK=${WASI_SDK:-/opt/wasi-sdk}
if [ ! -x "$WASI_SDK/bin/clang" ]; then
	printf 'lz4: wasi-sdk clang not found at %s/bin/clang (set WASI_SDK)\n' "$WASI_SDK" >&2
	exit 1
fi

if [ ! -d "$UPSTREAM_DIR/.git" ]; then
	git clone --filter=blob:none --no-checkout "$UPSTREAM_REPO" "$UPSTREAM_DIR"
fi
git -C "$UPSTREAM_DIR" fetch --depth=1 origin "$UPSTREAM_REV" >/dev/null 2>&1
git -C "$UPSTREAM_DIR" checkout --detach "$UPSTREAM_REV" >/dev/null 2>&1

stage=$(mktemp -d)
trap 'rm -rf "$stage"' EXIT

cp "$UPSTREAM_DIR"/lib/lz4.c "$UPSTREAM_DIR"/lib/lz4.h "$stage"/
cp "$shared"/wago_freestanding.c "$shared"/wago_freestanding.h "$stage"/
cp "$here"/wago_lz4.c "$stage"/

"$WASI_SDK/bin/clang" --target=wasm32-wasip1 -O2 -nostdlib -ffreestanding -DNDEBUG \
	-DLZ4_USER_MEMORY_FUNCTIONS -I"$stage" \
	"$stage"/lz4.c "$stage"/wago_freestanding.c "$stage"/wago_lz4.c \
	-Wl,--no-entry \
	-Wl,--export=lz4_compress_run -Wl,--export=lz4_decompress_run \
	-Wl,--export=lz4_input_ptr -Wl,--export=lz4_output_ptr \
	-Wl,--export-memory -Wl,--initial-memory=2097152 \
	-o "$stage/lz4.wasm"

got=$(shasum -a 256 "$stage/lz4.wasm" | awk '{print $1}')
want=$(shasum -a 256 "$here/lz4.wasm" | awk '{print $1}')
if [ "$got" != "$want" ]; then
	printf 'lz4: rebuilt artifact differs from the checked-in artifact\n  got  %s\n  want %s\nreview the diff before re-pinning the manifest digest\n' "$got" "$want" >&2
	exit 1
fi
printf 'lz4: verified %s\n' "$got"
