# Shared function compilation: further regression investigation

**Keep the experiment unaccepted.** This pass removes substantial allocation and frame regressions, but does not establish an acceptably small execution regression. Pressure execution has an unfavorable median in both final timing cohorts, with uncertainty too wide to bound it. A nonsignificant difference is not an equivalence proof.

This supplements [the initial experiment](shared-function-compilation.md) and [the first execution mitigation](shared-function-execution-mitigation.md). Their frozen baseline, subset, correctness exclusions and original helper-only comparison remain intact. No rebase, merge, push, or workload selection change occurred.

## Frozen revisions

| Point | Exact production source | Meaning |
|---|---|---|
| A | `31547a885b6770cc0f1718f69bce495b5f3e38ce` | Original frozen baseline |
| D | `ddb6abda094f2af9103beb47c4452ad0c43c6424` | Previous measured execution mitigation |
| E | `1ac475a57b61c82a5ffd1a620af3e6498672cba8` | Intermediate join, clean-spill and operand-arena changes |
| F | `9ce1333cb013c94102dacdf14465cecdbb208e44` | Final candidate in this report |

D was delivered with tests/documentation at `caaf8fca423bebe723e5810856f684fdc745533e`. B, the original helper-only commit `9ff39cf3a2aabb9fb2204cdb5d426a6bb59f207a`, was not retimed in this follow-up. The isolated branch remains `experiment/shared-function-compilation` in `/home/jtenner/.codex/worktrees/shared-function-pilot/wago`.

Implementation is split into seven commits:

| Commit | Change |
|---|---|
| `3aecf1eb83b91c04f6995ddca97a012ac23d0e2e` | Preserve clean local homes and agree on one result register at if exits |
| `3f04b6368477b2b44bb9e240139885311e84f51c` | Size common scratch from actual value-record creation |
| `c6d88b86b2d5095c51aac9372ff7d63e6800333a` | Evict clean values to existing local homes without another store |
| `1ac475a57b61c82a5ffd1a620af3e6498672cba8` | Allocate target operand arenas only on first fallback |
| `d52bf3fa1d5e6ac049471801727ec047611bf1f9` | Reuse spill slot zero until terminal return in no-if functions |
| `162d3a43b451c5183bd4a053bbc8210622758332` | Defer target control-frame reservation until first fallback |
| `9ce1333cb013c94102dacdf14465cecdbb208e44` | Use AMD64 LEA for immediate addition with a live source |

Full commit identities, binary SHA256 values and harness hashes are also in the evidence manifest. Final delivery adds only this report after F.

## Ownership and safety

The common layer still owns the streaming stack, stable value IDs, deferred edges, authoritative locations, register owners, local state, spill accounting, agreements and return preparation. No new IR, independent target semantic state, per-operation object, or unbounded scan was added. The target arena representation and growth policy are unchanged; allocation is deferred until a function actually needs fallback.

A node's clean home is a valid memory copy, independent of its authoritative location. Overwriting a local invalidates an older version's home before materialization can evict that value. Canonicalization captures older borrows and deferred dependencies before stores, and skips stores to unchanged homes. Both branch exits store local/prefix state before moving their single result to the same nonreserved register. Reconstruction then releases old ownership and installs the result owner. Detached predicates can briefly retain their old home identities across entry reconstruction; their physical copies remain valid until `condition()` immediately consumes them, before body writes.

Without an if, slot zero can hold temporary spills: return fully materializes the only result before overwriting that slot, and admission excludes any later consumer. If-containing functions retain disjoint agreement and temporary ranges. Tests exercise register-result and memory-result ABIs. AMD64 LEA uses the existing encoder and signed-displacement legality; i32 truncation and i64 addition preserve integer semantics. Shared arithmetic carries no live flags across these operations.

The original conservative admission budget remains a separate check. A tighter allocation bound counts actual node creation without expanding eligibility. Diagnostics confirm **1,333/1,380 functions and 23,451 body bytes** still migrate. Corpus/input hashes are unchanged. The synthetic subset remains 1,031/1,032 functions and 21,406/21,412 bytes; existing modules remain 302/348 and 2,045/21,069 bytes.

## Tests and independent review

Final AMD64 core/compiler/runtime, public API and conformance checks pass with the existing pinned WABT 1.0.41 and spec interpreter revision `9d36019973201a19f9c9ebb0f10828b2fe2374aa`. Checked focused backend/public tests and the fixed fixtures/selected corpus checks pass. New cases cover nested result registers, older local versions under real pressure and overwrite, both integer widths, both return ABIs, and shared/fallback transitions in both orders with one and two workers.

ARM64 checked focused tests execute successfully under QEMU. The final ARM64 binary was built at `162d3a43`; F's subsequent change is confined to an AMD64 build-tagged file. Native ARM64 hardware and performance remain **not measured**. Cross-builds are not presented as execution testing.

Independent agent `contract_review` reviewed each ownership, spill, control, lazy-allocation, return-slot and LEA change. No concrete defects were found. Its lifetime nuance about detached predicate homes is documented above. The review was read-only and ran no builds/tests. An omitted scratch-field declaration caused one temporary build failure; it was fixed before the final build and measurement, and the failed log is retained.

## Measurement cohorts

All cohorts use the identical delivered harness and fixed 14-module corpus. No samples were discarded or replaced. Production binaries were built before timing. Native compilation includes analysis/codegen and supported Close, with decode/validation setup outside timing. Full compilation also includes decode/validation. Execution compiles outside timing and verifies outputs. Every compile calls the compiler; outputs are closed, not cached or accumulated. The existing same-instance JSON-AS GC-state limitation remains.

Host: AMD Ryzen 7 8845HS, Debian 13/Linux 6.12.111+, Go 1.27.1, GOAMD64=v1, normal optimization, no production tags, explicit bounds checks. GOMAXPROCS=1, public-default one compiler worker, GOGC=100, GOMEMLIMIT=off. Performance governor and boost were enabled; frequency was not locked. No builds/tests/profiling ran during timing. Other desktop activity could not be eliminated.

| Cohort | Order, repeated three times | Duration | Fresh processes per revision | CPU |
|---|---|---:|---:|---:|
| Intermediate | A/D/E/E/D/A | 200 ms, all 43 rows | 6 | 2 |
| Final primary | A/F/F/A | 200 ms, all 43 rows | 6 | 2 |
| Longer execution diagnostic | A/D/F/F/D/A | 1 s, all nine synthetic execution rows | 6 | 4 |

Each process uses count 1. Iterations within a process are not independent samples. Aggregates are equal-weight per-process geometric means, not production-weighted throughput. Benchstat reports median, 95% envelope, p and n. Multiple comparisons weaken isolated marginal p values. Do not combine these cohorts into one A/D/F primary comparison or splice percentages across them.

The final primary run was noisier than the intermediate run. A subsequent host snapshot found active Chrome processes and 32.1% combined utilization on CPU2/3's sibling pair over three seconds, versus 5.5% on CPU4/5. CPU4 was selected for the longer follow-up using that load observation, before viewing its results. It still exhibited large outliers. This snapshot establishes host activity, not a causal attribution for every earlier sample.

## Final primary timing

Each row has six independent processes per revision. Values marked inconclusive do not demonstrate equivalence.

| Boundary | A | F | Change / evidence |
|---|---:|---:|---|
| Migrated native compile aggregate | 64.20 µs ±6% | 49.30 µs ±6% | −23.21%, p=.002 |
| Migrated full compile aggregate | 82.97 µs ±8% | 68.15 µs ±11% | −17.86%, p=.002 |
| Migrated execution aggregate | 46.00 ns ±7% | 43.74 ns ±7% | Inconclusive, p=.240 |
| Full-sample native compile aggregate | 69.48 µs ±7% | 61.36 µs ±5% | −11.69%, p=.002 |
| Full-sample full compile aggregate | 90.40 µs ±10% | 81.68 µs ±9% | Inconclusive, p=.065 |
| Pressure execution | 51.64 ns ±10% | 54.34 ns ±10% | Median +5.23%; inconclusive, p=.310 |
| Join execution | 16.55 ns ±10% | 16.80 ns ±21% | Median +1.51%; inconclusive, p=.589 |
| Many-locals execution | 84.40 ns ±4% | 56.30 ns ±1% | −33.29%, p=.002 |

The earlier intermediate cohort found D→E migrated native compilation −7.34% and full compilation −3.31%; execution was inconclusive. E pressure remained +2.50% versus A (p=.026), prompting the final return-slot and LEA work. All intermediate results are retained.

The longer execution diagnostic also fails to bound the remaining regressions:

| Boundary | A | D | F | A/F evidence |
|---|---:|---:|---:|---|
| Pressure | 47.16 ns ±29% | 47.94 ns ±12% | 49.86 ns ±38% | Median +5.73%; p=.093 |
| Join | 15.39 ns ±36% | 15.95 ns ±4% | 15.79 ns ±49% | Median +2.60%; p=.699 |
| Migrated execution aggregate | 43.82 ns ±9% | 42.04 ns ±5% | 42.00 ns ±16% | Inconclusive, p=.065 |

The unfavorable pressure medians remain visible. Disassembly verifies that F is smaller, but its reduction begins with recently spilled memory operands while A first reduces several register operands. Different spill-victim choices and resulting dependency/store-forwarding behavior are plausible remaining causes; this is an inference from code, not a measured microarchitectural attribution. No additional allocator-policy change was made in this pass. The investigation thresholds are not automatic acceptance criteria, and neither cohort supports a tight execution-regression bound.

## Deterministic allocation and native storage

Native compilation allocation medians are exact across the six final processes:

| Workload | A B/op | F B/op | Byte change | A/F allocs/op |
|---|---:|---:|---:|---:|
| Small | 8,248 | 7,536 | −712 (−8.63%) | 21/20 |
| Pressure | 19,488 | 12,120 | −7,368 (−37.81%) | 18/23 |
| Join | 10,152 | 7,936 | −2,216 (−21.83%) | 26/21 |
| Deep blocks | 11,504 | 10,000 | −1,504 (−13.07%) | 22/26 |
| Many locals | 29,192 | 20,592 | −8,600 (−29.46%) | 21/22 |
| Many functions | 50,600 | 49,888 | −712 (−1.41%) | 24/23 |
| Large then small | 275,320 | 119,680 | −155,640 (−56.53%) | 30/24 |
| Fallback | 8,248 | 8,584 | +336 (+4.07%) | 21/22 |

Many-locals allocation was 58,888 B/op in D. The reduction comes from tighter common node reservation and removing unused target backing, not a smaller source-line count. Some workloads still have more allocation events despite fewer bytes. They are bounded per-worker backing allocations, not a new allocation for every operation.

| Function/storage | A | D | F |
|---|---:|---:|---:|
| Pressure code bytes | 825 | 611 | 510 |
| Pressure frame bytes | 232 | 248 | 232 |
| Pressure spill-slot high water | 27 | 29 | 27 |
| Join code bytes | 85 | 117 | 90 |
| Join frame bytes | 24 | 40 | 24 |
| Small code/frame bytes | 46/0 | 51/24 | 49/24 |
| Deep code/frame bytes | 45/0 | 46/24 | 46/24 |
| Many-functions code/summed frame bytes | 14,865/0 | 17,425/12,288 | 16,401/12,288 |

Frame/slot parity is established for pressure and join; leaf frames still regress. F pressure code is 315 bytes smaller than A (38.18%). Join code remains five bytes larger (5.88%). Summed function frames are native storage requirements, not simultaneous resident process memory.

All shared-only synthetic modules reserve zero target operand/control backing in F. For many locals, common scratch is 7,192 bytes; pressure is 4,340 and deep is 1,396. Full per-module NodeScratch*, ScalarScratch*, ControlScratch*, hints and function-attempt counters are in `scratch-accounting.md`. Worker high-water sums are accounting envelopes, not simultaneous peaks. Serial retained counters describe backing at the compiler observation point, not proof of a retained Go object. Large-then-small exercises reuse inside one module worker.

## Fixed-work memory and RSS attribution

Primary memory runs are separate from latency: 270 compile/close operations, then three batches of 36 retained modules followed by Close, reference removal and GC with Runtime alive, then Runtime close. A separate native mapped-output scenario follows. Both use the same fixed nine synthetic workloads. Six fresh processes per revision run identical work. No forced OS memory release is used.

| Measurement | A median | F median | Change |
|---|---:|---:|---:|
| Public compile/release allocated bytes | 37,770,424 | 27,770,336 | −10,000,088 (−26.48%) |
| Public compile/release allocations | 55,065 | 54,816 | −249 (−0.45%) |
| Native compile/release allocated bytes | 19,245,136 | 9,283,664 | −9,961,472 (−51.76%) |
| Native compile/release allocations | 6,295 | 6,055 | −240 (−3.81%) |
| Retained native code bytes, 36 modules | 167,268 | 163,060 | −4,208 (−2.52%) |
| Retained native mapped bytes | 278,528 | 278,528 | 0 |
| Public HeapAlloc after Runtime release | 206,392 | 206,088 | −304 (−0.15%), overlapping spread |
| Native HeapAlloc after third release | 365,632 | 365,328 | −304 (−0.08%), overlapping spread |
| Peak process RSS, KiB | 20,986 | 23,082 | +2,096 (+9.99%) |
| Peak RSS min–max, KiB | 20,488–22,848 | 22,824–24,856 | Six processes each |

All lifecycle metrics, including allocation deltas, HeapInuse/Objects/Released and current RSS, appear with spreads in the external memory tables/JSON. Public release-phase heap plateaus rather than growing each cycle. The initial report's delayed-output-reclamation/extra-GC caveat remains; the later heap drop cannot be assigned solely to engine teardown. Sampled retained heap profiles again identify compiled metadata/snapshots; their stochastic profile totals are not exact cross-revision heap deltas.

The normal RSS increase is a real measurement and remains in the primary table. It is not explained by compiler scratch or native mappings: initial median RSS, before any compile, was 12,464,128 bytes in A and 16,422,912 in F, despite nearly equal live Go heap.

Separate, identical `/proc/self/smaps` and heap-profile overlays establish a startup-residency contributor. This host has transparent huge pages set to `always`. Candidate BSS mapping `011be000-03213000` carries an extra 2,048 KiB anonymous huge page. Test-binary text grows about 49.9 KB and BSS only 32 bytes; segment placement changes physical residency in 2 MiB units. Setting `GODEBUG=disablethp=1` removes Go-heap huge pages but leaves that BSS page in F. The diagnostic is binary/layout-specific and does not establish that every deployed runtime has the same offset.

To check the attribution, a child-process launcher sets `PR_SET_THP_DISABLE` before exec, checks it with `PR_GET_THP_DISABLE`, and runs the same unmodified A/F production benchmark binaries. It changes no global setting and does not force page release. Three A/F/F/A fixed-work memory blocks give:

| Separately labelled whole-process THP-disabled diagnostic | A | F |
|---|---:|---:|
| Peak RSS median, KiB | 19,576 | 19,506 |
| Peak RSS min–max, KiB | 19,320–20,384 | 19,448–19,568 |
| Samples | 6 | 6 |

The ranges overlap; F is not shown to reduce physical process memory. Additional smaps runs verify zero huge pages in both processes and remove the startup offset. This supports huge-page/layout attribution, while **not replacing normal-process results or recommending a production OS-policy change**. Current RSS can also include reusable allocator pages; Go allocation totals omit native mappings.

## Remaining costs and verdict

1. **Execution remains unqualified.** Pressure's final median is approximately 5–6% above baseline in two cohorts, but host noise prevents a tight estimate. Join timing is inconclusive. Investigate spill-victim order/dependency chains on an otherwise idle native host; retain all existing unfavorable samples.
2. **Normal process RSS remains higher.** Huge-page/BSS placement explains an important component in these test executables. Qualification of actual deployment binaries and their memory policy remains necessary; compiler allocation savings are not RSS parity.
3. **Leaf storage is still conservative.** Small/deep functions retain 24-byte frames where A has none; many functions retain 12,288 summed frame bytes and 1,536 extra code bytes (+10.33%). Join retains five extra code bytes.
4. **Some allocation counts and fallback overhead remain.** Pressure adds five allocations (27.78%), deep four (18.18%), and many locals one (4.76%). Fallback adds 336 B/op and one allocation. Aggregate allocation totals improve substantially, but do not erase these cases.

**Recommendation: retain the fixes on the isolated experiment branch, but do not accept or merge the shared pilot yet.** Correctness, allocated bytes, mapped native storage, and two important frame regressions have stronger evidence now. Execution equivalence or a suitably small regression has not been demonstrated. Native ARM64, additional worker policies, full ARM64 backend emulation (baseline-reproducing crash), the known unrelated root CLI/installer failures, strict JSON-AS state reset, and transfer-level instrumentation of the shared allocator remain incomplete. Admission timing counters are preserved separately, but the old copied standalone admission microbenchmark was excluded because it predates the tighter sizing logic.

## Reproduction and preserved evidence

All large logs, raw samples, binaries, disassembly and profiles are outside production source:

`/home/jtenner/.codex/experiments/wago-sharing-regressions-20261003`

`manifest.json` records frozen sources, primary/diagnostic binary hashes and identical harness hashes. `diagnostics.json`, `corpus-code-hashes.tsv`, `scratch-accounting.md`, `*-pressure.bin/.asm`, and their hashes preserve admission/output/storage proof. `measurement-runs.json` has 36 intermediate timing/memory processes; `final-measurement-runs.json` has 24 final primary processes; `long-measurement-runs.json` has 18 longer execution processes; all exited successfully. `final-results/`, `long-results/` and `nothp-results/` contain separate analyses. No cohort is silently substituted for another.

Use the unchanged five `bench/suite/sharing_*_test.go` harness files identified by the manifest in each frozen worktree. Build from each repository root, run from `bench/suite`:

```bash
go test -c -o /tmp/F-suite.test ./bench/suite
go test -c -tags=wago_codegenstats -o /tmp/F-diagnostic.test ./bench/suite
cd bench/suite
GOMAXPROCS=1 GOGC=100 GOMEMLIMIT=off taskset -c 2 /tmp/F-suite.test -test.run='^$' -test.bench='^(BenchmarkSharing(Native|Full|Exec)|BenchmarkCompile|BenchmarkCompileFull|BenchmarkExec)$' -test.benchtime=200ms -test.count=1 -wago.corpus=tiny,fib_rec,many_funcs,xxhash,json-as
GOMAXPROCS=1 GOGC=100 GOMEMLIMIT=off taskset -c 4 /tmp/F-suite.test -test.run='^$' -test.bench='^BenchmarkSharingExec$' -test.benchtime=1s -test.count=1
GOMAXPROCS=1 GOGC=100 GOMEMLIMIT=off WAGO_SHARING_MEMORY=1 taskset -c 2 /tmp/F-suite.test -test.run='^TestSharing(Memory|MappedMemory)$' -test.v -test.count=1
```

Unset `WAGO_SHARED_SCALAR`; repeat the balanced orders above. `run_measurements.py`, `run_final.py`, `run_long.py`, `resource_wrapper.py` and their analysis scripts preserve exact commands and ordering. `run_nothp.py` and `without_thp.py` preserve the separate whole-process THP diagnostic. Smaps overlays and `run_smaps*.py` preserve identical diagnostic modifications on A/F; their profiles and mappings are not primary memory samples. Preserve an existing output directory before rerunning scripts, which use fixed filenames.

Correctness commands from the candidate root:

```bash
PATH=/home/jtenner/Projects/wago/.tools/wabt-1.0.41-linux-x64/bin:$PATH WAGO_SPEC_INTERPRETER=/home/jtenner/Projects/wago/.tools/spec-interpreter-9d36019973201a19f9c9ebb0f10828b2fe2374aa/wasm WAGO_SPEC_INTERPRETER_REVISION=9d36019973201a19f9c9ebb0f10828b2fe2374aa go test -p 1 ./src/core/... ./src/wago/... ./tests/...
go test -tags=wago_regalloccheck ./src/core/compiler/backend/railshot/shared ./src/core/compiler/backend/railshot/amd64 ./src/wago -run 'TestShared|TestGolden|TestExec|TestControl|TestModuleControl' -count=1
GOOS=linux GOARCH=arm64 go test -c -tags=wago_regalloccheck -o /tmp/F-arm64.test ./src/wago
/tmp/qemu-aarch64-static /tmp/F-arm64.test -test.run '^TestSharedScalar' -test.v
```

For missing native ARM64 results, build the frozen A/D/F sources and identical harness on a native ARM64 host, replace the backend package with `railshot/arm64`, and run the same correctness, timing and memory commands with an available physical CPU. Emulated timings must not be reported as native results.
