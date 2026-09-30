# Clang 8.0.1 WASI frontend

The artifact is from `binji/wasm-clang` commit
`648c4a89997a351eef75cdaec3ef5b89d4937dec`. The published module has
SHA-256 `2a466f0e990329d3230b869d04fc20803eae96a7feb3a3f6c93e25a77b8aed1d`.
`build.sh` changes only its legacy `wasi_unstable` import namespace to
`wasi_snapshot_preview1`; the result is
`d817b7af8c2cc851527d5872256f27079d6905750a755668683293ec8e36b584`.
The upstream Apache/LLVM license notices are retained here.

The corpus passes C and C++17 source files on stdin to Clang's `-cc1` frontend
and checks generated LLVM IR. The independent Wasmtime oracles normalize only the
amount of padding before LLVM's `; preds =` block comments, which differs
between the two WASI hosts; every other byte remains exact. Wazero 1.9.0
panics while compiling this artifact on macOS/arm64, so its comparison is
skipped. Clang 8's Wasm assembly backend traps in Wago, and file-backed C
source reads only the first byte under current WASI hosts; this fixture does
not claim object generation or full compiler-driver support.
