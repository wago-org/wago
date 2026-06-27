#!/usr/bin/env sh
# Fetch large third-party wasm binaries into corpus/vendor/ (gitignored). The
# manifest references them via 'path' and the suite skips any that are absent, so
# this is optional — run it to populate the real-large tier. Re-run to refresh.
set -eu

here=$(cd "$(dirname "$0")" && pwd)
dest="$here/vendor"
mkdir -p "$dest"

fetch() { # url filename
	printf 'vendor: %s\n' "$2"
	curl -fsSL -o "$dest/$2" "$1"
}

# wasm3 interpreter compiled to WASI — validates on wago but the backend can't
# compile it yet (WASI host imports), so it lands in the decode/validate tier.
fetch "https://github.com/wasm3/wasm3/releases/download/v0.5.0/wasm3-wasi.wasm" "wasm3-wasi.wasm"

# clang compiled to wasm has no canonical public download (the known builds
# compile it from LLVM source). Provide one yourself: drop it at
# corpus/vendor/clang.wasm, or set CLANG_WASM_URL to fetch it here.
if [ -n "${CLANG_WASM_URL:-}" ]; then
	fetch "$CLANG_WASM_URL" "clang.wasm"
fi

printf 'vendor: done\n'
