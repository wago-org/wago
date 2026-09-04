#!/usr/bin/env sh
# Rebuild qoi.wasm from the pinned upstream revision (see ../README.md).
# qoi.h is compiled byte-for-byte from upstream; wago_qoi.c and the shared
# freestanding shim are Wago's reviewed port. The rebuilt artifact is compared
# against the checked-in digest and is never overwritten in place.
set -eu

here=$(cd "$(dirname "$0")" && pwd)          # tests/corpora/qoi
root=$(cd "$here/../../.." && pwd)           # repository root
shared="$here/../shared"

UPSTREAM_REPO=https://github.com/phoboslab/qoi.git
UPSTREAM_REV=97bacc86a9c4abf5a2d452102dc26546c4c670b9
UPSTREAM_DIR="$root/.tmp/upstream/qoi"

WASI_SDK=${WASI_SDK:-/opt/wasi-sdk}
if [ ! -x "$WASI_SDK/bin/clang" ]; then
	printf 'qoi: wasi-sdk clang not found at %s/bin/clang (set WASI_SDK)\n' "$WASI_SDK" >&2
	exit 1
fi

if [ ! -d "$UPSTREAM_DIR/.git" ]; then
	git clone --filter=blob:none --no-checkout "$UPSTREAM_REPO" "$UPSTREAM_DIR"
fi
git -C "$UPSTREAM_DIR" fetch --depth=1 origin "$UPSTREAM_REV" >/dev/null 2>&1
git -C "$UPSTREAM_DIR" checkout --detach "$UPSTREAM_REV" >/dev/null 2>&1

stage=$(mktemp -d)
trap 'rm -rf "$stage"' EXIT

cp "$UPSTREAM_DIR"/qoi.h "$stage"/
cp "$shared"/wago_freestanding.c "$shared"/wago_freestanding.h "$stage"/
cp "$here"/wago_qoi.c "$stage"/

"$WASI_SDK/bin/clang" --target=wasm32-wasip1 -O2 -nostdlib -ffreestanding -DNDEBUG -I"$stage" \
	"$stage"/wago_freestanding.c "$stage"/wago_qoi.c \
	-Wl,--no-entry \
	-Wl,--export=qoi_encode_run -Wl,--export=qoi_decode_run \
	-Wl,--export=qoi_input_ptr -Wl,--export=qoi_output_ptr \
	-Wl,--export-memory -Wl,--initial-memory=2097152 \
	-o "$stage/qoi.wasm"

got=$(shasum -a 256 "$stage/qoi.wasm" | awk '{print $1}')
want=$(shasum -a 256 "$here/qoi.wasm" | awk '{print $1}')
if [ "$got" != "$want" ] && [ "${UPDATE:-0}" != 1 ]; then
	printf 'qoi: rebuilt artifact differs from the checked-in artifact\n  got  %s\n  want %s\nreview the diff before re-pinning the manifest digest\n' "$got" "$want" >&2
	exit 1
fi
if [ "$got" != "$want" ]; then
	cp "$stage/qoi.wasm" "$here/qoi.wasm"
	printf 'qoi: updated artifact to %s; update MANIFEST.json after review\n' "$got"
	exit 0
fi
printf 'qoi: verified %s\n' "$got"
