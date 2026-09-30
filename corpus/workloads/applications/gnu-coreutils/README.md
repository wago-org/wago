# GNU coreutils 8.32

These are the Biowasm Emscripten builds of GNU `seq` and `tr`. They are real,
separate coreutils executables rather than aliases of Wago's existing Rust
uutils/coreutils workload.

`build.sh` pins the downloaded artifacts, uses the matching JavaScript glue in
Node/V8 to establish the exact stdout oracle, then performs one mechanical WABT
rewrite. The rewrite moves Emscripten's legacy split-i64 `fd_seek` import out of
the WASI namespace so Wago's typed Preview 1 import remains standards-correct.
It does not change the program code.

The command harness rejects filesystem syscalls. `seq` only writes stdout;
`tr` reads stdin and writes stdout. The `fadvise64_64` hint used by `tr` is a
successful no-op, matching its advisory semantics.

Run `./build.sh` with `curl`, Node, WABT, Perl, and `shasum` available to verify
the pinned artifacts and their V8 oracles.
