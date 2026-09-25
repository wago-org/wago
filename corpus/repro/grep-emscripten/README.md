# GNU grep Emscripten divergence

`grep.wasm` comes from the Biowasm GNU grep 3.7 package. `build.sh` verifies the
published artifact and renames its legacy split-i64 `fd_seek` import so it
cannot be confused with the modern Preview 1 ABI. This is a retained repro,
not an admitted corpus workload.

For `grep error -` over `inputs/records.txt`, the matching Biowasm JavaScript
glue under Node/V8 writes the three matching records; stdout SHA-256 is
`7c7131c284ea18173f0d391ccb0a8a59989cb896d125d13f7b68b5b05d92b74c`.
Wazero produces the same output with the corpus mocks. Wago currently returns
status 1 with empty stdout, so this program must remain outside
`corpus/catalog.json` until that divergence is fixed.
