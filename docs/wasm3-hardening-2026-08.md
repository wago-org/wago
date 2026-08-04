# Wasm3/Core 3 hardening and footprint pass (August 2026)

This pass followed explicit test-first red/green phases for correctness and
runtime-hardening findings from the wasm3 branch audit.

## Correctness and hardening

- Generic allocating array helpers now synchronize mutable collector-backed
  globals before any collection, matching the struct-helper boundary. A forced
  collection regression previously produced a cast failure after a live struct
  handle was reclaimed and reused; Throughput and Tiny tests now return the
  original value.
- Generic `array.new_elem` now validates the complete source, allocates without
  default initialization, and then publishes the values. Valid arrays with
  non-null reference element types no longer fail the defaultability check.
- Linux guard installation reads both previous dispositions before replacement,
  publishes distinct SIGSEGV/SIGBUS chain targets before either handler is live,
  rejects unchainable dispositions, and rolls SIGSEGV back if SIGBUS installation
  fails. Both amd64 and arm64 assembly select the predecessor by signal number.
- Guard growth commits one reservation-relative 64 KiB Wasm page per fault. This
  removes up to fifteen additional signals and `mprotect` calls for sequential
  first touches of one newly grown Wasm page on linux/amd64.
- Context cancellation cleanup is deferred and idempotent, including arbitrary
  host panics. Signal retries are capped at 256 50-microsecond intervals; a host
  call that remains parked observes the armed trap on return without causing an
  unbounded `/proc/self/task` broadcast loop.
- The `.wago` artifact version is 31 because generated native code now depends on the widened basedata and indexed-memory ABI; version 30 artifacts are rejected rather than mixed across ABIs.
- Memory32 now supports the complete 65,536-page (4 GiB) boundary. Memory 0 uses
  an authoritative u64 byte-size cache at basedata offset 288; indexed-memory
  directory entries use u64 byte counts. The legacy u32 cache remains populated
  for compatibility. `NewMemory` rejects limits above 65,536 instead of silently
  truncating them.

## Allocation and memory work

Reference array construction gained reusable `ArrayInitializerRootScratch`
storage. On the Ryzen 7 8845HS audit machine, the direct constructor benchmarks
changed as follows:

| Constructor | Before | After |
|---|---:|---:|
| uniform reference array, 256 elements | 72 B / 4 allocs | 0 allocs |
| fixed reference array, 256 elements | 153 B / 5 allocs | 0 allocs |
| fixed reference array, 4096 elements | 171 B / 6 allocs | 0 allocs |

Wago's generic array helper owns one scratch object in `gcPublicState`; this adds
48 bytes to that lazy state and avoids per-constructor root-set/interface churn.

Throughput backing growth now becomes geometric after the first committed page,
rather than copying the entire backing slice once per page until 1 MiB. A 512 KiB
backing that needs 576 KiB now reserves 768 KiB in one growth step.

After the first executable mapping, `Compiled.Code` is re-sliced onto the exact
length of the RX mapping. The original Go-heap code backing can then be collected,
removing the previous second live copy while preserving codec/snapshot reads.

## Compile and invocation measurements

The duplicate post-lowering `moduleRequiredFeatures` body scan was removed;
`Compiled.requiredFeatures` reuses the result from `analyzeModuleRequirements`.
Representative serial cold results after the change:

| Fixture | Full compile | Allocated | JIT code |
|---|---:|---:|---:|
| wasm3, 184,108 bytes | 20.1–21.1 ms | 8.25 MB | 1,380,480 bytes |
| sqlite3, 938,882 bytes | 115.6–121.3 ms | 22.64 MB | 5,170,945 bytes |

A follow-on compiler audit compared the `bd45d76d` branch baseline with this
change on the same Ryzen 7 8845HS host, Go 1.24.4, `GOMAXPROCS=1`, pinned to CPU
0. Five repeated public-`Compile` measurements per side gave these medians:

| Fixture | Compile time | Allocated bytes | Allocations |
|---|---:|---:|---:|
| wasm3 | 27.29 ms → 26.02 ms (-4.6%) | 8.245 MB → 8.244 MB | 23,722 → 23,680 |
| sqlite3 | 148.02 ms → 138.54 ms (-6.4%) | 22.636 MB → 22.598 MB | 84,973 → 84,730 |
| Ruby | 1.718 s → 1.567 s (-8.8%) | 132.749 MB → 132.672 MB | 589,194 → 589,396 |
| esbuild | 1.302 s → 1.045 s (-19.7%) | 94.046 MB → 93.944 MB | 156,992 → 154,018 |
| Dew Map/Set WasmGC | 4.737 s → 157.7 ms (-96.7%, 30.0x) | 210.489 MB → 147.224 MB (-30.1%) | 704,372 → 104,962 (-85.1%) |

The Dew result exposed a quadratic trap-stub grouping pass: each trap site
rescanned every earlier site even when all sites belonged to one inlined source
function. Sorting sites once by source function makes grouping and patching
linear after the sort. The Dew native-code result remains exactly 6,886,271
bytes, so the compile-time reduction does not trade for more generated code.
A 16,384-site synthetic reproducer fell from a 43.56 ms median to 12.08 ms.

Exact GC-frame liveness now builds compact 40-byte CFG nodes, keeps ordinary
successors inline, stores `br_table` targets in one arena, folds reachability into
the node, omits the unused `liveOut` array, and computes allocation and call maps
in one dataflow pass. Its 16,384-operation microbenchmark changed from 2.786 ms,
9,929,337 B, and 34,834 allocations to 0.997 ms, 4,399,104 B, and 5 allocations.
Merge-state buffers likewise store one byte per pinned local rather than one per
Wasm local, and non-lazy functions skip impossible `lsConstZero` scans.

Immediately dropped, side-effect-free deferred expression trees are erased
before materialization. A 512-tree synthetic compile changed from approximately
283.0 to 245.5 microseconds while generated code fell from 5,240 to 120 bytes.
Integer div/rem and signals-backed loads remain materialized so their traps are
preserved; explicit-bounds loads may be discarded only after their check has
already executed. The stripped standard and minimal Go runtime binaries are both
4,096 bytes smaller than the `bd45d76d` baseline.

Reflection-free returning host benchmarks remain allocation-free at approximately
176–191 ns/op for direct and table-indirect calls on the audit machine.

### Complete benchmark refresh

A complete current-host benchmark sweep was repeated on August 4, 2026 after the
follow-on audit. The host was the same Linux/amd64 Ryzen 7 8845HS with Go 1.24.4,
`GOMAXPROCS=1`, and `taskset -c 0`. Ordinary benchmarks used three 100 ms samples;
the two cold-stage attribution benchmarks used five one-iteration samples as
required by their setup model.

The sweep enumerated all 306 top-level benchmarks available to the linux/amd64
build: 272 in the root module and 34 in the separate `bench` module. It produced
419 distinct root-module metric rows, 1,082 explicit-bounds corpus/ISA rows, and
1,084 signals-based corpus/ISA rows. All 21 benchmarks that require WABT were
rerun with the checksum-pinned WABT 1.0.41 tools on `PATH`; the eight Core 3 text
products that WABT cannot parse used the checksum/revision-pinned official spec
interpreter fallback. Each of those 21 benchmarks completed three samples.

The ARM64-only branch-hint benchmark, ARM64 dropped-tree compiler benchmark, and
ARM64 exception benchmarks were additionally executed under the local QEMU
compiler/runtime gate. Their timings are not presented as native ARM64
performance measurements. Ten optional benchmarks remain intentionally without
numbers because their external payloads are unavailable: five pinned MoonBit
JSON rows, four Starshine rows, and the out-of-tree SQL-injection fixture. They
were explicitly enumerated and skipped rather than silently omitted.

The benchmark publisher now accepts Go output both with and without the trailing
`-N` processor suffix. Go omits that suffix at `GOMAXPROCS=1`; previously a
pinned single-CPU capture was valid benchmark output but `benchpub` parsed zero
rows. Tests cover suffix-free and suffixed samples plus container-row removal.
Generated raw logs and charts remain local artifacts as required by
`docs/codegen-benchmarks.md`.

A stripped program that only loads a precompiled artifact links to approximately
2.11 MB on linux/amd64, versus approximately 4.69 MB for a program that invokes
the compiler. The existing package-level linker boundaries therefore already
permit compiler-free deployment when applications use `Compiled.UnmarshalBinary`
and do not reference `Compile`.

## Regression matrix

The pass added regressions for:

- mutable GC global survival across allocating array helpers, in both collectors;
- non-null-reference `array.new_elem`;
- host-panic cancellation cleanup;
- Linux signal install ordering, distinct chaining, and rollback;
- full 4-GiB initial memory and growth from 65,535 to 65,536 pages;
- executable-code single-copy residency and post-map serialization;
- array-root scratch correctness under forced collection;
- dropped deferred div/rem and explicit/signals-backed load traps;
- compact exact GC-frame liveness across if, loop, and `br_table` edges;
- large inlined trap-site groups without quadratic patching; and
- geometric Throughput backing growth.

Both the default and `wago_guardpage` runtime/Wago suites are expected to remain
green; snapshot fixtures explicitly compile with explicit bounds because
signals-based compiled modules are intentionally not snapshot-serializable.
