# CPython 3.12 Wago trap

The `python-3.12.0.wasm` release asset from
`vmware-labs/webassembly-language-runtimes`, tag
`python/3.12.0+20231211-040d5a6`, has SHA-256
`e5dc5a398b07b54ea8fdb503bf68fb583d533f10ec3f930963e02b9505f7a763`.
It compiles in Wago. Run it with argument `-` and feed `inputs/program.py` on
stdin. Wasmtime and wazero print the same seventeen-bucket result (stdout
SHA-256 `a9cfd18aeeb61f448d9314c2687d0bf22b1a287a4a3b165a5aa676ff4b0de869`).
Wago instead traps on a linear-memory out-of-bounds access at function 1669,
without stdout or stderr. The artifact is not admitted to `catalog.json`.
