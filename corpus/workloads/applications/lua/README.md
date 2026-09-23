# Lua 5.4.6 CLI

`lua.wasm` is the standalone Lua interpreter from the `@antonz/lua-wasi`
5.4.6 npm tarball (SHA-256
`2b69ee6e70c4e1ed4c7b30488a7464e11303b2f54941dc54bd5d02ff1ce54a49`).
The upstream artifact SHA-256 is
`02754c9822caf5112e9a2ccaec3dd29076bf37b51d65cb886ae1606389370c84`.
It imports the older `wasi_unstable` module name. `build.sh` reproducibly
changes only the import namespace to `wasi_snapshot_preview1`; the resulting
artifact SHA-256 is
`51e8072539c5ba5f4e97e7e776ef852f9879cb5b97b4e5d0f9f1e9c3956ffbe2`.

The corpus executes a Lua script that generates one million deterministic
events, aggregates them into 257 table buckets, ranks the buckets, and computes
an exact checksum. The expected output was captured independently with
Wasmtime. The port and Lua are MIT licensed; their notices are retained here.
