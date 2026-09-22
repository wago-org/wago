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

`ashell-tree.wasm` is the separate [a-Shell release 0.1](https://github.com/holzschu/a-Shell-commands/releases/tag/0.1)
asset ID `20591324`, updated 2020-05-10, with SHA-256
`72ac9f09f35757a5714611f1922c325a8c33e8230e1a37ab7fc4104dbfbd2a64`.
The binary identifies itself as tree 1.8.0; its original source is available
from the upstream tree 1.8.0 distribution, and the included `LICENSE` covers
both tree versions. The corpus adapter supplies a virtual `/` current directory,
an empty environment, and unsupported responses for shell execution and
directory changes. This workload passes `/` explicitly, traverses four pinned
files, and has the same exact output in Wago and wazero. The release binary
cannot be rebuilt from a known exact source commit, so its asset ID and digest
are the artifact pin.
