# SQLite file-backed query admission blocker

The passing `sqlite3-query` corpus command uses the same SQLite 3.53.4 Wasm
artifact with an in-memory JSON table and a thousand-row cross join. An
additional file-backed workload remains excluded: run the artifact with
`-batch :memory: ".read workload.sql"` and preopen this directory as `/`.
Wasmtime and wazero produce the following stdout:

```text
blue|333|16048|0|96
green|334|15924|0|96
red|333|15895|0|96
999|2449090
```

The output SHA-256 is
`851e863834c4fcde81e772cf74686b142691fc03519359312936de973f36b15a`.
With Wago's current WASI host, the same invocation traps with a linear-memory
out-of-bounds access before producing stdout. A recursive CTE passed directly
as a command argument instead produced a disk I/O error; setting
`PRAGMA temp_store=MEMORY` still trapped. These are distinct from the passing
in-memory JSON scan and have not been admitted to the corpus.
