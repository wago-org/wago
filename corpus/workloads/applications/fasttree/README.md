# FastTree WebAssembly command

`fasttree.wasm` is derived from the Biowasm FastTree 2.1.11 artifact.
`build.sh` verifies the published module and expands its minified host names to
the shared Emscripten command contract.

The workload infers a nucleotide tree from five aligned sequences on stdin.
The exact Newick output is generated and checked in `build.sh` with the
published JavaScript glue under Node/V8 and
`corpus/tools/emscripten-v8-oracle.mjs`.
