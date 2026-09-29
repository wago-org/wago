# Profiler qualification snapshot

Implementation base: `5aaa150c3b2afe269271a2d7a834114ae9a260b7`, with uncommitted
profiling changes. This snapshot records local qualification on September 28,
2026. It is not a claim that every platform, ABI, or workload is qualified.

## Delivery scope

The delivery target is the guide's first useful release: a defined workload and
capture phase, durable guest/helper attribution, and output usable by existing
viewers. The implementation also includes optional timelines, sparse source and
compiler-site annotations, comparisons, and limited AMD64 unwind metadata.
Those additions do not imply complete stack or platform support.

| Requirement | Implemented contract and qualification boundary |
|---|---|
| Remove all diagnostics from ordinary builds | Build-time removal covers recording, compiler and GC telemetry, image metadata, timelines, exporters, and reports. The 24-binary audit complements state-layout, allocation, and generated-code checks. |
| Reproducible workload capture | Validated workload definitions, explicit phases, actual completed work, versioned manifests, and failed/incomplete capture states replace the JSON-only harness. Collector phase isolation is reported per backend. |
| Correct code-image ownership | Finalized regions, logical and mapping identities, real mapping retirement, shared-instance ownership, and ordered attachment are covered by deterministic and race tests. Metadata absent at compilation or artifact reload remains explicitly unknown. |
| Native collection and existing viewers | Linux perf/jitdump and macOS Samply leaf attribution have real capture evidence; perf-map and pprof exports remain available. Linux repeated-startup reliability has an unresolved collector stall documented below. |
| Connect hotness to compiler output | Function statistics, sparse final-PC origins, explicit spill/reload and bounds-branch sites, annotation, and comparable per-operation diffs are available. Static decisions are not reported as measured savings. |
| Optional elapsed timelines | Nested boundaries and lifecycle spans preserve invocation/instance identity and distinguish inclusive/exclusive elapsed time from CPU measurements. Loss suppresses unsupported exclusive estimates. |
| Honest capability reporting | Complete mixed stacks, ARM64 unwind rules, source-file lines, and additional backends remain unqualified. Cooperative sampling and guest-memory profiling are future extensions from the guide. |

The proposed 3% sampling budget has supporting local measurements, not a
controlled-runner release guarantee. The unresolved Linux startup failure must
be resolved or independently qualified before advertising reliable repeated
capture on that collector/kernel combination. It does not justify suppressing
errors or marking failed captures complete.

## September 29 review follow-up

The implementation remains experimental. The CI fixes at `ccdc008b` passed the
full GitHub CI run, including Linux/amd64 TinyGo. Follow-up capture/report fixes
add phase-local completed work, one text/JSON top model, independent collection
and conversion safety deadlines, and saved-input/aggregation limits.

Native Linux/amd64 regression tests cover silent injection and sample-conversion
hangs, deadline errors, child reaping, incomplete manifests, and preservation of
raw data. The integrated CLI test uses a non-returning Wasm call to distinguish
measurement duration from the independent collection deadline. Phase reporting
tests hold initialization samples fixed while changing subsequent iteration
counts; metadata-only JSON and text share rows and limits. Input tests cover
compressed expansion, byte/count/code limits, metadata and aggregation limits,
and trailing JSON. Focused suites passed with Go 1.22.12 on native AMD64; ARM64
race checks passed with Go 1.26.5.

Staticcheck 2024.1.1 now checks ordinary, runtime, profiling, and compiler/GC
telemetry builds. Existing reviewed ordinary findings apply to shared code in
diagnostic builds; new findings and duplicate occurrences still fail. All four
profiles passed with no new findings. Two unused helpers exposed by the expanded
check were removed. All 24 ordinary-build DCE configurations passed again.

These checks do not resolve the historical perf enable-acknowledgement stall or
establish a new overhead bound. Artifact sidecars, application harness APIs,
rotation/churn scaling, and deeper stack/memory analysis remain later milestones.

## Current results

| Check | Result |
|---|---|
| Ordinary-build diagnostic DCE | Passed for 24 configurations: manager, standard runtime, minimal runtime, and embedding application; Linux/macOS/Windows on AMD64/ARM64 |
| Release sizes | All four unchanged Linux/amd64 budgets pass with Go 1.22.12, TinyGo 0.41.1, and GNU strip |
| Ordinary-build allocations | No additional bytes or allocations in six paired compilation, instantiation, and invocation benchmark cases |
| Darwin/arm64 ordinary runtime suite | Passed, including `go test ./...` and generated-facade consistency |
| Darwin/arm64 full diagnostic build | `go test -p=1 -tags=wago_profile,wago_gcstats ./...` passed; the separate bench-module profiler, JSON preset, and explain packages also passed with `wago_profile` |
| Explicit compiler diagnostics | ARM64 and native AMD64 suites pass with diagnostics enabled and disabled; CI exercises both |
| Profile lifecycle, bounded journal, export, and attribution | Deterministic and race tests pass |
| Artifact-reload capture phase | Public/prepared and reload/execute/all modes pass on native ARM64 and AMD64; ARM64 race repetitions pass; real JSON-AS Go CPU capture validates the reloaded workload |
| Linux/amd64 mappings and source metadata | Native tests pass for shared mappings, startup registration, import indexes, artifact reload, and late attachment |
| Linux real perf 7.0.14 capture | Execution-phase capture, jitdump injection, flat guest attribution after teardown, source annotations, and standard perf/pprof viewers passed |
| Linux repeated capture reliability | The enable-acknowledgement stall was reproduced and remains unresolved. A later 500-capture short-run sequence and all 18 long overhead captures completed without loss |
| Linux/amd64 native sampling overhead | At 99 Hz, paired median elapsed cost changes were 0.002% for JSON-AS, 0.365% for matrix multiplication, and 0.564% for SHA-256 on the qualified host; this is not a universal overhead or reliability guarantee |
| macOS/arm64 Samply 0.13.1 | Real guest leaf attribution and paired overhead experiments passed; observations are not presented as CPU nanoseconds |
| Jitdump DWARF record exporter | Native recursive fixture recovers nine frames with perf/libdw; metadata-free control recovers one. This is exporter qualification, not Railshot unwind support |
| Complete mixed Go/Wasm stacks | Not qualified; never advertised |
| Source maps | Principal opcode/deferred lowering origins and static inline ancestry tested on ARM64/AMD64; exhaustive provenance and source-file lines remain unavailable |
| Optional boundary timeline | Nested guest/host/helper paths, lifecycle, re-entry, traps, cancellation, panic, and capacity-loss fixtures pass; these are elapsed spans, not CPU measurements |
| Cost comparisons | Tests separate elapsed, sampled function, helper, and unknown costs per completed iteration; mismatched collection settings and ambiguous compiler joins are covered. The CLI reads the saved 194-sample native JSON-AS capture and preserves its totals in text/JSON comparisons |

## Default-build removal and size

Ordinary builds, including embedding applications, omit profiling and compiler
telemetry. `wago_profile`, `wago_codegenstats`, and `wago_gcstats` explicitly enable
the corresponding diagnostics. Requests for unavailable counters fail rather
than quietly returning empty statistics. Diagnostic environment reads compile
out. Ordinary compiled caches retain their 64-byte AMD64 footprint; the optional
live-attachment state occupies zero bytes in an ordinary build.

`scripts/check-diagnostic-dce.sh` inspects unstripped binaries for collectors,
journal operations, source-map hooks, compiler-counter bodies, boundary recording,
and capture/report implementations. API no-op stubs and type descriptors are not
active telemetry. All 24 configurations passed with Go 1.26.5.

The separate size gate uses the CI toolchain, Go 1.22.12 and TinyGo 0.41.1,
targeting Linux/amd64. GNU strip 2.42 removes TinyGo unwind sections and compacts
the ELF file. LLVM strip preserved segment gaps for the same input and produced
a larger file; that output is not interchangeable with the CI measurement.
`scripts/size-card.sh` accepts a GNU-compatible `SIZE_STRIP_TOOL` override and
handles an empty over-budget symbol list on macOS Bash 3.2.

| Profile | Bytes | Budget | Delta from unchanged base |
|---|---:|---:|---:|
| Manager | 7,983,256 | 9,000,000 | +4,096 |
| Standard runtime | 8,114,328 | 8,870,000 | -90,112 |
| Minimal runtime | 7,782,552 | 8,560,000 | -94,208 |
| Minimal TinyGo runtime | 2,355,904 | 2,420,000 | -45,864 |

Budgets were not raised. Earlier Go 1.26.5/LLVM-strip size failures are superseded
by this matched CI-toolchain comparison, not evidence that those larger outputs
passed. Both GNU- and LLVM-stripped TinyGo binaries executed the Fibonacci oracle.

Eight alternating baseline/candidate pairs used Go 1.22.12, `GOMAXPROCS=1`, and
300 ms benchmark intervals on Apple M4 Max. Paired median timing changes for
small-scalar compilation, table-host-thunk instantiation, scalar invocation,
both branch-hint cases, and re-exported invocation were within ±0.9%. Every case
retained its baseline bytes and allocation counts. Individual timing outliers
preclude interpreting these local runs as speedups or a universal throughput gate.

## Native sampling

A five-second Linux/amd64 JSON-AS capture at 99 Hz completed 77,301 validated
iterations and 154,602 invocations. Perf retained 492 samples: 480 resolved to
registered guest regions, and 12 remained unknown/non-guest. The journal retained
both image load and retirement events with no record or span loss. `perf report`
reported zero lost samples and resolved generated symbols after the child exited.
`go tool pprof -top` accepted `native.pprof`; `wagoprof annotate` joined sampled
`deserializeN` PCs to Wasm origins and static compiler decisions.

The host was an AMD Ryzen 7 7800X3D with kernel 7.0.0-31-generic and perf 7.0.14.
Its `perf_event_paranoid=4` policy remained unchanged. Qualification used a
read-only Ubuntu 24.04 container, default seccomp, no network, and only `PERFMON`
added to an otherwise empty capability set. The collector and its libraries,
runner, and workload inputs were mounted read-only; output and temporary files
had dedicated writable locations. This qualifies flat attribution on that setup,
not complete call chains or all Linux perf versions.

Real capture exposed perf's NUL-terminated `ack\n` response. The controller now
accepts both that response and the newline-only form. Native Linux tests cover
both forms across enable/disable, malformed replies, and denied capture. A later
repeated capture timed out before perf acknowledged enable; its child manifest
and collector log report failure. The host was also running unrelated workloads.
Six subsequent captures completed under syscall tracing, so the intermittent
startup failure is not claimed fixed. A failed child now triggers an interrupt
to perf, followed by forced shutdown after two seconds if necessary. A native
Linux regression test verifies bounded shutdown even when the collector ignores
interrupts, and checks that the original child failure survives in the bundle.
Consequently, that interrupted Linux experiment did not establish an overhead
budget or reliable repeated startup on that host. A later completed experiment
is recorded below; it does not explain the earlier startup failures.

A later short-capture reproduction failed on run 11 with 50 warmup iterations
and on run 6 with no warmup. Both failed before executing measured work. A
FIFO-focused syscall trace reproduced it on run 4: perf read the entire
`enable\n` command, then neither announced enabled events nor wrote the
acknowledgement before the child timed out. This rules out a lost command in
those runs, but does not identify the operation stalling inside event enable.
More intrusive syscall tracing passed 30 and 100 runs; sampling the collector
passed 40, and a separate process-state monitor passed 80. Those instrumented
passes do not establish a fix for a timing-sensitive failure.

A subsequent 500-run loop used a monitor that would sample the collector only
after its process persisted across two one-second checks. All 500 short captures
completed, so no stall profile was taken. Their manifests and reports contain
387,459 validated iterations and 1,958 native samples, including 23
unknown/non-guest samples, with no metadata loss. This establishes success for
that run sequence, not resolution of the earlier reproduced failure.

The returned CLI error now includes the original child diagnostic instead of
only the collector's eventual `signal: killed` status. Conversion requires a
readable, complete final child manifest, even when the collector itself exits
successfully. Regression tests cover missing/malformed manifests, child failure
with and without collector failure, and preservation of both errors.
Missing or invalid manifests now produce a schema-valid failed bundle whose
actual execution facts remain unknown. A damaged child manifest is preserved
byte-for-byte, and evidence-name collisions fail without overwriting either
file. The native Linux collector test also checks that raw perf bytes and the
original log survive interrupted manifest export and that conversion is skipped.
Collector completion is now a separate publication step: a successful child
leaves an incomplete manifest marked `collector_pending`, and only the parent
publishes completion after retaining output and finishing conversion. A real CLI
child/collector handoff test observes that intermediate state. Pending captures
are rejected by CPU reports; failed publication retains the old state and removes
temporary files. Native Linux protocol tests cover pending-child monitoring and
successful/failed injection. A real one-second perf capture completed with 15,787
validated iterations and 98 samples, two unknown/non-guest, with no metadata loss
or leftover temporary manifests.
The matching real Darwin/arm64 Samply check completed 20,140 validated iterations;
its 816 process-wide observations included 99 resolved guest leaves. It also
finished with no metadata loss, no pending collector flag, and no temporary
manifest files. These observations span all process phases and are not CPU
nanoseconds.

Perf conversion now streams directly into the sample parser. A CLI fallback
regression reproduced the old behavior with malformed output followed by a
sleeping producer; it now fails promptly and reaps the producer. Tests also cover
exact timestamps/weights, sample-limit cancellation, producer exit failures,
missing executables, and bounded diagnostic output for both injection and sample
conversion. Conversion errors return no partial sample list.
The complete capture package passed three repetitions on native Linux/amd64,
and race checks passed on Darwin/arm64. A real streamed perf capture retained
15,942 validated iterations and 98 samples, three unknown/non-guest. Replaying
its raw `perf.data` without `samples.json` produced an identical report, including
weights, function attribution, and compiler/source annotations.

The later Linux overhead experiment completed all 18 captures: three alternating
`none`/`perf` pairs per workload, using ten-second execute intervals, 50 warmup
iterations, the public invocation mode, native-code inclusion, and 99 Hz. The
same Go 1.26.5 profiling binary ran both backends; source maps, raw stacks, and
boundary timelines were disabled. Workload contracts, module hashes, target,
invocation mode, warmup, CPU, OS, and Go version matched within each workload.
Every capture validated its results and reported no metadata loss. The three
perf captures per workload retained 2,933, 2,944, and 2,950 native samples for
JSON-AS, matrix multiplication, and SHA-256 respectively.

| Linux workload | Median overhead at 99 Hz | Individual paired changes |
|---|---:|---|
| JSON-AS | 0.002% | +0.002%, -0.106%, +0.290% |
| Matrix multiplication | 0.365% | +0.780%, +0.199%, +0.365% |
| SHA-256 | 0.564% | +0.600%, +0.564%, -1.011% |

These are changes in elapsed nanoseconds per validated catalog iteration, not
changes inferred from sampled function percentages. Negative pairs and variation
show measurement noise; no speedup is claimed. The medians fall below the proposed
3% target for these workloads and this setup. Other host activity was not
excluded, so these local results do not replace a controlled-runner release gate
or establish a universal overhead bound. The run manifest is retained as
`/tmp/wago-perf-native-20260928/overhead-v2.json` on the qualification host; its
binary SHA-256 is
`bec71bfdf177cdf7127df10231258bf6d3f294c65178776fc26975382283d74b`.

On macOS/arm64, Samply 0.13.1 and Go 1.22.12 were measured in three alternating
pairs per workload/rate, with ten-second execute phases and 50 warmup iterations.
The metric is elapsed nanoseconds per completed, validated catalog iteration,
comparing the same profiling binary with `none` versus `samply`. All 36 captures
completed without reported metadata loss; every sampled capture resolved guest
leaf observations. Samply observes all process phases, while this overhead metric
uses the harness's execute interval.

| Workload | Median overhead at 499 Hz | Median overhead at 99 Hz |
|---|---:|---:|
| JSON-AS | 1.394% | 0.248% |
| Matrix multiply | 1.050% | 0.297% |
| SHA-256 | 1.025% | 0.219% |

One JSON-AS 499 Hz pair measured 7.202%; the other two measured 1.081% and 1.394%.
At 99 Hz, individual pairs across these workloads ranged from 0.074% to 0.904%.
The native default is now 99 Hz, with explicit `--rate` override. These local
results support that default; they do not promise ≤3% for every platform,
workload, optional metadata mode, or boundary timeline. They do not convert
Samply observations into CPU time or establish stack unwinding.

## Unwind export foundation

The low-level `JITDump.WriteUnwind` API now emits frame metadata bound to the
next exact code region. Portable/race and native AMD64 tests cover record layout,
mapping-generation/address/size/time binding, malformed framing, mapped-size overflow, orphaned
records, and sticky output errors. The maintained
[qualification fixture](../profile/testdata/unwindprobe/README.md) executes a
frameless native recursive function on a private stack. It is deliberately
separate from Railshot and does not enable stack capture in `wagoprof`.

The perf 7.0.14/libdw experiment found two integration requirements. Its ELF
writer consumes `.eh_frame` before `.eh_frame_hdr`; the jitdump prose describes
the opposite order. Its JIT module-base workaround also matches only paths
beginning `/tmp/jitted-`. With that prefix, both offline-only and mapped tables
recovered recursive callers. Under `/output/...`, the identical data symbolized
leaves but produced empty call chains. The final exporter API run had 195 samples
with nine recursive frames each. The maintained repository fixture then recorded
191 nine-frame samples and one non-fixture sample; a control without unwind records had 191
samples with one frame each. No runtime unwind mapping is necessary for the
qualified offline/libdw path. Other unwind backends are not established by this
experiment. Source links and reproduction commands are in the fixture README.

Opt-in compiler rules now cover AMD64 fixed-frame bodies with no calls or
supported direct-local call lowerings. A deterministic native matrix checks each
byte's CFA against actual frame adjustments across both call forms,
compact/uncompact layout, serial/parallel compilation, imported function offsets,
and removed/nonremoved adapters. Five complete runs passed. The same tests
compare generated bytes with metadata disabled, cover direct recursion, and
reject host-call bodies. Journal tests check copying, bounded retention, opt-in
behavior, sparse gaps, and malformed ranges; capture tests distinguish metadata
availability from qualified stacks and preserve unknown artifact reloads.

The exporter now converts those rows into offline DWARF records bound to each
exact code region. Unit tests interpret the resulting CFI and search table,
including unknown gaps, all advance widths, signed offsets, region relocation,
and unsupported/overflow cases. General-purpose and vector registers other than
the caller's SP and return PC are explicitly unrecoverable. A Go 1.26.5 native
row-encoder fixture recovered nine recursive frames in 186 samples, with one
sample outside the fixture. The initial JSON-AS perf capture passed injection and `readelf` frame decoding.
After direct-call admission, a fresh JSON-AS capture completed 30,092 validated
iterations, retained 57 compiler rows, emitted 19 unwind records, and passed
perf injection without metadata loss. These workload runs use flat sampling;
they do not qualify JSON-AS call chains.

The [Railshot recursion fixture](../profile/testdata/wasmunwind/README.md) then
qualified actual compiler-generated direct recursion with Go 1.26.5 and perf
7.0.14/libdw. Register ABI recovered nine guest frames in all 196 samples;
wrapper ABI recovered nine in 195 samples, with one outside guest code. Mixed
I32/F64 register calls recovered nine in all 194 samples. Their metadata-free
controls recovered at most one guest frame. All use the normal
public API and runtime transitions, with inlining disabled. These sampling
results qualify the tested direct-recursion paths, not all emitted code.

Entry-adapter rules now track saved result pointers, shared target-delta thunks,
and shared return tails. Independent tests walk final instructions and branches
and compare their stack changes to every recorded CFA; five repetitions passed
on Linux/AMD64. A subsequent ordinary register-ABI capture completed 87,925
checked invocations: 193 samples recovered nine guest bodies plus the adapter,
and one sample was outside guest code. The
[compacted-adapter fixture](../profile/testdata/adapterunwind/README.md) then
qualified hot-body recovery through legacy and target-delta shared adapters
(195 and 193 samples), and through the retained entries with shared return tails
(196 samples). Controls stopped at the guest body. All six runs checked both
returned values and reported no metadata loss. Brief thunk and return-tail
instructions remain covered by deterministic tests, not by exhaustive sampling.

The CLI now offers explicit bounded raw-stack collection on Linux/AMD64 with
`--stack-bytes`, requiring `--unwind-maps --include-code`. Captures retain the JIT
ELF/dump tree under `symbols` and use perf's `--symfs` after relocation; the
private discovery directory is removed. The final two-second CLI capture completed
89,941 checked recursive invocations and retained 196 flat observations. After
copying the bundle to a different path, perf recovered nine guest frames and the
adapter in 193 samples; three samples were outside guest code. The original temporary
directory was absent. This qualifies portable JIT-symbol lookup for the tested
perf/libdw implementation, not host DSO symbols or every supported viewer.
Protocol tests separately check flags, bounded configuration, file preservation
after injection failure, cleanup, flat report conversion, and assembly annotation
against the relocated symbol tree. The Linux capture tests passed three repetitions;
the command/capture suites also passed the race detector on Darwin/ARM64. Raw stack contents
are explicitly opt-in and the manifest records the configured maximum read size.
Both byte-limit boundaries also passed real one-second captures. With 256 bytes,
98 samples recovered nine guest bodies but stopped before the adapter. With
65,528 bytes, 97 samples recovered the bodies and adapter, with one sample outside
guest code. These runs demonstrate bounded-read truncation; they do not qualify
every call chain or establish an overhead budget. Assembly annotation reopened
the relocated bundle without relying on its original JIT directory. The current
ordinary-build DCE audit passes all 24 platform/build combinations.

Compiler sites now join sampled native PCs to explicit emitted operand spills,
materialized reloads, and linear-memory bounds branches. Native instruction tests
check GP, floating-point, and vector stack stores/loads on ARM64 and AMD64,
including AMD64 VEX encodings. The final-layout matrix verifies bounds-branch
bytes and Wasm origins through compaction, serial/parallel compilation, and
adapter retention/removal. AMD64 profiling tests passed three repetitions.
Journal and resolver tests cover opt-in retention, copied history, byte budgets,
unknown gaps, region ownership, rollback, and deleted sites; shared/report/capture
suites pass the race detector on Darwin/ARM64.

A real two-second Linux/AMD64 JSON-AS capture completed 31,730 checked iterations
without metadata loss. It retained 574 bounds-branch sites, 100 GP reload sites,
and eight GP spill sites. Two samples landed on recorded reloads and two on
bounds branches. JSON annotations retained the sites, and `go tool pprof -tags`
decoded their `compiler_site_kind` labels. These are sampled locations and static
emission facts, not dynamically counted reloads/checks or proof of an optimization
speedup. Ordinary-build symbol audits include the new recorder/remapper and pass
all 24 variants.

Recovery rules for host/indirect/tail calls, inlining, GC/EH/plugin paths,
ARM64, and mixed ABI transition qualification remain required before Wago can
advertise complete guest stacks. The manifest's guest-stack capability remains
false. Other signature shapes and paths still require qualification before
broader stack claims.
Ten additional parent-traced ordinary captures passed during this
investigation; they do not establish that the earlier intermittent startup
failure is fixed. The expanded ordinary-build audit, now also rejecting every
retained profiling-library implementation symbol, still passes all 24 targets.

The audit now also exercises an embedding application's explicit calls to the
unavailable profiling API. Default public sessions and span tokens have zero
state and no-op methods; they cannot pull in journal or clock implementations.
The native probe reports the required-build-tag error, and all 24 unstripped
builds pass the stronger check rejecting any executable profiling-library or
internal-journal symbol. Diagnostic builds retain the working session API.

## Artifact reload

`--phase=reload` isolates trusted-artifact decoding from compile and
`artifact-prepare`; `--reload-artifact` also supports executing the loaded image.
The harness serializes only the module compiled in the same run, records the
artifact SHA-256 and byte size, and closes the preparatory module before loading.
Public/prepared and reload/execute/all mode fixtures pass five repetitions on
native Linux/AMD64 and five under the Darwin/ARM64 race detector. Fixtures check
phase order, exact results, load/retire pairing, copied code, and the absence of
invented compiler/source metadata. A real JSON-AS `pprof` reload capture completed
with a 78,124-byte artifact and validated the workload. Its short decode interval
is not evidence of statistically meaningful CPU sampling. The Linux perf reload
phase also completed with its control handshake and no reported metadata loss.
Phase elapsed times
and execution duration use monotonic time; Unix timestamps remain for correlation.

## Metadata, ownership, and safety

Deterministic fixtures cover duplicate names, stripped names, imported-function
index offsets, shared/discontiguous regions, exact boundaries, thunks, retirement,
address reuse, snapshot/cursor ordering, and explicit exporter/capacity failures.
Late-attachment tests pass ten repetitions under the Darwin/ARM64 race detector
and ten on native Linux/AMD64, including concurrent private-thunk creation and
retirement, copied code survival, and logically closed modules retained by live
instances. Preexisting images mark first observation and do not claim metadata
that was unavailable at compilation. Live boundary-trace attachment is rejected.

Both Railshot backends retain final code identity with metadata on and off.
Compacted/uncompacted and serial/parallel fixtures verify source relocation,
rollback, deferred scalar loads/arithmetic, calls/control flow, imported indexes,
and inline ancestry. A JSON-AS capture retained 3,628 principal-origin ranges
covering 56,240 native bytes. Fused operations have one principal origin;
synthetic/unrecorded instructions remain unknown. Pprof preserves static inline
lines within a native location without inventing sampled caller frames or source
line numbers. Standard-viewer tests pass on native ARM64 and AMD64.

Timeline tests cover named/resolved and typed calls, prepared/reserved sessions,
re-exported callbacks, A-to-B-to-A ancestry, internal helpers, and inclusive versus
exclusive accounting. Initialization nests beneath instantiation; logical close
and eventual physical release remain distinct. Tracing does not change lease or
rooting ownership. Retention loss fails capture quality and suppresses exclusive
duration estimates.

Darwin cancellation stress exposed an unsafe context-pointer read during native
entry/exit transitions. The interrupt path now validates the candidate trap
pointer through a checked kernel copy and pins thread enumeration to its OS
thread. Invalid, unmapped, mismatched, and valid-context tests pass under the race
detector. Concurrent invoke/global/close stress passes 100 ordinary-build and 50
profiling-build repetitions. This fixes the observed invalid read; it does not
qualify asynchronous stack reconstruction.

## Reproduction

```sh
scripts/check-diagnostic-dce.sh
go test ./...
go test -tags=wago_codegenstats ./src/wago ./src/core/compiler/backend/railshot/arm64
go test -tags=wago_profile ./internal/jitprofile ./profile ./src/wago ./internal/genfacade
go test -tags=wago_profile ./internal/profcapture ./internal/profilecmd ./cli/internal/profiling
# Use the amd64 backend package on native amd64.
go test -tags=wago_profile ./src/core/compiler/backend/railshot/arm64
go test -tags=wago_profile -race ./internal/jitprofile ./profile ./src/wago \
  -run 'Test(CodeProfile|Journal|SnapshotPublication|BoundedJournal|JITDump|Export|Temporal|ParsePerf|Samply)'
(cd bench && GOWORK=off go test -tags=wago_profile ./cmd/wagoprof ./cmd/jsonprof)
GOTOOLCHAIN=go1.22.12 CARD_BASELINE_REF=HEAD \
  SIZE_REPORT=/tmp/wago-profile-size/size.md scripts/size-card.sh
scripts/build-profiler.sh /tmp/wagoprof
/tmp/wagoprof record --backend=perf --include-code --source-maps \
  --workload=json-as --duration=5s --warmup=50 --rate=99 --out=/tmp/json-perf.wagoprof
/tmp/wagoprof top /tmp/json-perf.wagoprof
/tmp/wagoprof annotate --function=deserializeN /tmp/json-perf.wagoprof
perf report --stdio -i /tmp/json-perf.wagoprof/perf.jit.data
go tool pprof -top /tmp/json-perf.wagoprof/native.pprof
```

Use an appropriately configured Linux environment for perf and GNU strip for the
CI-equivalent TinyGo size measurement. Ordinary CI does not require real sampling
or exact sample percentages. Complete native unwinding, source-file lines, other
collector/platform combinations, and repeatable Linux overhead remain open
qualification work.
