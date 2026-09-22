# tree 2.2.1 WASI CLI

`tree.wasm` is built from [tree 2.2.1](https://github.com/Old-Man-Programmer/tree/tree/d501b58ff9cbfd64272c8cbcad0bda36a3fada06)
at commit `d501b58ff9cbfd64272c8cbcad0bda36a3fada06` with WASI SDK 34.
The complete source is available at that pinned revision under GPL-2.0-or-later;
the upstream `LICENSE` is included here. Run
`WASI_SDK_PATH=/path/to/wasi-sdk-34.0 bash build.sh /path/to/clean/tree-checkout`
and compare the printed SHA-256 with `corpus/catalog.json`.

WASI has no `pwd.h` or `grp.h`, so the two compatibility headers use tree's
existing numeric UID/GID fallback. This workload does not request owner names.
The preopened fixture contains nested source and test directories, and its
recursive listing was independently captured with Wasmtime.
