# jq 1.8.2 WASI CLI

`jq.wasm` is built from the [jq 1.8.2 release](https://github.com/jqlang/jq/releases/tag/jq-1.8.2)
with WASI SDK 34 and its bundled Oniguruma regex engine. The release archive
`jq-1.8.2.tar.gz` has SHA-256
`71b8d6e8f5fe81f6c6d0d110e3892251f6ce76ed095abd315e26e6e1193af3af`.
Unpack it and run `WASI_SDK_PATH=/path/to/wasi-sdk-34.0 bash build.sh /path/to/jq-1.8.2 /new/build/dir`.
The script verifies source bytes and prints the artifact digest to compare with
`corpus/catalog.json`. `COPYING` is jq's MIT licence; `ONIGURUMA-COPYING`
covers the bundled regex engine.

The corpus workload generates a thousand JSON objects, groups them, and
aggregates each group. `-M` forces monochrome output because WASI hosts differ
in whether stdout is reported as a terminal. Wasmtime supplied the independent
exact-output oracle.
