#!/usr/bin/env sh
set -eu
here=$(cd "$(dirname "$0")" && pwd)
root=$(cd "$here/../../.." && pwd)
sdk=${WASI_SDK:-/opt/wasi-sdk}
upstream="$root/.tmp/upstream/nanosvg"
rev=239e102ec2c691f2902e20ace2ed36ee4a35cfe6
if [ ! -d "$upstream/.git" ]; then git clone --filter=blob:none --no-checkout https://github.com/memononen/nanosvg.git "$upstream"; fi
git -C "$upstream" fetch --depth=1 origin "$rev"
git -C "$upstream" checkout --detach "$rev"
"$sdk/bin/clang" --target=wasm32-wasip1 -O2 -DNDEBUG -nostartfiles -ffunction-sections -fdata-sections -I"$upstream/src" \
    ${REDUCED:+-DREDUCED} "$here/raster.c" -lm -Wl,--no-entry -Wl,--export=nanosvg_run -Wl,--export-memory -Wl,--strip-debug -o "$here/${OUTPUT:-raster.wasm}"
sha256sum "$here/${OUTPUT:-raster.wasm}"
