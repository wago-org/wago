#!/usr/bin/env sh
# Rebuild zlib.wasm from the pinned upstream revision (see ../README.md).
# The inflate translation units are compiled byte-for-byte from upstream;
# wago_zlib.c and the shared freestanding shim are Wago's reviewed port/runner.
# The rebuilt artifact is compared against the checked-in digest and is never
# overwritten in place.
set -eu

here=$(cd "$(dirname "$0")" && pwd)          # tests/corpora/zlib
root=$(cd "$here/../../.." && pwd)           # repository root
shared="$here/../shared"

UPSTREAM_REPO=https://github.com/madler/zlib.git
UPSTREAM_REV=e3dc0a85b7032e98380dec011bc8f2c2ee0d8fca
UPSTREAM_DIR="$root/.tmp/upstream/zlib"

WASI_SDK=${WASI_SDK:-/opt/wasi-sdk}
if [ ! -x "$WASI_SDK/bin/clang" ]; then
	printf 'zlib: wasi-sdk clang not found at %s/bin/clang (set WASI_SDK)\n' "$WASI_SDK" >&2
	exit 1
fi

if [ ! -d "$UPSTREAM_DIR/.git" ]; then
	git clone --filter=blob:none --no-checkout "$UPSTREAM_REPO" "$UPSTREAM_DIR"
fi
git -C "$UPSTREAM_DIR" fetch --depth=1 origin "$UPSTREAM_REV" >/dev/null 2>&1
git -C "$UPSTREAM_DIR" checkout --detach "$UPSTREAM_REV" >/dev/null 2>&1

stage=$(mktemp -d)
trap 'rm -rf "$stage"' EXIT

cp "$UPSTREAM_DIR"/adler32.c "$UPSTREAM_DIR"/crc32.c "$UPSTREAM_DIR"/crc32.h \
   "$UPSTREAM_DIR"/inffast.c "$UPSTREAM_DIR"/inffast.h "$UPSTREAM_DIR"/inffixed.h \
   "$UPSTREAM_DIR"/inflate.c "$UPSTREAM_DIR"/inflate.h \
   "$UPSTREAM_DIR"/inftrees.c "$UPSTREAM_DIR"/inftrees.h \
   "$UPSTREAM_DIR"/zutil.c "$UPSTREAM_DIR"/zutil.h \
   "$UPSTREAM_DIR"/zlib.h "$UPSTREAM_DIR"/zconf.h "$UPSTREAM_DIR"/gzguts.h \
   "$stage"/
cp "$shared"/wago_freestanding.c "$shared"/wago_freestanding.h "$stage"/
cp "$here"/wago_zlib.c "$stage"/

"$WASI_SDK/bin/clang" --target=wasm32-wasip1 -O2 -nostdlib -DNO_GZIP -I"$stage" \
	"$stage"/adler32.c "$stage"/crc32.c "$stage"/inffast.c "$stage"/inflate.c \
	"$stage"/inftrees.c "$stage"/zutil.c \
	"$stage"/wago_freestanding.c "$stage"/wago_zlib.c \
	-Wl,--no-entry \
	-Wl,--export=zlib_inflate_run -Wl,--export=zlib_input_ptr -Wl,--export=zlib_output_ptr \
	-Wl,--export-memory -Wl,--initial-memory=2097152 \
	-o "$stage/zlib.wasm"

got=$(shasum -a 256 "$stage/zlib.wasm" | awk '{print $1}')
want=$(shasum -a 256 "$here/zlib.wasm" | awk '{print $1}')
if [ "$got" != "$want" ]; then
	printf 'zlib: rebuilt artifact differs from the checked-in artifact\n  got  %s\n  want %s\nreview the diff before re-pinning the manifest digest\n' "$got" "$want" >&2
	exit 1
fi
printf 'zlib: verified %s\n' "$got"
