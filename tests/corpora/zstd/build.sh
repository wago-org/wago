#!/usr/bin/env sh
# Rebuild zstd.wasm from the pinned upstream revision (see ../README.md).
# The decompress translation units are compiled byte-for-byte from upstream;
# wago_zstd.c and the shared freestanding shim are Wago's reviewed port/runner.
# The rebuilt artifact is compared against the checked-in digest and is never
# overwritten in place.
set -eu

here=$(cd "$(dirname "$0")" && pwd)          # tests/corpora/zstd
root=$(cd "$here/../../.." && pwd)           # repository root
shared="$here/../shared"

UPSTREAM_REPO=https://github.com/facebook/zstd.git
UPSTREAM_REV=82d322c4973d9e2968d94047a40892bc6d9a9bdf
UPSTREAM_DIR="$root/.tmp/upstream/zstd"

WASI_SDK=${WASI_SDK:-/opt/wasi-sdk}
if [ ! -x "$WASI_SDK/bin/clang" ]; then
	printf 'zstd: wasi-sdk clang not found at %s/bin/clang (set WASI_SDK)\n' "$WASI_SDK" >&2
	exit 1
fi

if [ ! -d "$UPSTREAM_DIR/.git" ]; then
	git clone --filter=blob:none --no-checkout "$UPSTREAM_REPO" "$UPSTREAM_DIR"
fi
git -C "$UPSTREAM_DIR" fetch --depth=1 origin "$UPSTREAM_REV" >/dev/null 2>&1
git -C "$UPSTREAM_DIR" checkout --detach "$UPSTREAM_REV" >/dev/null 2>&1

stage=$(mktemp -d)
trap 'rm -rf "$stage"' EXIT

mkdir -p "$stage/common" "$stage/decompress"
cp "$UPSTREAM_DIR"/lib/zstd.h "$UPSTREAM_DIR"/lib/zstd_errors.h "$stage"/
cp "$UPSTREAM_DIR"/lib/common/*.h "$stage/common"/
cp "$UPSTREAM_DIR"/lib/common/entropy_common.c \
   "$UPSTREAM_DIR"/lib/common/error_private.c \
   "$UPSTREAM_DIR"/lib/common/fse_decompress.c \
   "$UPSTREAM_DIR"/lib/common/zstd_common.c \
   "$UPSTREAM_DIR"/lib/common/xxhash.c \
   "$UPSTREAM_DIR"/lib/common/debug.c \
   "$stage/common"/
cp "$UPSTREAM_DIR"/lib/decompress/*.c "$UPSTREAM_DIR"/lib/decompress/*.h "$stage/decompress"/
cp "$shared"/wago_freestanding.c "$shared"/wago_freestanding.h "$stage"/
cp "$here"/wago_zstd.c "$stage"/

"$WASI_SDK/bin/clang" --target=wasm32-wasip1 -O2 -nostdlib \
	-I"$stage" -I"$stage/common" -I"$stage/decompress" \
	"$stage"/common/entropy_common.c "$stage"/common/error_private.c \
	"$stage"/common/fse_decompress.c "$stage"/common/zstd_common.c \
	"$stage"/common/xxhash.c "$stage"/common/debug.c \
	"$stage"/decompress/huf_decompress.c "$stage"/decompress/zstd_ddict.c \
	"$stage"/decompress/zstd_decompress.c "$stage"/decompress/zstd_decompress_block.c \
	"$stage"/wago_freestanding.c "$stage"/wago_zstd.c \
	-Wl,--no-entry \
	-Wl,--export=zstd_decompress_run -Wl,--export=zstd_input_ptr -Wl,--export=zstd_output_ptr \
	-Wl,--export-memory -Wl,--initial-memory=2097152 \
	-o "$stage/zstd.wasm"

got=$(shasum -a 256 "$stage/zstd.wasm" | awk '{print $1}')
want=$(shasum -a 256 "$here/zstd.wasm" | awk '{print $1}')
if [ "$got" != "$want" ]; then
	printf 'zstd: rebuilt artifact differs from the checked-in artifact\n  got  %s\n  want %s\nreview the diff before re-pinning the manifest digest\n' "$got" "$want" >&2
	exit 1
fi
printf 'zstd: verified %s\n' "$got"
