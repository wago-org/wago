# json2csv WASI CLI

The pinned `json2csv.wasm` is a-Shell release 0.1 asset `68082085`, updated
2022-06-10. Its SHA-256 is in `corpus/catalog.json`. The CLI corresponds to
[jehiah/json2csv](https://github.com/jehiah/json2csv), which is MIT licensed;
its `LICENSE` is included here. The release does not identify an exact source
commit, so the immutable artifact digest is the executable provenance pin.

The workload reads five newline-delimited JSON objects, extracts a nested
field and two scalars, and checks the complete CSV stream. Wasmtime produced
the pinned output independently of Wago and wazero.
