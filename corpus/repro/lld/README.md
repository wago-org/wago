# LLD legacy WASI artifact

The `lld` artifact at `binji/wasm-clang` commit
`648c4a89997a351eef75cdaec3ef5b89d4937dec` has SHA-256
`36419ed202011765222098d7701218378b67f634d50f0a4625059ae2c9860f48`.
Its `wasi_unstable` import namespace can be changed to Preview 1 with WABT,
producing SHA-256
`69ea6e281b67d91d10d2f711a50226247f48bfcd1eb1c255ba5e9be8c394e35b`.
That module compiles in Wago and prints its version in Wasmtime. However,
linking a valid Wasm object from a preopened file reports `unknown file type`
under Wasmtime. The companion Clang module also misreads file-backed source;
the apparent host/ABI mismatch needs investigation before LLD can be admitted.
No compile-only or version-only workload is counted as running.
