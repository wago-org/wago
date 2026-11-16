# Same-source arena-cache gate

Production code `686ce58f`, qualification/docs `caa46ef0`; control and candidate binaries built together from the cache worker source with the same seven compiler tags, candidate adding only `wago_gc_direct_arena_cache`. Driver checkout4327a2eb is not the binary source. The source/build receipts are preserved alongside the runtime gate. No sparse cleanup, adaptive retirement, constructor or range-loop treatment is mixed into this comparison.

12/12 Wago audit and54/54 runtime process receipts passed, none missing. Artifact hashes, checksums, installed direct-view/config receipts and exact allocation/minor/full counts match across Wago variants. Audit128warmups verifies actual collection; timing100warmups then9×32calls in three rotating case/variant rounds. Young2MiB/two8MiB old, elision/age2off. Linux x86-64 Xeon8573C, Go1.27.1; Node24.22.1/V813.6.233.17-node.58 --no-liftoff. GOMAXPROCS1, no new affinity pin. Own local jobs paused, not a proven-exclusive host. Per-process UTC/load/allowed-affinity retained. Canonical adjacent V8 runner hash640fe8db000f7cde9a12929c3d85a89f469920bd5f62860c48885cee69b9d3a8 verified before execution. No failures/restarts occurred.

## Process-level comparison

Each cell is median of three process medians (each process contains nine batch samples), µs. This intentionally analyzes independent process repetitions rather than pooling27internal samples as independent.

| Case | Control µs | Candidate µs | V8 µs | Candidate/control | Control/V8 | Candidate/V8 |
|---|---:|---:|---:|---:|---:|---:|
| struct-allocation-access | 6267.403 | 5792.573 | 1462.498 | 0.924× | 4.285× MISS | 3.961× MISS |
| array-allocation-access | 14160.189 | 13857.141 | 9757.266 | 0.979× | 1.451× MISS | 1.420× MISS |
| array-fill | 16703.287 | 15373.926 | 11805.056 | 0.920× | 1.415× MISS | 1.302× MISS |
| array-copy-checksum | 302.063 | 311.136 | 169.001 | 1.030× | 1.787× MISS | 1.841× MISS |
| array-loop-copy-checksum | 650.945 | 684.629 | 189.498 | 1.052× | 3.435× MISS | 3.613× MISS |
| tree-survival | 2147.079 | 2251.316 | 1024.170 | 1.049× | 2.096× MISS | 2.198× MISS |

## Pair direction and process spread

Within-round ratios are diagnostics: processes are rotated, not simultaneous.

| Case | Three control medians µs | Three candidate medians µs | Three candidate/control ratios |
|---|---|---|---|
| struct-allocation-access | 6186.635, 6457.282, 6267.403 | 6668.001, 5792.573, 5781.672 | 1.078, 0.897, 0.922 |
| array-allocation-access | 14983.582, 12437.688, 14160.189 | 15311.552, 13479.174, 13857.141 | 1.022, 1.084, 0.979 |
| array-fill | 16703.287, 17307.890, 16018.069 | 15373.926, 16541.787, 13095.900 | 0.920, 0.956, 0.818 |
| array-copy-checksum | 302.063, 257.431, 347.962 | 353.893, 311.136, 280.783 | 1.172, 1.209, 0.807 |
| array-loop-copy-checksum | 681.400, 538.476, 650.945 | 643.677, 720.550, 684.629 | 0.945, 1.338, 1.052 |
| tree-survival | 2147.079, 2069.123, 2358.444 | 2251.316, 3099.030, 2220.571 | 1.049, 1.498, 0.942 |

## Admission and compile costs

Worker instrumented receipts identify one admitted cache in struct: one allocation use, one read use and two cold reload sites. The other five corpus cases are nonadmitted controls and their generated code sizes remain unchanged. Struct emitted size rises1775→1839bytes (+64bytes). Compile-probe evidence and admission receipts are copied without claiming those earlier compile probes ran in this runtime quiet window.

## Verdict

Every case in both variants misses the ≤1.15×V8 target. Struct process-median ratio is0.924× (7.6% lower), but its within-round ratios1.078,0.897,0.922 include a regression. Critically, nonadmitted fill also improves to0.920× despite unchanged generated code size/admission, whereas nonadmitted copy,loop-copy and tree regress by3.0%,5.2% and4.9% in process-median ratios. Control movement and only three process repeats prevent attributing the apparent struct improvement to the cache itself or claiming a solid general gain.

Keep cache opt-in pending a focused same-CPU, adjacent reversed-order struct comparison with nonadmitted controls and enough independent repetitions. Do not promote from this gate. Exact source mechanisms are plausible, but code-layout effects and external host variation remain possible. This is six local synthetic Wasm artifacts, not the complete wasm.fyi suite; V8 internal allocation elimination is not measured. No overall V8-parity or pure-collector speed claim follows.

Artifact paths in retained reproduction scripts are relative to an experiment workspace root; local execution prefixes have been removed. Hashes and measured data are unchanged.
