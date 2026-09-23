# uutils/coreutils

`coreutils.wasm` is the upstream `wasm32-wasip1` multicall release at version
0.10.0. The release archive SHA-256 is
`ee86ae5bda92f7db76ece3c9ddbdfa02528eb026195a2ed47e39d2601bb02c0b`;
the module SHA-256 is
`393da2c407ef0498be397f48d8b030cd4106bd30e0ddec504bfc75718486d034`.
The MIT license text is retained here.

The harness passes `coreutils` as `argv[0]` and the subcommand as `argv[1]`.
The admitted `sort`, `sha256sum`, `base64`, and `wc` commands each process the
same pinned record file and have byte-exact Wasmtime reference output.
