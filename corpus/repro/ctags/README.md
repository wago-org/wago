# a-Shell Ctags admission blocker

`fetch.sh` downloads pinned a-Shell release 0.1 asset `ctags.wasm` (asset ID
139432404, updated 2023-12-07). It imports `ashell_getcwd`, which the corpus
adapter provides as a virtual `/` path. This is enough to instantiate the
binary, but not to index the fixture: `ctags -x --sort=no /source.c` warns
`cannot open input file "/source.c"`. Wago reports `Bad file descriptor` and
wazero reports `Operation not permitted`. A relative path also fails in both.
The no-sort option avoids the binary's host-temp-file dependency; passing `-`
does not make it read source from stdin. No zero-workload result is admitted.

The remaining blocker is its a-Shell-specific filesystem/right handling, not
the `ashell_getcwd` import. A standalone rebuild or a carefully scoped WASI
filesystem compatibility layer is needed before admission. The binary is not
redistributed here; its exact source revision and linked licensing remain to
be established before committing an executable corpus artifact.
