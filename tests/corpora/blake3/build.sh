#!/usr/bin/env sh
# Rebuild blake3.wasm from the pinned upstream revision (see ../README.md).
# The portable BLAKE3 core (blake3.c, blake3_dispatch.c, blake3_portable.c,
# blake3_impl.h) is compiled byte-for-byte from upstream; only wago_blake3.c is
# Wago's reviewed freestanding port/runner. The rebuilt artifact is compared
# against the checked-in digest and is never overwritten in place.
set -eu

here=$(cd "$(dirname "$0")" && pwd)          # tests/corpora/blake3
root=$(cd "$here/../../.." && pwd)           # repository root

UPSTREAM_REPO=https://github.com/BLAKE3-team/BLAKE3.git
UPSTREAM_REV=77b257eee7da5cd608eaf6be8343d3a4c9776af2
UPSTREAM_DIR="$root/.tmp/upstream/blake3"

WASI_SDK=${WASI_SDK:-/opt/wasi-sdk}
if [ ! -x "$WASI_SDK/bin/clang" ]; then
	printf 'blake3: wasi-sdk clang not found at %s/bin/clang (set WASI_SDK)\n' "$WASI_SDK" >&2
	exit 1
fi

if [ ! -d "$UPSTREAM_DIR/.git" ]; then
	git clone --filter=blob:none --no-checkout "$UPSTREAM_REPO" "$UPSTREAM_DIR"
fi
git -C "$UPSTREAM_DIR" fetch --depth=1 origin "$UPSTREAM_REV" >/dev/null 2>&1
git -C "$UPSTREAM_DIR" checkout --detach "$UPSTREAM_REV" >/dev/null 2>&1

stage=$(mktemp -d)
trap 'rm -rf "$stage"' EXIT

cp "$UPSTREAM_DIR"/c/blake3.c \
   "$UPSTREAM_DIR"/c/blake3.h \
   "$UPSTREAM_DIR"/c/blake3_impl.h \
   "$UPSTREAM_DIR"/c/blake3_dispatch.c \
   "$UPSTREAM_DIR"/c/blake3_portable.c \
   "$stage"/
cp "$here"/wago_blake3.c "$stage"/

"$WASI_SDK/bin/clang" --target=wasm32-wasip1 -O2 -nostdlib -ffreestanding -DNDEBUG -I"$stage" \
	"$stage"/blake3.c "$stage"/blake3_dispatch.c "$stage"/blake3_portable.c \
	"$stage"/wago_blake3.c \
	-Wl,--no-entry \
	-Wl,--export=blake3_hash -Wl,--export=blake3_keyed_hash -Wl,--export=blake3_derive_key \
	-Wl,--export=blake3_input_ptr -Wl,--export=blake3_output_ptr \
	-Wl,--export-memory -Wl,--initial-memory=1048576 \
	-o "$stage/blake3.wasm"

got=$(shasum -a 256 "$stage/blake3.wasm" | awk '{print $1}')
want=$(shasum -a 256 "$here/blake3.wasm" | awk '{print $1}')
if [ "$got" != "$want" ]; then
	printf 'blake3: rebuilt artifact differs from the checked-in artifact\n  got  %s\n  want %s\nreview the diff before re-pinning the manifest digest\n' "$got" "$want" >&2
	exit 1
fi
printf 'blake3: verified %s\n' "$got"
