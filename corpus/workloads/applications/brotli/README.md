# Brotli 1.2.0 WASI CLI

`brotli.wasm` is built from [Brotli v1.2.0](https://github.com/google/brotli/releases/tag/v1.2.0)
at commit `028fb5a23661f123017c060daa546b55cf4bde29` with WASI SDK 34.
Run `WASI_SDK_PATH=/path/to/wasi-sdk-34.0 bash build.sh /path/to/clean/brotli /new/build/dir`
and compare the printed SHA-256 with `corpus/catalog.json`.

WASI lacks `chown()`; `wasi_compat.h` compiles out Brotli's optional output
metadata ownership copy. The corpus uses `-n` and streams, so that path is not
executed. Process-clock emulation satisfies the CLI's timing dependency.
`LICENSE` is copied from upstream.

The compression workload processes a deterministic 2.16 MB, production-shaped
JavaScript fixture at quality 10. The decompression workload retains its smaller
pinned stream and plaintext fixture. `-f` is required because Wago's WASI host
reports piped stdin/stdout as terminal-like; the exact output hashes were
captured independently with Wasmtime.
