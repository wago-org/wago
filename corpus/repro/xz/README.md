# XZ CLI admission blocker

Run `bash fetch.sh` to download the a-Shell `xz.wasm` release 0.1 asset (ID
208109158, updated 2024-11-21). The script checks its SHA-256 before saving
it. The binary reports XZ Utils 5.3.1alpha and has SHA-256
`b3c7dc3ec1fe4e4370796c2f6e341d1da0edb99582ab15ce3229ce3575ef81df`.
The pinned compressed input has SHA-256
`64f999ed9f0448036cd2d4842f87f5cee4edf3800e50ba085c4bb76ae711bcf1`;
decompression must produce SHA-256
`15f4817d6bb6e105ef12c8b6d2383c32d1646a16824365d96935445f436afc89`.

`wasmtime run xz.wasm -d -c < inputs/source.js.xz` produces the expected
output. Wago's WASI adapter reports stdin as a character device, so XZ rejects
compressed stdin with `Compressed data cannot be read from a terminal`.
Wazero accepts the same stdin workload.

Passing a file path instead does not work with this particular prebuilt binary.
Wasmtime rejects its `path_open` call with `convert Rights: Int conversion
error`; Wago and wazero report a bad descriptor. That appears to be an a-Shell
host-contract difference, not a Wago compilation failure. Do not add this
artifact to the passing corpus until a compatible host adapter or standalone
rebuild passes Wago and an independent reference runtime with an exact oracle.

The upstream XZ Utils 5.3.1alpha
[`COPYING`](https://github.com/tukaani-project/xz/blob/v5.3.1alpha/COPYING)
says xz/liblzma are public domain, with LGPL-2.1+ conditional on bundled GNU
getopt. Verify the binary's exact linked licensing before admission; the
prebuilt binary is intentionally not redistributed in this repository.
