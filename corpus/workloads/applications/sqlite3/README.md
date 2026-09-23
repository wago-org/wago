# SQLite 3.53.4 WASI CLI

The checked `sqlite3.wasm` is the official SQLite 3.53.4 amalgamation plus
`system_stub.c`, built with WASI SDK 34. SQLite's source is [public domain](https://www.sqlite.org/copyright.html).
The stub only makes the shell's unsupported `.system` subprocess command return
failure; the SQL engine and workload are unmodified. Dynamic load extensions
and threads are disabled.

Download `https://www.sqlite.org/2026/sqlite-amalgamation-3530400.zip` and
verify SHA-256 `1e71ddf93849c6a6ecf58b827c0692073d2dd7ee40196158068f7b29f422e87d`
(upstream SHA3-256 `628a44cfe82c66aed1ccbbe85a562d2e33ebe64b3288981ed76285612227934e`).
Unpack and run `WASI_SDK_PATH=/path/to/wasi-sdk-34.0 bash build.sh /path/to/sqlite-amalgamation-3530400`.
The script checks both source files before building and prints the resulting
artifact SHA-256, which must match `corpus/catalog.json`.
