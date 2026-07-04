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
# (A ~180 KiB copy is also committed as corpus/wasm3.wasm; this refreshes it.)
fetch "https://github.com/wasm3/wasm3/releases/download/v0.5.0/wasm3-wasi.wasm" "wasm3-wasi.wasm"

# The multi-megabyte real-large tier: genuinely large real-world programs the
# manifest references via 'path' (corpus/vendor/*). Too big to commit, so they
# only enter the suite once fetched. Both are core-1.0 wasm that decode+validate
# on wago but carry WASI/host imports the backend can't compile yet.
#
# Ruby 3.3 interpreter (~16 MiB): 17k functions, ~11 MiB of code — the largest
# validate workload in the corpus.
fetch "https://cdn.jsdelivr.net/npm/@ruby/3.3-wasm-wasi@2.7.1/dist/ruby.wasm" "ruby.wasm"

# esbuild bundler (~12 MiB): the Go toolchain compiled to wasm — a very different
# code shape (Go's runtime + GC) from the LLVM-emitted binaries above.
fetch "https://cdn.jsdelivr.net/npm/esbuild-wasm@0.21.5/esbuild.wasm" "esbuild.wasm"

# clang compiled to wasm has no canonical public download (the known builds
# compile it from LLVM source). Provide one yourself: drop it at
# corpus/vendor/clang.wasm, or set CLANG_WASM_URL to fetch it here.
if [ -n "${CLANG_WASM_URL:-}" ]; then
	fetch "$CLANG_WASM_URL" "clang.wasm"
fi

printf 'vendor: done\n'
