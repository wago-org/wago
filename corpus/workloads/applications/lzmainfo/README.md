# XZ Utils 5.8.4 lzmainfo WASI CLI

This `lzmainfo.wasm` is built from [XZ Utils 5.8.4](https://github.com/tukaani-project/xz/releases/tag/v5.8.4)
with WASI SDK 34. The release archive `xz-5.8.4.tar.xz` has SHA-256
`4ce24038fd4221e0d13bc1a2de7a4db56e90b92b3bf75321f6c14be73f65de4b`.
Unpack it and run `WASI_SDK_PATH=/path/to/wasi-sdk-34.0 bash build.sh /path/to/xz-5.8.4 /new/build/dir`.
The script verifies source bytes and prints the artifact digest to compare with
`corpus/catalog.json`.

The CLI parses the same pinned `.lzma` stream as `lzmadec`; Wasmtime supplied
the independent exact-output oracle. The CLI and liblzma are 0BSD; see
`COPYING` and `COPYING.0BSD` from the release. This is an inspection command,
not a full decompression benchmark.
