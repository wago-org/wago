#!/usr/bin/env sh
# Rebuild coremark.wasm from the pinned upstream revision (see ../README.md).
# The four benchmark translation units are compiled byte-for-byte from upstream;
# only core_portme.h/core_portme.c are Wago's reviewed freestanding port. The
# rebuilt artifact is compared against the checked-in digest and is never
# overwritten in place.
set -eu

here=$(cd "$(dirname "$0")" && pwd)          # tests/corpora/coremark
root=$(cd "$here/../../.." && pwd)           # repository root

UPSTREAM_REPO=https://github.com/eembc/coremark.git
UPSTREAM_REV=1f483d5b8316753a742cbf5590caf5bd0a4e4777
UPSTREAM_DIR="$root/.tmp/upstream/coremark"

WASI_SDK=${WASI_SDK:-/opt/wasi-sdk}
if [ ! -x "$WASI_SDK/bin/clang" ]; then
	printf 'coremark: wasi-sdk clang not found at %s/bin/clang (set WASI_SDK)\n' "$WASI_SDK" >&2
	exit 1
fi

# Fetch the exact pinned revision.
if [ ! -d "$UPSTREAM_DIR/.git" ]; then
	git clone --filter=blob:none --no-checkout "$UPSTREAM_REPO" "$UPSTREAM_DIR"
fi
git -C "$UPSTREAM_DIR" fetch --depth=1 origin "$UPSTREAM_REV" >/dev/null 2>&1
git -C "$UPSTREAM_DIR" checkout --detach "$UPSTREAM_REV" >/dev/null 2>&1

stage=$(mktemp -d)
trap 'rm -rf "$stage"' EXIT

cp "$UPSTREAM_DIR"/core_list_join.c \
   "$UPSTREAM_DIR"/core_matrix.c \
   "$UPSTREAM_DIR"/core_state.c \
   "$UPSTREAM_DIR"/core_util.c \
   "$UPSTREAM_DIR"/coremark.h \
   "$stage"/
cp "$here"/core_portme.c "$here"/core_portme.h "$stage"/

"$WASI_SDK/bin/clang" --target=wasm32-unknown-unknown -O2 -nostdlib -I"$stage" \
	"$stage"/core_list_join.c "$stage"/core_matrix.c "$stage"/core_state.c \
	"$stage"/core_util.c "$stage"/core_portme.c \
	-Wl,--no-entry -Wl,--export=coremark_run -Wl,--export-memory \
	-o "$stage/coremark.wasm"

got=$(shasum -a 256 "$stage/coremark.wasm" | awk '{print $1}')
want=$(shasum -a 256 "$here/coremark.wasm" | awk '{print $1}')
if [ "$got" != "$want" ]; then
	printf 'coremark: rebuilt artifact differs from the checked-in artifact\n  got  %s\n  want %s\nreview the diff before re-pinning the manifest digest\n' "$got" "$want" >&2
	exit 1
fi
printf 'coremark: verified %s\n' "$got"
