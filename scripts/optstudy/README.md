# Optimization campaign

Goal: compile at least 30% faster, use at least 30% less physical memory, and
execute no slower across a broad pinned wasm.fyi suite. Allocation traffic is a
separate diagnostic; it does not satisfy the physical-memory gate.

Worktree: `/Users/work/Code/Wago/wago-optimization-ablation`, branch
`perf/general-optimization-ablation`, forked from freshly fetched `origin/main`
`fc6af651c`. The original checkout and its uncommitted changes are untouched.

Latest qualified source: **remove unused loop-memory hint work**, following
removal of seven weak ARM64 policies. Fresh Darwin ARM64 Go 1.27.1 ratios against
starting main: compilation **0.714**, peak RSS **0.840**, allocation traffic
**0.202**, native bytes **0.924**. Ballpark: 29% faster / 16% lower total peak RSS.
These are one alternating pair per input under competing load, not precision
results. Neither requested 30% gate is established. Goal remains ACTIVE.

All 20 default native images match prior qualified Go images. All 20 corpus
pipelines and 2,116 complete valid off/on pairs pass across 93 selections.
Backend/wasm/public tests, profiling and register checks pass. Linux ARM64,
AMD64 and RISC-V runners and the precompiled runtime cross-build. Shared and
optimization sources/tests are unchanged from their preceding qualified pass.

The matching TinyGo 0.42.0 threads/Boehm speed profile compiled all 20 inputs,
with native hashes matching previous TinyGo images: compile **0.668**, peak RSS
**0.800**, allocated bytes **0.424**, native bytes **0.913** against matching
original main. This supplemental compiler profile has no new selected-20
execution matrix; it does not replace ordinary-Go acceptance. It also misses
the 30% physical-memory target. Continue from the latest checkpoint below.

## Run or resume

From this worktree on macOS or Linux:

```sh
go build -o /tmp/wago-optstudy-arm64-fc6af651c ./scripts/optstudy
python3 scripts/optstudy/run.py \
  --binary /tmp/wago-optstudy-arm64-fc6af651c \
  --root /Users/work/Code/Web/wasm.fyi/corpora/applications \
  --out .validation/optstudy-arm64-main --samples 3
```

Rerun the same command to resume: every completed sample is flushed and renamed
atomically, and existing samples are skipped. Failed samples remain visible;
they are not passes. The process lock prevents two writers to one campaign.
Binary, corpus, source revision, host, and execution repetition settings must
match the saved manifest. A changed binary needs a new output directory.

The manifest freezes the first workload by ID in each of the 25 application
categories before measurement. This is the initial screen, not the entire final
qualification suite: add independent upstream modules, larger modules, SIMD,
host imports, and native AMD64 before making general removal decisions.

Each optimization is forced off and on against the default selection. Order
alternates across workloads and samples. Each sample uses a fresh process,
`GOMAXPROCS=1`, one function worker, and the default bounds policy. Compile time
excludes input read, config construction, explicit GC, and oracle setup.
Peak RSS includes process startup and input residency. Warm execution includes
the prepared `WasmFunc.Invoke` call and exact oracle comparison; every invocation
must match. Native code hashes identify which switches change emitted code.

The host has competing background load. Timings are screening evidence until
qualified with isolated alternating baseline/candidate measurements. No shared
benchmark-lock.py exists at its historically used path on this host; this
runner's lock only serializes its own output campaign.

## Recovery checkpoint (2026-10-23)

- All 25 default cases passed exact oracles on native Darwin/ARM64.
- `.validation/optstudy-main` was an initial discovery run over the combined
  architecture catalog. It includes rejected AMD64-only options; do not use
  that run for architecture coverage claims.
- The corrected `.validation/optstudy-arm64-main` completed all 15,225 samples:
  15,150 exact-oracle passes and 75 required-stack-fence rejections. It uses the
  101-entry architecture-specific catalog, 25 cases, and three fresh processes
  per selection. A second identical run completed zero new samples, verifying
  resume behavior. The process is terminal, not still running.
- Summary: `.validation/optstudy-arm64-main-summary.json`. Of 100 options with
  complete on/off pairs, 60 changed no emitted code on this initial suite. This
  is insufficient evidence to remove them: the initial suite omits imports,
  threads, GC, and SIMD. Apparent execution differences without code changes
  also expose measurement noise from the busy host.
- `stack-fence=false` is rejected by the runtime's bounded-execution policy.
  Record that contract restriction explicitly; do not count it as an execution
  regression or a successful ablation.
- Neither 30% gate has been established. The initial screen changed no
  production optimization; subsequent changes are recorded below.

## Larger-module screen

`BenchmarkCompileFullOptimizationAblation` now covers every selected module and
forces every architecture-supported switch off and on, including normally
disabled switches. Invalid selections appear as skipped with their reason.
For a fast screen use `-benchtime=1x -count=1` and
`-wago.bench.optimization-ablation`; select modules with `-wago.corpus`.

The first larger suite selects json-as, json-as-simd, blake-as-simd, utf-as-simd,
zstd, yyjson, utf8proc, libtommath, nanosvg, kissfft, monocypher, drwav, fastfloat,
pcre2, cjson, lodepng, wren, tinyxml2, lua, and grain-stdlib-array. These are
available in wasm.fyi's upstream corpus and this checkout's pinned catalog.

Artifacts in `.validation/`:

- `optstudy-compile-main.txt`: original-main public compile measurements.
- `optstudy-compile-tail.txt`: rejected attempt to avoid speculative tail
  reservations; no allocation improvement, roughly unchanged time.
- `optstudy-compile-reserve45.txt`: rejected increase of the initial reservation;
  saved allocations for two modules but increased allocations on most others.
- `optstudy-compile-grow125.txt`: rejected gentler heap-buffer growth.
- `optstudy-grow125-pairs.json`: alternating fresh-process comparison of that
  growth change against a frozen `fc6af651c` baseline. All 20 native-code hashes
  matched; allocation geomean fell about 2.3%, peak RSS about 0.1%, and compile
  time rose about 1.7% on the busy host. The production change was reverted.
- `optstudy-upstream-ablation-main.txt`: full 20-module off/on compile screen.
  Completed successfully (process session 13594 is terminal). There are 4,000
  paired compilation samples over 100 switches and 20 workloads, plus default
  and stack-fence-on controls. Stack-fence-off remains rejected by config.
- `optstudy-upstream-ablation-summary.json`: equal-workload off/on ratios. Only
  one compilation per setting; timings are deliberately ballpark screening,
  as requested. Code-size equality alone does not establish code equality.

Validation: runtime and ARM64 backend package tests passed; `TestCorpus` passed
for all 20 larger modules with the growth experiment, which was then reverted.

## Current candidate: skip impossible common-exit scans

`arm64/peephole.go` now checks the necessary compare/branch/compare shape inside
the existing branch-target walk. Earlier passes neither create compare words
nor move words, so a function lacking that shape cannot acquire a valid packet.
The common-exit fold itself and all its safety checks are unchanged.

`optstudy-preflight-pairs.json` records an alternating, one-launch-per-revision
comparison against frozen original main on all 20 larger modules. All native
code hashes match exactly. Ballpark compile geomean: **8.3% faster**, allocated
bytes unchanged, peak RSS **0.5% lower** (within noise). This establishes a
promising compile improvement, not the final performance or memory gate.
The full ARM64 backend tests and exact `TestCorpus` checks passed for this
candidate. The three buffer experiments are reverted. Subsequent ownership
changes are recorded below.

Next: larger-module profiling attributes substantial memory to worker code
arenas, encoder growth, and operand-stack allocation. Metadata cloning is a
smaller contributor on these modules. Investigate those allocation owners and
extend code-hash/execution checks as further admission checks are combined.

`optstudy -raw module.wasm` reports fresh-process compile time, allocation
traffic, peak RSS, and native-code hash without executing the module. Its
`oracle=not-run-compile-only` field deliberately distinguishes this mode from
the exact-oracle application runner.

## Current candidate: code arena ownership (2026-10-23)

ARM64 parallel workers now emit into their own arena's unused tail. Encoder
growth can still detach the function, with the existing append fallback.
Deferred single-worker joining reuses that arena instead of allocating a second
image. A 32-byte source gap per function guarantees that alignment padding and
compaction never overwrite the next unread function. Multi-worker joining keeps
its existing independent output image.

Public ARM64 single-worker compilation now writes into an owned RW mapping,
which first instantiation seals RX without copying. Other architectures and
parallel compilation retain deferred publication. The existing code-cache
ownership/finalizer protocol releases this mapping; it is not a leak or a
replacement of allocation traffic with an untracked permanent buffer.

Short alternating comparisons across the same 20 upstream modules:

| Change | Baseline | Compile time | Go allocations | Peak RSS |
|---|---|---:|---:|---:|
| worker tail emission | precheck candidate | -6.5% | -10.8% | -1.2% |
| tail emission plus in-place join | original main | -12.3% | -18.5% | -1.6% |
| mapped serial publication plus precheck | original main | -7.9% | -51.5% | -3.2% |

Artifacts: `optstudy-worker-tail-pairs.json`, `optstudy-worker-join-pairs.json`,
and `optstudy-mapped-pairs.json`. Each comparison has byte-identical native code
for all 20 modules. These timings are ballpark screens on a busy host, not a
ranking based on small timing differences. The 30% physical-memory and compile
gates remain unachieved. More than 30% fewer Go allocations is not the same as
30% less physical memory.

Routing all small-function modules through the worker path regressed allocation
traffic (notably Grain). That routing experiment was reverted; its record is
`optstudy-all-worker-pairs.json`.

Validation: the complete untagged ARM64 backend package passed. The new
`TestDeferredWorkerArenaMatchesMappedSerialArm64` compares code and entries
across one, two, and four workers, ordinary/compact policies, large modules,
and mapped/deferred ownership; it also passed under the race detector and with
codegen statistics enabled. The new
`TestSerialCompileOwnsWritableImageUntilInstantiateArm64` checks mapping reuse,
serialization, sealing, live-instance retention, and final release. Focused
compiled metadata/ownership/cache tests and the 20-module exact corpus passed.

The older tagged `TestCompileWorkersDeterministicArm64` fails its resource-stat
comparison on untouched original main as well as the candidate (baseline log:
`/tmp/wago-optstudy-baseline-worker-tests.txt`). Do not describe the entire
tagged suite as passing. Direct code-and-entry comparisons are covered by the
new test independently of those worker-dependent counters.

`optstudy-mapped-applications` completed with exact-oracle passes in all 25
application categories (three checked warm repetitions per workload). Its tool
session 79511 is terminal. No benchmark process should be inferred live from
these files; poll the tool session or inspect the actual process first.

## Current candidate: gate irrelevant GC admission work

`api.go` now checks the validated required GC feature before asking whether a
module qualifies for collector-free i31 execution. Without GC instructions or
types, that proof always ended false after multiple body walks. GC modules
retain the original proof, and source identity, validation, and generated code
are unchanged.

The final candidate for this checkpoint is `/tmp/wago-optstudy-gc-gate-final`.
`optstudy-gc-gate-final-pairs.json` records binary hashes and alternating fresh
processes against original `fc6af651c` over the same 20 larger modules. Native
code hashes match in all cases. Equal-workload ballpark geometric means:

- public compilation: **22.8% faster**;
- Go allocation traffic: **51.5% lower**;
- process peak RSS: **3.8% lower**.

The full `go test ./src/wago` passed (16.4 seconds), including the GC/i31 cases.
The ownership regression now checks that executable protection is deferred;
ARM64 serial compilation reuses an owned RW mapping while other architectures
continue to map heap staging at first instantiation. The 20-module exact corpus
passed. `optstudy-gc-gate-final-applications` passed all 25 application kernels,
with three checked warm repetitions each. All processes from this checkpoint
are terminal; no benchmark is still running.

Two experiments were discarded: native single-word encoder stores were slower
in a short direct encoder check (1873 vs 1615 ns per 1024-word burst), and a
straight-line CFG preflight had no allocation benefit on the selected corpus.
Their temporary source/tests were removed. Intermediate `word32`, `nativeword`,
`gc-admission`, and `linear-proof` measurements are exploratory and must not be
reported as the current candidate. The final result above excludes both
discarded experiments.

Neither the 30% compile-time target nor the 30% physical-memory target is met.
Next work should distinguish fixed runtime residency from compiler scratch and
code/data residency, and continue reducing repeated general compile analysis.

## Current candidate: operand-node reuse and guarded-compare admission

ARM64 now has a default-on `operand-node-reuse` optimization. Popped nodes remain
alive until their consuming lowering instruction finishes, then return to the
existing free list. Nested inline lowering retires only its own suffix; caller
temporaries remain valid. Ordinary nodes remain pointer-free and 32 bytes;
the per-worker stack owner grows by one slice header. Oversized ID vectors are
discarded at function boundaries. The existing disabled path remains available
through `WithOptimization("operand-node-reuse", false)`.

The guarded-test CCMP pass now shares the mandatory branch-target walk's packet
preflight with common-exit compares. A function lacking the required TST/branch/
TST shape skips that separate scan. Earlier passes cannot create TST words or
change their positions, so this admission check retains the complete transform.

`optstudy-node-reuse-ablation.json` compares the new switch off/on over all 20
upstream modules: native code is identical, remaining allocation traffic falls
about 19.2%, compile time about 2.4%, and peak RSS about 0.7%. These are one-pair
screens, not precision claims. `optstudy-node-reuse-applications` completed all
75 default/off/on samples across 25 categories, with exact oracles checked on
every warm invocation.

The final combined candidate for this checkpoint is
`/tmp/wago-optstudy-node-reuse-final`. Its comparison with frozen original main
using the same extended raw harness is `optstudy-node-reuse-final-pairs.json`:

- public compilation: **21.9% faster**;
- Go allocation traffic: **60.8% lower**;
- process peak RSS: **4.2% lower**;
- native-code hashes: identical for all 20 modules.

Validation: full ARM64 backend and optimization catalog tests passed; the entire
ARM64 backend also passed with `wago_regalloccheck`. Focused lifetime/worker tests
passed under the race detector. Full `go test ./src/wago` passed (17.1 seconds),
and the 20-module exact corpus passed. The layout test now accounts for the
additional per-owner retirement slice; per-node size is unchanged.

`-raw module.wasm -hold 30s` prints READY after the measured compilation and holds
the compiled module alive for `vmmap` inspection. The harness now also reports
startup peak RSS and Go heap/stack diagnostics; heap reservations are not RSS.
The PCRE2 snapshots (`optstudy-vmmap-base.txt`, `optstudy-vmmap-candidate.txt`)
were collected before node reuse: physical footprint fell 8305 -> 6673 KiB and
VM_ALLOCATE resident bytes 6240 -> 4608 KiB. Snapshot footprint and process RSS
are distinct measurements. Both inspected processes were terminated after their
snapshots; none remains live.

The new `run.py --options operand-node-reuse` limits a campaign to that switch
while retaining frozen identity and restart checkpoints. Raw mode also accepts
`-option` and `-enabled`; its on/off allocation difference verifies the override
is actually consumed.

Keep bounded-signed-quotient for now: the initial application matrix changed
audio-adpcm code 488 -> 552 bytes and reduced its execution time roughly 21–32%.
Its low suite-wide selection count alone is insufficient reason to remove it.
No optimization has yet been deleted solely on noisy timing or size equality.

An asynchronous clarification asks which memory metric governs the 30% target.
Until the user answers, peak process RSS remains the gate. The compile-time and
peak-RSS gates are still unmet; do not mark the full goal complete.

## Current candidate: one validation walk, compact scratch, direct emission (2026-10-23)

The current production changes also include:

- A bounded finalizer inventory collects branch pairs and adjacent store/load
  sites during the mandatory target walk. Overflow retains the complete scan;
  tests compare inventory and full scans at 0, 1, 64, 65, and 257 candidates,
  including external entry into a load.
- Public compilation uses `DecodeModuleForValidation`, then immediately runs
  full typed validation. Container/local decoding is retained; instruction syntax
  is checked once by that validator. Ordinary standalone decoding is unchanged.
  Rejected input goes through the ordinary decoder to preserve syntax diagnostics.
  Empty expressions, missing ends, trailing instructions, malformed immediates,
  and data-count requirements have differential regression coverage. A three
  second mutator fuzz run completed 82,184 executions without a mismatch.
- ARM64 serial codegen emits directly into the owned mapped image. A proposed
  reusable small-function staging buffer was removed after the 20-module screen:
  forcing it off preserved every native hash, cut remaining compiler allocation
  about 18%, and was compile-neutral within one-shot noise. A separate native-word
  writer was also removed after longer profiling showed no reliable advantage over
  ordinary Go append. These weak candidate optimizations are not public switches.
- Operand reuse now also retires constant-folded roots, direct assignments, and
  discarded expressions. Fully consumed children retain immediate reuse. A long
  constant-folding regression stays in the initial arena; nested lowering keeps
  its caller's live temporaries.
- Control-flow stack rebuilds retire their complete linked node set, not only
  nodes popped by ordinary instructions. The detached-ID vector has an independent
  retention bound. This cut remaining allocation about 11% with unchanged hashes.
- Trap sites store an 8-byte branch/packed-source record instead of three 32-bit
  fields. The common function/PC domain is inline; uncommon wide coordinates use
  a scratch side table. Sorting, profiling, reset, and boundary tests cover both
  representations. The 20-module screen cut remaining compile time about 3% and
  allocation about 3%, with every native hash unchanged.

Relevant screening artifacts:

| Artifact | Comparison | Ballpark result |
|---|---|---|
| `optstudy-fused-decode-pairs.json` | fast decode vs original main | compile -28.1%, allocation -60.8%, RSS -4% |
| `optstudy-native-word-pairs.json` | native word store vs buffered candidate | compile -6%, allocation unchanged |
| `optstudy-native-word-buffer-ablation.json` | buffering on vs off | compile -7%, remaining allocation +17.4% |
| `optstudy-root-reuse-pairs.json` | complete root retirement vs popped roots only | compile -3.6%, remaining allocation -10.5% |
| `optstudy-root-reuse-final-pairs.json` | combined root candidate vs original main | compile -22.4%, allocation -58.8%, RSS -4.7% |
| `optstudy-exact-prefault-pairs.json` | exact destination zero-fill vs root candidate | compile -7.1%, allocation unchanged |
| `optstudy-exact-prefault-final-pairs.json` | final exact-write candidate vs original main | compile -22.2%, allocation -58.8%, RSS -4.3% |
| `optstudy-flush-retirement-pairs.json` | complete control-flow retirement vs prior candidate | compile unchanged, allocation -10.8%, RSS -0.5% |
| `optstudy-packed-traps-pairs.json` | packed trap sources vs complete retirement | compile -3.1%, allocation -2.9%, RSS unchanged |
| `/tmp/wago-direct-final-vs-main-2x.json` | direct-emission candidate vs original main, two alternating samples | compile -22.8%, allocation -70.6%, RSS -4.2% |

These are independent short screens on a busy host. Do not multiply their timing
ratios or select the best launch as the final result. Across checkpoints the
combined candidate is roughly 20–30% faster, with about 59% fewer allocated bytes;
physical process RSS remains only a few percent lower. A separate single-sample
`vmmap` peak-physical-footprint screen over all 20 modules also found only a 5.2%
geomean reduction (`optstudy-exact-prefault-footprints.json`). Every inspection
process was terminated after capture. The complete target is
still open. No repeated campaign was run just to refine precision.

The raw binary for the latest screen is `/tmp/wago-optstudy-direct-final`.
Its generated-code hashes match the previous candidate on all 20 upstream
modules. The previous candidate matches original main on all 20, so execution
instructions remain identical. `optstudy-native-word-applications` passed 125
default/off/on exact-oracle samples for both new switches across 25 categories;
`optstudy-root-reuse-applications` passed 75 for the strengthened reuse switch.
Every warm invocation was checked, rather than merely timing a module that ran.

The execution ablation now includes core exports and fresh command instances.
It measured 4,737 rows across every ARM64 switch and all selected workloads.
Default execution passed for all 20 modules, including cjson, lodepng, wren,
tinyxml2, lua, and Grain command workloads. `reg-abi=false` exposes a deterministic
Grain wrapper-ABI trap that also reproduces on untouched `fc6af651c`; it is a
pre-existing off-mode correctness defect, not attributed to this candidate.

Full runtime and ARM64 backend tests pass after strengthened root retirement;
backend register-allocation checks and focused race tests pass too. The 20-module
exact corpus passes. The final exact-write candidate's runtime check is recorded
in `optstudy-exact-prefault-runtime-tests.txt`.
The final source also cross-compiles the full public runtime tests for AMD64 and
Linux/RV64. Those are compilation checks, not native execution qualification.

Discarded during this checkpoint: destination pre-zeroing (the copy faults its
own pages), buffered small-function emission, the unsafe native word writer,
zeroing the entire mapped reservation, removing Darwin MAP_JIT, and restoring
heap publication. All are reverted from production source.

No production optimization was deleted merely for a low hit count. The full
switch sweep is complete, but uncovered imports/GC/thread cases prevent calling
code-size equality proof of universal irrelevance. Continue with general changes
that reduce actual compiler work or storage; do not tailor to module identities.

Four weak ARM64 policies were removed from the compiler and public catalog:
`loop-memory-base`, `bounded-branch-index`, `wide-reuse-pins`, and
`pure-reduce-vector`. Across the
20-module larger screen they changed no native size; across the earlier exact
25-category screen they changed no native hash; and the execution sweep showed
no suite-wide benefit. A stable 10x compile check found `loop-memory-base`
neutral, while fresh-process screens of the pruned/stripped defaults were about
1–5% faster with unchanged allocation and native hashes. `weighted-scalar-merge`
remains on: it changes pcre2 native code and its measured execution direction
favored the optimization, so compile-only pressure was not enough to remove it.

Seven other weak policies now default off while their diagnostic rollback paths
remain available: `guarded-test-ccmp`, `streaming-i32-reduction`,
`dot-loop-vector`, `crowded-products`, `linear-integer-tree`, `loop-trap-cell`,
and `prepared-light-entry`. The loop-vector entry performs one policy check per
eligible loop instead of entering four disabled recognizers.

The latest general scratch reductions are code-identical on all 20 modules:

- ordinary hint scans count local events without materializing the diagnostic
  event tape; detailed residency diagnostics retain the full tape;
- bounded host-CFG admission uses fixed compact arrays rather than heap slices;
- memory trap capacity uses the already collected memory-op ceiling, and packed
  trap sources use 12 function bits plus 19 PC bits with a wide fallback;
- exception and deferred-edge fields moved out of ordinary ARM64 control merges,
  shrinking the hot record from 96 to 72 bytes;
- call relocations pack their internal-entry flag into the bounded target word,
  shrinking each relocation from 12 to 8 bytes;
- validator control-stack watermarks use 32-bit indexes, shrinking each frame
  from 80 to 72 bytes;
- trap-source sorting is skipped when a function has no inlined foreign source.

## Current candidate: trap chains and numeric validation

Ordinary non-compact ARM64 functions with no inlined foreign source now link
memory-trap sites through the unresolved branch immediate. Each immediate is
cleared before final branch patching. This avoids a scratch record for every
memory check. Singleton groups retain their exact source PC; shared groups keep
the existing unknown-PC behavior. Profiling and compact code retain the site
table, and foreign sources or chains above the packed range convert back to it.
The differential regression compares final bytes for zero, one, two, and 257
sites, mixed conditional/unconditional branches, a foreign-source singleton,
and an oversized function. Across the 20 modules this cut remaining allocation
traffic about 7%; every native hash matched.

Core numeric validation now replaces a concrete operand-stack suffix in place,
skips proposal dispatch for complete numeric effects, and decodes immediate-free
numeric operations directly in the streaming validation loop. Polymorphic stacks
and type errors retain the original pop/subtype path. Differential tests compare
every numeric effect's result, diagnostics, and error-state stack across concrete,
mismatched, unknown, and unreachable inputs and control-boundary underflow.
Validator value slots encode the existing bottom type instead of retaining a
padded boolean, reducing each slot from 24 to 16 bytes. Tagged source ledgers and
collector-root analysis consume the same unknown-value predicate.

The combined checkpoint is `/tmp/wago-optstudy-fused-numeric`.
`.validation/optstudy-fused-numeric-checkpoint.json` records binary hashes, pinned input
hashes, and all measurements against original main. Five public compiles per
module, with revision order alternating by module, found compile time **26.4%
lower**. Fresh processes found allocated bytes **78.9% lower**, total peak RSS
**8.7% lower**, and startup-subtracted incremental peak RSS **25.3% lower**.
All 20 native-code hashes matched. These are ballpark shared-host results; the
30% compile and physical-memory gates remain open. Incremental RSS is diagnostic
and does not replace the total-process gate.

## Current candidate: remove comparison phase padding

ARM64 previously replaced instructions deleted by hot-path optimizations with
NOPs at the next loop alignment or cold function tail. This held later function
addresses stable for optimization comparisons. `omit-cold-phase-padding` now
defaults on and discards that phase debt; setting it false restores the previous
layout. Actual loop/function alignment and all branch relocation remain active.
Across all 20 modules, removing comparison padding reduced native code by 5.6%
and compile time by about 2.3% in the short on/off screen.

An alternating per-module execution check covers all 23 core/command workloads
with exact oracles. Its geometric mean was 0.6% slower, within this short screen's
noise, but individual fresh-command timings were mixed (wren +12%, tinyxml2
+25%). This establishes correctness and roughly unchanged aggregate speed, not
a claim that every workload is faster. The record is
`.validation/optstudy-no-phasepad-exec.json`.

Primitive ABI conversion now directly maps canonical i32/i64/f32/f64/v128 types,
avoiding temporary structural descriptors. Reference and noncanonical forms keep
the complete descriptor conversion. Focused ABI/type tests and the full runtime
suite pass. Native hashes match the padding-removal candidate for all 20 modules.

The latest combined measurement is
`.validation/optstudy-phasepad-primitiveabi-checkpoint.json`, with binary/input
hashes and all raw measurements. Against original `fc6af651c`, five public
compiles per module found **29.7% lower compilation time**, **78.9% lower Go
allocation traffic**, **8.5% lower total peak RSS**, **25.0% lower incremental
peak RSS**, and **5.6% smaller native code**. The raw binary is
`/tmp/wago-optstudy-primitiveabi`; the test binary is
`/tmp/wago-suite-primitiveabi.test`. The full 30% physical-memory gate remains open.

## Current candidate: avoid unused worker reservations

ARM64's bounded compactor owns three fixed 128-entry inventories. These now live
in an optional worker-owned allocation rather than consuming 4 KiB inside every
ordinary worker header. Compact compilation initializes this storage lazily and
reuses it across functions. Ordinary branch folding, identity offset validation,
and profiling retain their existing paths. The full backend suite, including
compact-code cases and register-allocation checks, passes.

The compiler's initial operand arena is now capped at 128 nodes. Larger live sets
retain the existing geometric growth and stable node IDs. This follows the earlier
operand-retirement improvements: a 256-node speculative first chunk is usually
unnecessary. A 64-node experiment saved only another 2% of allocation and was
about 6.6% slower in the short comparison; it was reverted.

The lazy-inventory screen cut remaining allocated bytes 3.6%, and the 128-node
screen cut another 3.8%, with identical native hashes for every selected module.
Records: `optstudy-lazycompact-pairs.json`, `optstudy-nodecap128-pairs.json`, and
the rejected `optstudy-nodecap64-pairs.json`, all under `.validation/`.

`.validation/optstudy-scratchcompact-checkpoint.json` binds the combined source
to binary hashes and all 20 input hashes. Against original main, the latest
five-compile alternating screen found compilation **26.4% faster**, allocation
traffic **80.4% lower**, total peak RSS **9.5% lower**, and incremental peak RSS
**26.0% lower**. Native code remains 5.6% smaller than main and byte-identical to
the preceding padding-removal candidate. Timing variation between short screens
does not justify claiming the compile gate passed.

A separate fresh-process `vmmap` inspection of the preceding primitive-ABI
checkpoint found peak physical footprint **11.6% lower** across all 20 modules
(`.validation/optstudy-current-physical-footprints.json`). PCRE2 and yyjson were
about 36% lower, while small modules changed much less. Each inspected process
was terminated after its snapshot. This metric is separate from peak RSS and
does not establish the memory gate. The latest binaries are
`/tmp/wago-optstudy-nodecap128` and `/tmp/wago-suite-scratchcompact.test`.

## Current candidate: identity maps and transient ownership

Ordinary ARM64 finalization now represents its identity map with bounds checks,
without reserving the full deletion/prefix table. Compact code keeps the complete
map in its existing lazy sidecar. Profile source/site remapping uses the same
checked identity ranges. Boundary comparisons against the old map, the backend
suite, and the profiling build pass.

Transient buffer headers now live only in the worker's persistent function
state. Resetting the function preserves that state directly instead of copying
it to and from a second set of headers in module scratch. The cleanup regression
checks this owner and its pointer-bearing buffers.

Quick alternating 20-module screens found the identity change about 4% faster
and the header change about 6% faster than their respective predecessors; all
native hashes matched. Do not multiply these timings to infer the combined
result. Records are `.validation/optstudy-identitysmall-pairs.json` and
`.validation/optstudy-one-transient-pairs.json`.

The direct main comparison is
`.validation/optstudy-one-transient-checkpoint.json`, containing binary/input
hashes and raw measurements. One compile per fresh process, alternating revision
order by module, found compile time **32.1% lower**, allocation traffic **80.7%
lower**, total peak RSS **9.2% lower**, and native code **5.6% smaller**. This is
a short screen, following the user's preference for ballpark measurements.
The 30% physical-memory target remains open. The current binaries are
`/tmp/wago-optstudy-one-transient` and `/tmp/wago-suite-one-transient.test`.

## Current candidate: omit unused residency telemetry

Ordinary ARM64 hint scanning no longer allocates one zero shadow-summary record
per eligible interval function. Detailed scans retain the full telemetry. The
regression check covers serial and parallel scans, comparing every non-telemetry
hint and sidecar with detailed scanning. All 20 native hashes match the preceding
candidate. This is a small cleanup: the 20-module screen saved under 0.1% of
allocated bytes and showed no meaningful physical-memory change. Its timing
variation is not evidence of another compile-speed improvement.
Record: `.validation/optstudy-noemptytelemetry-pairs.json`; the raw binary is
`/tmp/wago-optstudy-noemptytelemetry`. The main comparison above remains the
latest combined checkpoint, and the physical-memory target remains open.

Two broader experiments were rejected and reverted:

- Ordinary-mode logical/compact constant materialization saved 0.4% of native
  code, changed no allocated bytes, and showed no peak-RSS benefit. The short
  23-workload execution check passed exact oracles but was about 2% slower in
  aggregate, with mixed individual results. Records:
  `optstudy-compactconstants-pairs.json` and `optstudy-compactconstants-exec.json`.
- A pointer-free 12-byte compiler type representation preserved full uint32
  definition coordinates, passed runtime/backend/type tests, and emitted
  identical native hashes. It saved only 0.8% of overall allocation while growing
  the runner by about 180 KiB and showing no peak-RSS improvement. The original
  16-byte representation is restored. Record: `optstudy-type96-pairs.json`;
  `optstudy-type96-experiment.patch` preserves the rejected representation for
  reference. These evidence files live under `.validation/`.

## Refreshed full switch matrix

The post-cleanup compiler was screened once with every retained ARM64 option
forced off and on across the 20 modules (`-benchtime=1x -count=1`). The compile
matrix contains 1,960 valid pairs: all 99 options were attempted, but disabling
the mandatory stack fence is rejected. The execution matrix contains 2,253 valid
workload pairs across 23 core/command workloads. Its only execution failure is
the previously main-reproduced Grain out-of-bounds trap with `reg-abi=false`.
This is not an all-green execution matrix; the failure remains an explicit gap.

The short screen reconfirms that operand retirement/reuse cuts remaining
allocation traffic substantially (on/off about 0.53), while omitting comparison
padding cuts native code about 5.6% with roughly unchanged execution (about 1.00).
Small timing differences in this one-iteration matrix do not establish another
reason to change defaults. Records and parsed summaries:

- `.validation/optstudy-noemptytelemetry-ablation.txt`
- `.validation/optstudy-noemptytelemetry-ablation-summary.json`
- `.validation/optstudy-noemptytelemetry-exec-ablation.txt`
- `.validation/optstudy-noemptytelemetry-exec-ablation-summary.json`

The current changed/untracked sources and deletion manifest are archived in
`.validation/optstudy-noemptytelemetry-source.zip`. The worktree remains
authoritative and uncommitted. The compact-policy qualification follows below.

## Current candidate: compact-policy qualification and linked traps

`WAGO_COMPACT=1` now selects the same full policy as the explicit backend compact
option, including bounded relaxation, code alignment, compact constants, and
adapter sharing. Previously the environment flag selected finalizer shrinking
without selecting that complete policy. A regression compares the two entry
paths' native bytes and entry offsets. The public default remains ordinary.

Across 20 modules, the full policy produced about **6.6% less native code** than
ordinary compilation, with roughly unchanged compile time, **14.8% more allocated
bytes**, and **1.1% higher total peak RSS** in the quick screen. All 20 exact
corpus checks passed. The alternating 23-workload execution check passed exact
oracles and was roughly unchanged in aggregate; individual results were mixed,
including a 19% slower monocypher sample. This does not qualify a default switch.
Records: `.validation/optstudy-fullcompact-pairs.json` and
`.validation/optstudy-fullcompact-exec.json`.

Compact functions without inlined foreign sources can now use branch-linked
memory traps too. Linked groups participate in shared trap bodies and shared
unwind tails, and are patched before finalizer offset remapping. A table-vs-chain
regression covers singleton/grouped traps, conditional/zero/unconditional branch
forms, foreign-source fallback, shared-body toggles, and finalized code. Native
hashes match the previous full-compact candidate across all 20 modules. This
recovers about **7.4% of compact-mode allocated bytes**, with no demonstrated
total-RSS gain. Record: `.validation/optstudy-compactchain-pairs.json`.

Full backend/runtime checks, tagged register-allocation checks, the full profiling
backend suite, and the 20-module compact corpus checks pass. The profiling run
also exposed stale tests for default-off linear-tree selection and old comparison
padding; those were reproduced against the previous source and corrected. The
computed-index jump-table test now selects ordinary dispatch explicitly, so
compact relaxation cannot remove the operation it is meant to exercise.

Current binaries: `/tmp/wago-optstudy-compactchain` and
`/tmp/wago-suite-compactchain.test`. Source/deletion manifests are archived in
`.validation/optstudy-compactchain-source.zip`. The ordinary default's previous
main comparison remains the latest combined checkpoint. The 30% physical-memory
target is still unachieved.

## Current candidate: wrapper-call pin preservation

The Grain `reg-abi=false` failure was traced to an ABI setup ordering error.
Module-global publication uses X9 as scratch, but X9 is also an eligible local
pin. Direct and home-aware indirect wrapper calls published globals before
saving dirty local pins, so they could store a native global-cell address into
a local's home. A later local read supplied that value as a Wasm memory address.
Both call paths now home local pins before publishing globals or setting up the
wrapper arguments. Host and cross-instance paths already preserve the relevant
pins before publication.

The small runtime regression reproduces the corruption for direct, indirect,
and call_ref calls, with both eager and lazy local spills. It fails against the
saved pre-fix source and passes after the fix, with register ABI on and off.
The original Grain command also passes. Full runtime/backend suites, targeted
tagged register-allocation tests, the full profiling backend suite, and the
profiling-build regression pass. Temporary boundary probes were removed and the
hung automatic reduction process was terminated.

The new single-iteration switch-execution matrix passes all **2,254 valid pairs**
plus the **23 default workloads**. All 99 options were attempted; `stack-fence`
cannot be disabled and has no off/on pair. The previously recorded Grain failure
is fixed in this candidate. The matrix records native execution and checks exact
oracles before timing; it is a rough performance screen, not precision evidence.
Records: `.validation/optstudy-wrapperfix-exec-ablation.txt` and
`.validation/optstudy-wrapperfix-exec-ablation-summary.json`.

The latest direct-main fresh-process screen is
`.validation/optstudy-wrapperfix-checkpoint.json`: compile time **27.7% lower**,
allocated bytes **80.7% lower**, total peak RSS **8.3% lower**, and native code
**5.6% smaller**. Neither 30% gate is established. The saved pre-fix regression
failure is `.validation/optstudy-wrapperfix-regression-before.txt`.
Current binaries: `/tmp/wago-optstudy-wrapperfix` and
`/tmp/wago-suite-wrapperfix.test`; the source/deletion manifest is archived in
`.validation/optstudy-wrapperfix-source.zip`.

## Current candidate: share the encoder's word append

A before/after diagnostic caller found that compiling one small module brought
about 1.5 MiB of executable text into residency, compared with 320 KiB of extra
writable VM pages. A short CPU profile over all 20 modules attributed most
compiler samples to instruction-word emission, inlined into many emitters.
This motivated reducing duplicated compiler text rather than changing storage
semantics again. The previous unsafe/native/safe word-writer experiments remain
rejected.

The ordinary Go byte append in ARM64 `Asm.word` now has a `go:noinline` boundary.
This keeps its slice-growth and word-store sequence shared across instruction
emitters. The raw runner is about **470 KiB smaller**. In the separate diagnostic
caller, resident executable text after compilation fell from 3312 KiB to 3168
KiB. That caller's dirty physical footprint did not improve; it is distinct from
the total peak-RSS gate. Both inspected processes were terminated.

The quick alternating screen versus the preceding candidate showed about 5%
less compile time and 0.7% less peak RSS, with identical allocation and native
hashes on all 20 modules. Do not multiply that timing by an older main comparison.
The direct-main checkpoint found compile time **27.3% lower**, allocation traffic
**80.7% lower**, total peak RSS **9.8% lower**, and native code **5.6% smaller**.
This keeps both 30% gates open. Encoder/backend/runtime tests, tagged allocation
checks, the full profiling backend suite, and all 20 exact corpus checks pass.

Artifacts:

- `.validation/optstudy-wordoutlined-pairs.json`
- `.validation/optstudy-wordoutlined-checkpoint.json`
- `.validation/optstudy-wordoutlined-memoryphase.json`
- `.validation/optstudy-wordoutlined-profile.cpu` and its benchmark output
- `.validation/optstudy-wordoutlined-source.zip`

Current binaries: `/tmp/wago-optstudy-wordoutlined` and
`/tmp/wago-suite-wordoutlined.test`. The profile predates outlining and is bound
to `/tmp/wago-suite-wrapperfix.test`; the diagnostic caller is separate from the
raw resource runner. The compiler worktree remains authoritative and uncommitted.

## Current candidate: ordinary zero-test branches

The existing `zero-branch` optimization now covers ordinary `br_if` lowering,
in addition to compact mode, when the materialized i32 condition crosses an
empty reconciliation edge. It replaces CMP plus conditional branch with one
zero-test branch. The existing range fallback and non-empty-edge path remain.
Tests cover ordinary/compact policy, both branch-fold settings, the optimization
off/on, and noncanonical upper bits in public i32 carriers.

The bounded source-loop proof now decodes the shorter CBNZ Wn recipe as well as
CMP Wn,#0/B.NE. Positive alignment/width controls and negative condition, opcode,
target, observer, length, and reserved-register controls retain the proof's
semantic and coverage requirements. This restores the existing one/two-worker
source-loop coverage test for the changed native shape.

Native code shrank on 18 of 20 modules, about **0.3%** in aggregate. A quick
20-module screen showed no allocation change, about **2.6% less compile time**,
and negligible peak-RSS movement. The alternating 23-workload execution screen
passed exact oracles and was roughly unchanged overall (ratio about 0.995), with
mixed individual timings. These small timings are ballpark evidence, not a new
combined-main measurement. Records: `.validation/optstudy-ordinaryzero-pairs.json`
and `.validation/optstudy-ordinaryzero-exec.json`.

Two build/layout experiments were rejected without retaining source changes:

- Disabling compiler-package inlining shrank the runner by another 900 KiB but
  made compilation about **50% slower**, with allocation **7.8% higher** and total
  peak RSS only **1.6% lower**. Disabling remaining encoder inlining added no
  meaningful memory benefit. Record: `optstudy-package-outlining-pairs.json`.
- Grouping nine byte-identical core files under a shared filename prefix kept
  all native hashes and allocation unchanged but gave no peak-RSS improvement.
  The original filenames are restored. Record: `optstudy-corelayout-pairs.json`.

The latest direct-main resource checkpoint remains the preceding word-outlining
one. An optional clarification asks which memory measure governs the 30% target:
total peak RSS is about 10% lower, startup-subtracted incremental peak RSS about
29% lower, and allocated bytes about 81% lower. **Total peak RSS remains the gate
unless the user explicitly chooses another measure.** No gate has been changed.

Current binaries: `/tmp/wago-optstudy-ordinaryzero` and
`/tmp/wago-suite-ordinaryzero.test`. Source/deletion manifests are archived in
`.validation/optstudy-ordinaryzero-source.zip`. The worktree is authoritative.

## Rejected validator stack keys and refreshed checkpoint

A validator-only 64-bit value codec halved operand-stack entries while retaining
full public types through a lazy side table. Frontend tests passed and all 20
native hashes matched, but the quick screen saved only 0.2% of allocations and
did not lower total peak RSS (ratio 1.006). The prototype was removed in full;
the preceding ordinary-zero-branch candidate remains authoritative. Evidence:
`.validation/optstudy-stackkeys-rejected-pairs.json`.

A fresh single alternating pair against the fixed main baseline measured about
26% faster compilation, 81% fewer allocated bytes, 9.5% lower total peak RSS,
and 5.9% less native code. These are ballpark measurements; neither 30% target
has been established. Record:
`.validation/optstudy-ordinaryzero-main-checkpoint.json`. Optimization, shared
backend, ARM64 backend, frontend, and runtime package tests passed after removing
the prototype. Prior execution oracle and switch-matrix evidence still applies
to the restored candidate. Next investigate compiler executable residency and
the current emission profile; tiny stack allocation changes have little room.

## Emission profile and rejected growth split

A short current-candidate profile over the 20 modules still attributes about
62% of samples to `Asm.word`, 13% to page advice, and 11% to system calls.
This profile uses the suite's default worker policy, whereas resource checkpoints
force one worker; it is a direction-finding profile, not a gate measurement.
Disassembly confirms that ordinary byte append already becomes one 32-bit store.

Splitting rare slice growth from common emission reduced the Go frame from 96
to 32 bytes, but added bounds checks and measured only a noise-sized compile
change (ratio 0.987), with no memory improvement. A separate `nosplit` trial
measured compile ratio 1.072, also without memory improvement. All 20 native
hashes matched in both trials. Both changes were removed. Evidence is saved as
`optstudy-coldgrow-rejected-pairs.json`, `optstudy-nosplitword-rejected-pairs.json`,
and `optstudy-ordinaryzero-current-profile.cpu` in `.validation/`.

The inlining diagnostic inventory is saved in `optstudy-arm64-inline.txt`.
Its largest repeated sites include register allocation, branch patching, and
load/store wrappers. Wrapper inlining mostly passes constants to an already
shared helper, so count alone does not justify outlining. Future trials should
target duplicated work or cold error construction, and measure once before
adding a lasting boundary. The ordinary-zero candidate remains unchanged.

## Current candidate: compiler checks and ordinary function-local trap bodies

Register allocation and mandatory imm19/imm26 branch patching now keep their
small result/error checks at shared Go call boundaries. This removes about
64 KiB from the runner without changing any of the 20 native hashes. The quick
screen was slightly faster to compile, with allocation and RSS unchanged.
The boundaries retain the existing failure checks and panic/error behavior.

The existing `shared-trap-body` option now also shares complete terminal trap
bodies within ordinary functions. Each group supplies its own source function,
PC, and trap code to one global-publication/record/unwind tail. Ordinary code
still uses the established separate bodies when the option is off. Compact
policy retains its separate fallback for sharing only unwind tails. The native
code screen shrank about 1.8% beyond the preceding candidate across all 20 modules.
No hot instruction or success-path check was intentionally changed.

A further extension sharing bodies between ordinary functions saved another
small amount of code but reproducibly slowed Grain about 5%. A same-binary
100-invocation off/on control confirmed the regression; the function-local-only
control was unchanged. Between-function sharing is restored to compact-only.
Its data is retained under `optstudy-moduletraps-*`; it is not the current result.

Current evidence in `.validation/`:

- `optstudy-coreoutlined-pairs.json`: compiler boundary screen and exact hashes.
- `optstudy-ordinarytraps-pairs.json`: function-local body sharing screen.
- `optstudy-localtraps-main-checkpoint.json`: fresh original-main comparison,
  compile ratio 0.728, allocation 0.193, total peak RSS 0.906, native code 0.924.
- `optstudy-localtraps-exec.json` and `.txt`: 23 exact-oracle execution rows,
  overall timing ratio 0.981, with noisy individual improvements. Treat this as
  roughly unchanged execution, not a precise speedup claim.
- `optstudy-localtraps-grain-toggle.json`: local-only 100-invocation control,
  on/off timing ratio about 0.996.
- `optstudy-localtraps-exec-ablation.txt` and `-summary.json`: all 2,254 valid
  off/on workload pairs over 99 catalog selections pass. Stack-fence-off remains
  rejected by the bounded-execution policy.
- `optstudy-localtraps-source.zip`: changed sources, deletion list, and hashes.

Backend/runtime suites and the register-allocation/profile builds passed for
the local-sharing candidate. Expanded tests compare trap codes and precise frame
attribution with sharing off/on, exercise ordinary/compact and one/two workers,
and compare linked memory traps to the site table through finalization.
Current raw binary: `/tmp/wago-optstudy-localtraps-final`. Execution test binary:
`/tmp/wago-suite-ordinarytraps.test`. Final raw code hashes match that local-only
candidate on all 20 inputs. Both 30% gates remain open. Next work must find a
larger physical-residency reduction; further tiny scratch savings cannot close it.

## Shelved singleton-trap fallthrough experiment

Removing a singleton trap stub's branch to its immediately following instruction
reduced native code on 16/20 inputs, but only 0.015% overall. A quick screen was
about 2% faster to compile with unchanged allocation and no RSS improvement.
The 20-module exact corpus, ordinary backend/runtime, and register-allocation
checks passed. One profile-build run crashed in the raw `arm64spike` trampoline
with a split-stack overflow. The failing test name was not captured in that
original tool output. This remains unexplained, not an attributed regression.

Subsequent diagnostics passed: one profile replay, five profile-suite repetitions,
and ten GC-stressed raw-spike repetitions. The preceding candidate also passed
three corrected GC-stressed suite repetitions, excluding the two prototype-shape
tests. An earlier direct-binary baseline check used the wrong working directory
and failed relative fixture reads; its failures do not identify a code bug.

Given the tiny benefit and unresolved crash, all three experiment source files
were restored byte-for-byte from `optstudy-localtraps-source.zip`. The candidate
and binaries in the preceding section remain authoritative. The shelved source,
measurements, and diagnostic logs are in `optstudy-trapfallthrough-rejected.zip`.
Do not report the failed profile run as a pass or claim its cause is known.

The existing checkpoint's mean process peak is about 8.5 MB versus 9.5 MB on the
base, while startup is about 6.0 MB for both. Mean live Go heap after compilation
is only 0.36 MB and native output 0.21 MB. Reservations and released heap pages
are separate figures. These phase diagnostics reinforce the next priority:
reduce active compiler code residency rather than chasing tiny object savings.

## Current candidate: shared compiler load/store wrappers

The seven ARM64 scalar integer/FP load/store convenience wrappers retain shared
Go call boundaries. The existing address selection, register checks, and emitted
instructions are unchanged. This removes about 48 KiB from the raw runner and
keeps argument setup out of hundreds of lowering sites. The quick paired screen
was compile-neutral to slightly faster, with unchanged allocated bytes and about
1% lower total peak RSS. All 20 native hashes match the preceding local-trap
candidate exactly. Backend/runtime, register-allocation, and profile-build suites
passed. No new native execution benchmark was needed for identical images;
the preceding 23-workload oracles and switch matrix remain the native baseline.

A broader trial also outlined operand-value creation, pin lookup, spill-offset
calculation, zero-branch dispatch, memory-width lookup, and GP/FP ownership.
It reduced the binary further but showed no additional resource benefit. Those
seven extra boundaries were removed, with their six source files checked
byte-for-byte against the preceding checkpoint.

Evidence in `.validation/`:

- `optstudy-loadstore-outline-pairs.json`: retained wrapper screen, native
  hashes unchanged, binary size 13,095,554 to 13,046,466 bytes.
- `optstudy-shared-small-helpers-pairs.json`: broader, rejected helper trial.
- `optstudy-loadstore-main-checkpoint.json`: one alternating pair against main
  on all 20 inputs; compile ratio 0.720, allocation 0.193, total peak RSS 0.897,
  native code 0.924. Both 30% gates remain open.
- `optstudy-loadstore-source.zip`: authoritative source/deletion manifest.

Current raw binary: `/tmp/wago-optstudy-loadstore-final`. The prior
`/tmp/wago-suite-ordinarytraps.test` emits the same native images for all 20
default inputs. The source remains uncommitted and the main checkout untouched.

## Minimal-caller diagnostic and function-sentinel metadata

A separate minimal public-API caller tested the same 20 inputs once per revision,
with and without precompile forced GC. Both were built from the same source and
Go toolchain. The smaller caller showed about 12% lower candidate peak RSS in
either mode, while startup fell only about 0.4 MB versus the campaign runner.
The harness and forced GC therefore do not explain the missing 30% reduction.
This is supplementary evidence only; the original RSS gate is unchanged. Source,
binary identities, and records are in `optstudy-minimal-caller-diagnostic.zip`.

The frontend now constructs the GC descriptor table directly for function-only
recursive groups without super edges. It preserves flattened IDs, finality,
empty-group behavior, and the exact `KindFunc` sentinel representation, using one
output allocation instead of a temporary flattened resolver table. Such literals
satisfy the heap-layout and super-edge invariants by construction. Types with
super edges, structs, arrays, or unknown kinds retain the full lowering and
validation path. Differential tests cover grouped/empty declarations and finality;
super tests retain valid edges and reject self/out-of-range edges.

Public compilation also drops the former array-descriptor configuration function:
it performed no rewrite and only repeated the validation already owned by the
metadata builder. Untrusted compiled metadata keeps its independent validation.

The quick retained-change screen saved about 0.26% of remaining allocated bytes,
with unchanged native hashes on all 20 inputs and negligible RSS movement.
The fresh direct-main screen measured compile ratio 0.734, allocation 0.193,
total peak RSS 0.892, and native code 0.924. These are ballpark timings, not a
demonstration of either 30% gate. Do not multiply successive screen ratios.

Full frontend, GC-native, runtime, register-allocation, and profile suites passed.
The actual 20-module corpus passed with the new metadata path. Current artifacts:

- `optstudy-function-sentinels-pairs.json` and `-main-checkpoint.json`;
- `optstudy-function-sentinels-corpus.txt`;
- `optstudy-function-sentinels-source.zip` with source/deletion/binary hashes.

Current binaries: `/tmp/wago-optstudy-function-sentinels` and
`/tmp/wago-suite-function-sentinels.test`. Native hashes still match the preceding
candidate, so the prior native execution oracle/timing and switch-matrix records
remain the execution baseline. Both targets remain open. The next larger option
to investigate is avoiding unused GC-only metadata in modules whose validated
requirements prove that no collector metadata can be consumed; preserve public
type metadata and all reference/snapshot boundary checks when testing that idea.

## Current candidate: omit unused collector metadata

Compilation now omits the GC descriptor table when validated requirements contain
no GC, typed-function-reference, or exception feature and there are no custom
instruction bindings. Ordinary numeric/SIMD and opaque funcref/externref modules
retain their complete public function/type metadata. The standalone frontend
descriptor builder still returns its indexed function sentinels when requested.
Collector-bearing, typed-reference, exception, and custom-instruction modules
retain full descriptor construction. Their representation and validation are
unchanged. The same proof avoids the collector-signature boundary scan, and a
disabled exact-root plan is no longer entered solely to return nil.

The quick elision screen saved about 1.4% of remaining allocated bytes with all
20 native hashes unchanged; total peak RSS was essentially unchanged. A fresh
main comparison measured compile ratio 0.736, allocation 0.190, total peak RSS
0.900, and native code 0.924. Treat compilation as roughly 26-27% faster and RSS
as roughly 10% lower, not a demonstration of either 30% gate.

Full runtime/frontend/GC suites and the profile reference/GC tests passed. New
tests preserve numeric, funcref, and externref signatures and execution through
public artifact round trips, including older artifacts carrying function
sentinels. Existing invalid-descriptor validation remains in place. The actual
20-module corpus passed, and the current binary passed all 2,254 valid off/on
execution pairs across 99 catalog selections. AMD64 and RV64 test binaries
cross-build on Linux; these are build checks, not foreign-architecture runs.

Artifacts in `.validation/`:

- `optstudy-gc-metadata-elision-pairs.json`;
- `optstudy-gc-metadata-main-checkpoint.json`;
- `optstudy-gc-metadata-corpus.txt`;
- `optstudy-gc-metadata-exec-ablation.txt` and `-summary.json`;
- `optstudy-gc-metadata-source.zip` with exact source/deletion/binary hashes.

Current binaries: `/tmp/wago-optstudy-gc-metadata-final` and
`/tmp/wago-suite-gc-metadata-final.test`. No commit or push has been made.
Both targets remain open. Remaining work should pursue larger code-working-set
or pipeline changes; this structural metadata saving does not explain the
remaining gap in physical memory.

## Phase residency and cold GC matcher entry

A diagnostic caller paused after decode, validation, direct backend lowering,
and public compilation for json-as and Lua. Resident executable text increased
about 0.25 MiB through validation, another 0.86 MiB in the backend, and about
0.27-0.30 MiB in the remaining public compile path. These snapshots retain the
decoded module and backend output and repeat early stages during public Compile;
they diagnose code residency and are not resource-gate measurements. Full vmmap
captures, source, and summaries are in `optstudy-residency-experiments.zip`.

Two operand-arena experiments were rejected after one screen each. A direct
first-chunk representation kept checked indexing and stable IDs but grew the
runner about 16 KiB with no compile gain and slightly higher RSS. Sharing the
existing checked node lookup shrank the runner about 48 KiB but was slightly
slower with no RSS benefit. All 20 native hashes matched in both trials. Arena,
scratch, and diagnostic sources were restored exactly to the prior checkpoint.

ARM64 body entry now checks the necessary loop-header bytes before calling the
i31 reduction recognizer and the existing struct/array helper flags before
calling those recognizers. These exact prerequisites already existed inside
the proofs; caller checks avoid entering cold routines merely to reject an
ordinary function. The full matchers and their semantic checks remain intact.
The quick screen was about 3% faster to compile and 0.8% lower in total peak RSS,
with identical native hashes on all 20 modules. A separate split of GC-root
helpers into inline wrappers added no RSS benefit and was fully reverted.

The fresh main screen measured compile ratio 0.722, allocation 0.190, total peak
RSS 0.885, and native code 0.924. These are ballpark measurements; neither 30%
gate is met. Do not multiply successive trial ratios. Backend/runtime suites,
register-allocation and profile builds, positive and negative GC reduction
selection tests, and the 20-module corpus pass. The preceding switch matrix
remains the native execution baseline; default native hashes are unchanged.

Current artifacts in `.validation/`:

- `optstudy-gc-reduction-gates-pairs.json` and `-main-checkpoint.json`;
- `optstudy-gc-reduction-corpus.txt`;
- `optstudy-residency-experiments.zip`, including rejected trial data/source;
- `optstudy-gc-reduction-source.zip`, the exact retained source/deletion manifest.

Current binaries: `/tmp/wago-optstudy-gc-reduction-final` and
`/tmp/wago-suite-gc-reduction-gates.test`. Only the caller checks were retained
from this turn; both arena variants and the GC-root wrapper split are removed.

## Compiler declaration locality checkpoint

Ordinary ARM64 compiler paths now live in `a_core_*.go`, ahead of less frequently
used paths in Go linker source order. A temporary AST planner selected complete
function declarations using coverage from one in-process compile of each of the
20 modules. It moved 889 declarations across 65 files, preserving normalized
declarations, comments, and compiler directives exactly. Types, globals, and
initialization were not moved. Ten original files became empty and were removed.
This is a compiler source layout change, with no module-specific runtime rules.

One fresh-process pair per corpus against original main `fc6af651c` measured
compile ratio **0.655**, allocated bytes **0.190**, total process peak RSS
**0.871**, and native code bytes **0.924**. These are ballpark estimates, not
precision measurements. The compilation target is met in this sample; the
30% total peak RSS target is still unmet. All 20 native hashes match the preceding
GC reduction checkpoint, so its generated execution code is unchanged.

Backend/runtime, register-allocation, profile, and 20-corpus execution checks
pass. The switch matrix passes 2,254 on/off workload pairs across 99 selections;
the unsafe stack-fence override remains deliberately rejected. Linux AMD64 and
RV64 runtime test binaries cross-build; these are not native execution tests.

Current binaries are `/tmp/wago-optstudy-core-layout` and
`/tmp/wago-suite-core-layout.test`. The authoritative retained-source archive is
`.validation/optstudy-core-layout-source.zip`, with the source/deletion and binary
hash manifest. The corresponding `-main-checkpoint.json` contains every raw
sample. `optstudy-core-layout-planning.zip` preserves the coverage, AST planner,
original declarations, and reversible move plan. The worktree remains uncommitted.

## Public compiler locality checkpoint

The same declaration-preserving layout screen now groups 261 public compiler
helpers from 42 files into `src/wago/a_compile_*.go`. Four original files became
empty and were removed. The AST plan verifies complete normalized declarations,
comments, and compiler directives. Build constraints and filename architecture
suffixes are retained. There are no semantic or runtime matching changes.

The quick paired screen against the preceding backend-layout candidate measured
compile ratio 0.994, allocated bytes 1.000, total peak RSS 0.974, and code bytes
1.000. All 20 native images match exactly. Extending layout movement to the
decoder, validator, and frontend gave only about 0.9% lower RSS with 2% slower
compilation; all those source changes were reverted and checked byte-for-byte.

A fresh direct screen against original main measured compile ratio **0.719**,
allocation **0.190**, total process peak RSS **0.851**, and native bytes **0.924**.
The baseline itself was about 18% faster than in the preceding screen and the
candidate about 9% faster; one-pair timing estimates should not be treated as
precision results. The latest compile estimate is about 28% faster, versus the
preceding 35% estimate. Do not claim a firm 30% compile gate from those samples.
The 30% total peak RSS target remains unmet at about 15% lower. Keep the goal
active and preserve the original acceptance metrics.

A module-wide reachability screen found zero unreachable local functions in all
20 inputs, including initializer-declared reference targets and direct/tail call
edges. No dead-function transformation was added. That scan and both layout trial
plans are retained as diagnostic evidence rather than production overhead.

Current binaries are `/tmp/wago-optstudy-public-layout-final` and
`/tmp/wago-suite-public-layout.test`. Current source and raw results are recorded
in `.validation/optstudy-public-layout-source.zip` and
`optstudy-public-layout-main-checkpoint.json`; `optstudy-public-layout-planning.zip`
retains the reversible planner and rejected extension. Sources remain uncommitted.
The runtime suite and 20-corpus execution tests pass, as does the current toggle
matrix with 2,254 valid paired workload rows across 99 selections. Linux AMD64
and RV64 runtime test binaries cross-build; they were not executed on this host.
Backend register-allocation and profile checks remain qualified at the preceding
checkpoint: this change moves only public compiler declarations, and all default
native images remain identical.

## Post-layout screens: no additional retained production changes

Four follow-up trials were rejected after one fresh-process pair per module:

| Trial | Compile ratio vs public-layout candidate | Peak RSS ratio | Decision |
|---|---:|---:|---|
| One/two-byte LEB decoder paths | 0.991 | 1.002 | No material resource benefit |
| Deferred-child and opcode gates before tree matchers | 1.050 | 0.996 | Slower compilation |
| Only the default-off linear-tree caller gate | 1.067 | 0.997 | Slower compilation |
| Direct validation of scalar constants and local operations | 1.026 | 1.004 | No full-pipeline gain |

Allocation traffic and every native code hash were unchanged in all four
screens. LEB boundary/round-trip and full decoder tests passed before rejection;
the broad matcher trial passed the backend suite; scalar-leaf differential and
full decoder tests passed before rejection. Production files were restored
byte-for-byte to `optstudy-public-layout-source.zip`, and temporary tests removed.
These noisy short screens justify declining the changes, not precise regressions.

A fresh CPU profile of the retained `BenchmarkCompileFull` over all 20 modules
used one fixed 20-operation run per module. It includes Close, so a separate
diagnostic caller labels only `cfg.Compile`: that filtered profile assigns about
74% of 159 CPU samples to the encoder's `Asm.word`, almost entirely at its final
32-bit store. Mapping and Go page-release calls account for about another 16%.
This is a coarse warm caller profile, not a replacement for the cold raw gate.

A 256-byte staging diagnostic over one pair of fresh code mappings per native
module size saved only about 3% write-only time, with identical bytes. It excludes
mapping, release, and hashing time and does not establish a compiler speedup.
No buffered encoder API was added. A temporary, single-worker build overlay
counted all encoder word writes: each module emitted about 0.96–1.00 words per
final output word (bulk template/alignment copies bypass that counter). This
rules out excessive full-body retries as the explanation on these inputs. The
store-heavy profile has not yet been reproduced by the simple write diagnostic;
investigate the actual interleaved compiler/mapped-image behavior before another
writer rewrite. No instrumentation remains in production source.

All trial data, sources, profiles, diagnostics, and binary hashes are retained in
`.validation/optstudy-postlayout-experiments.zip` and its screening manifest.
The qualified candidate and main comparison remain the public-layout checkpoint:
about 28% less compile time in the latest sample, 15% lower total peak RSS, and
unchanged execution images. Both 30% targets remain open; the goal stays active.

## Smaller recursive hint frames and independent sampling

The next diagnostic found that minor faults did not explain the apparent word
store cost. After the first compile, actual compilation and a simple mapped
writer generally faulted about the output's required page count; system CPU time
was small. A macOS `/usr/bin/sample` capture of an unprofiled diagnostic caller
then disagreed sharply with Go's CPU profile. Its collapsed application leaves
included 74 scanner samples, 62 direct-op decoder samples, 59 body-loop samples,
49 peephole samples, and only 15 word-writer samples. These are coarse counts,
not precise percentages or proof of a particular Go/runtime bug. The earlier
74% Go word-writer ranking is not a reliable basis for another writer rewrite
without resolving this disagreement. Fault and independent-sample evidence are
saved with the current diagnostic archive.

The retained change extracts memory/proposal, indirect-call, and miscellaneous
immediate handling from the recursive ARM64 hint scanner. The existing
`SkipInstructionImmediateWithMemarg64` now retains its scratch record in its own
non-inlined frame. The same classifier, feature flags, event recording, call
propagation, and error paths remain. There is no new matching rule or policy.

On this Go 1.27.1 Darwin/ARM64 build, disassembly shows the recursive frame shrink
from **800 to 288 bytes (64%)**. The intermediate three-helper split used 480
bytes; shared skip scratch accounts for the final reduction. One paired raw
screen against the public-layout candidate measured compile ratio **0.983**,
allocation **1.000**, total peak RSS **0.988**, and native bytes **1.000**. Go stack
reservation fell 128 KiB for PCRE2 and 64 KiB for Wren and Grain in that screen.
Stack reservation is diagnostic evidence and is not the physical-memory gate.

The fresh direct screen against original main `fc6af651c` measured compile ratio
**0.677**, allocation **0.190**, total process peak RSS **0.850**, and native code
bytes **0.924**. All 20 native hashes match the preceding candidate. These remain
ballpark single-pair measurements: this sample clears the compile target, but
the 30% total peak RSS target remains unmet at about 15% lower. Keep the goal active.

Decoder, backend, runtime, register-allocation, and profile-build suites pass.
The 20-corpus execution run and toggle matrix pass, with 2,254 complete paired
workloads across 99 selections. Linux AMD64 and RV64 runtime test binaries
cross-build; they were not executed here. The baseline remains starting
`origin/main` at `fc6af651c`; the older local `main` ref is not used for comparison.
Current binaries are `/tmp/wago-optstudy-hint-frame-compact` and
`/tmp/wago-suite-hint-frame-compact.test`. The latest raw checkpoint and exact
source/deletion manifest are `.validation/optstudy-hint-frame-main-checkpoint.json`
and `optstudy-hint-frame-source.zip`. `optstudy-hint-frame-diagnostics.zip` retains
fault counters, independent sampling, frame disassembly, and intermediate screens.
Sources remain uncommitted; the primary checkout is preserved.

## Post-frame memory screens

Two further trials were fully reverted to the qualified hint-frame source:

- An immutable default-config selection avoided materializing the full option
  map on ARM64 and preserved captured defaults, explicit overrides, catalog
  ownership, public inspection, and constructor allocation limits. The new
  semantic tests and full optimization/backend/runtime suites passed. All 20
  native hashes matched. Its first paired screen was compile ratio 1.022 and
  RSS 0.992; the final direct-main screen was compile 0.737 and RSS 0.845. That
  small RSS saving did not justify the broader API/state complexity or the
  latest compile estimate below the target. The initial extra empty-map
  allocation failed the constructor budget; removing that map fixed the test
  before the final screen. No tests or APIs from this trial remain.
- Coallocating scratch, operand-stack header, and encoder in one typed owner
  removed two fixed allocations but saved only 176 allocated bytes per module.
  The paired screen was compile 0.990, allocation 0.998, RSS 1.001, and native
  bytes 1.000. The backend suite passed and all 20 native hashes matched. There
  was no material RSS benefit, so the extra owner type was removed.

A decoded metadata screen found no name sections in any of the 20 modules.
Repairing or redesigning private name compaction cannot improve this corpus
screen. Active data ranges from 32 bytes to about 317 KB; almost all modules have
only small payloads. No data-compression or metadata-ownership change was added.
The stack-fence toggle exception was inspected and remains deliberate: public
configuration requires the bounded-execution barrier. Its rejected off setting
is recorded as an invalid selection, not a passed off/on execution pair.

The existing raw measurements also show startup peak RSS at a geometric mean
of about 64% of original compile peak RSS, and 76% of candidate compile peak RSS.
Peaks are not additive; this is a headroom diagnostic, not attribution. It
reinforces that tiny header/metadata savings cannot close the total-RSS gap.
The illustrative per-module caps in the audit are not new acceptance gates.

Artifacts: `.validation/optstudy-postframe-memory-screens.zip` and
`optstudy-memory-headroom-audit.json`. The retained production source is still
`optstudy-hint-frame-source.zip`, unchanged byte-for-byte before this documentation
update. Its latest qualified ratios remain compile **0.677**, Go allocation
**0.190**, total peak RSS **0.850**, native bytes **0.924**. The goal stays active.
A user clarification about whether the memory target means total peak RSS or
Go allocation traffic is pending. Keep total peak RSS as the target unless the
user explicitly chooses a different metric; silence is not a change of scope.

## Shared index-opcode decoder checkpoint

The validator now uses one common decode arm for the 17 opcodes with one index
immediate. Validation and immediate scanning share a 256-entry opcode-kind
lookup, replacing repeated instruction construction and a second mapping switch.
This is a format-wide change; no corpus names, sizes, or patterns are consulted.
The runner is 16,416 bytes smaller. In one alternating pair per corpus against
the preceding hint-frame runner, compile ratio was **0.972**, total peak RSS
**0.990**, allocations **1.000**, and native bytes **1.000**. All 20 native code
hashes matched. These are ballpark screens under background host load.

A fresh comparison directly against starting origin/main `fc6af651c` gave compile
**0.722**, Go allocation **0.190**, total peak RSS **0.854**, and native bytes
**0.924**. Do not multiply successive isolated ratios. The compile estimate is
near the requested threshold and varies between screens; the 30% total-RSS
reduction remains unmet. The existing peak-RSS target and pending metric question
remain unchanged. No additional benchmark repetitions were taken for precision.

Validator, ARM64 backend, public runtime, and all 20 corpus tests passed. The
full one-execution optimization matrix passed 2,254 complete off/on workload
pairs across 99 selections. Stack-fence off remains deliberately invalid at the
public API. Linux AMD64 and RISC-V test binaries cross-built; they were not run.
The prior regalloc/profile qualification is unchanged because backend code and
all emitted corpus images are unchanged; those tagged suites were not rerun.

A separate tree-backed validator outlining trial shrank its hot stack frame
from 944 to 608 bytes and passed validator tests. Its isolated compile ratio was
0.995 and RSS 1.003 with identical allocations/native output. It was fully
reverted because it did not improve the requested memory metric.

Current reviewable source, binary hashes, exact paired results, and validation
are saved in `.validation/optstudy-index-decode-source.zip`,
`optstudy-index-decode-main-checkpoint.json`, and the matching test/matrix files.
`optstudy-index-decode-diagnostics.zip` includes the rejected tree trial. The
qualified source differs from the hint-frame checkpoint only in this decoder
change and checkpoint documentation. Sources remain uncommitted in the isolated
worktree; the primary checkout is preserved. The goal stays active.

## Post-index working-set screens

The shared-index-decoder checkpoint remains the retained production source.
Two further general trials passed the backend suite and produced identical
native hashes and sizes on all 20 modules, but were fully reverted:

| Trial | Compile ratio | Go allocation ratio | Total peak RSS ratio | Decision |
| --- | ---: | ---: | ---: | --- |
| Table recipes for 65 ordinary integer opcode dispatch arms | 1.071 | 1.000 | 1.006 | Slower; runner grew 96 bytes |
| Explicit compact parent state for plain blocks and try_table, preserving loop/if recursion | 1.050 | 1.009 | 0.994 | Added allocation and compile cost for less than 1% RSS gain |

Ratios compare one alternating fresh-process pair per corpus against the
qualified shared-index runner. The flat-block trial reduced Go stack reservation
by 64 KiB on yyjson, pcre2, and lua. Reservation is not physical RSS; the measured
RSS result above is the acceptance evidence. Neither prototype remains in source.
No precision repetitions or new main comparison were run after these reverts.

Current retained raw samples have less than 1 MiB live Go heap per module while
process peak RSS is roughly 7–9 MiB. This does not attribute the peak or make
heap and RSS interchangeable. It does make another tiny buffer/header change an
unlikely path to the remaining gap. The previous allocation profile puts metadata
snapshot construction ahead of most remaining individual allocation sites, but
snapshot/public ownership and mutation isolation must remain correct.

The existing validator summary is fixed flags plus body size. It cannot directly
replace ARM64 hint scanning: ordered local recurrence facts, branch/loop-weighted
local and global hotness, interval last-use/event facts, and loop eligibility
are still missing. Skipping that scan on the current flags would be unsound.
A future fused analysis must provide those facts rather than infer their absence.
There were no MethodByName/Method calls found in production src/wago or src/core
that would explain broad reflection-driven method retention; that hypothesis
has no supporting evidence here.

Artifacts: `.validation/optstudy-postindex-screens.zip` and
`optstudy-postindex-screening.json`. All retained source/deletion hashes were
verified against `optstudy-index-decode-source.zip` before this documentation
update. Its qualified total ratios remain compile 0.722, Go allocation 0.190,
peak RSS 0.854, and native bytes 0.924. The goal remains active; the 30% peak-RSS
target is unmet and the user metric clarification remains pending.

## Shared cold decoder errors checkpoint

Two-field decoder errors now use one private, non-inlined constructor. Bounds,
LEB, syntax, and classification readers keep the same error code, offset, and
zero-valued section/cause fields. Existing section-context attachment is
unchanged. An independent AST normalization check reversed all 96 replacements
across 12 files and proved that the remaining executable AST was identical.
This is format-wide cold-path sharing, with no workload recognition or altered
validation policy. It saves **98,032 runner bytes** against the shared-index
checkpoint while all 20 native code hashes and allocated-byte totals remain equal.

One alternating pair per module against that preceding runner gave compile
ratio **1.008**, allocated bytes **1.000**, total peak RSS **0.995**, native bytes
**1.000**. The compile/RSS changes are small screening estimates; runner size is
an exact build observation. The final formatted runner was then compared once
directly with starting origin/main `fc6af651c`: compile **0.749**, Go allocation
**0.190**, total peak RSS **0.841**, native bytes **0.924**. The latest compile
screen is about 25% faster and remains below the requested 30% improvement;
previous screens varied between about 28% and 32%. Do not multiply screen
ratios or promote an earlier timing sample to proof of the current gate. The
30% total peak-RSS target also remains unmet. No precision repetitions were run.

Decoder, ARM64 backend, and public-runtime suites passed, along with the 20
corpus tests and all 2,254 complete optimization off/on workload pairs across
99 selections. Stack-fence off is still deliberately invalid. Linux AMD64 and
RISC-V runtime test binaries cross-built but were not executed. Backend source
is unchanged, so the prior profile/regalloc validation and identical native
images are retained evidence; those tagged suites were not rerun here.

A preceding trial outlining only the two validation-error constructors saved
32,576 runner bytes but measured compile ratio 1.072 and RSS 0.996. That trial
was fully reverted before this one; no validation-error helper change remains.
A read-only signature audit also ruled out signature interning as a useful
memory target on this suite: the largest function-signature record table is
21,952 bytes and the largest signature-byte payload is 1,410 bytes. Public
mutation isolation was preserved; no signature representation was changed.

Current source/deletion manifest and binary hashes:
`.validation/optstudy-cold-decode-errors-source.zip`. Exact main results and
qualification: `optstudy-cold-decode-errors-main-checkpoint.json`, its test and
matrix files, and `optstudy-cold-decode-errors-diagnostics.zip`. The latter holds
the AST proof, metadata audit, rejected validation-error prototype, and scripts.
Sources remain uncommitted in the isolated worktree. The primary checkout is
preserved, and the goal remains active with the peak-RSS metric unchanged.

## Branch immediate decoding checkpoint

The existing branch decoder now uses one B/BL mask and arithmetic sign extension
for its immediate fields. Plain-B decoding still rejects BL, imm19 still returns
word displacement, and final targets retain byte offsets. No instruction pattern
or safety condition was added or removed. New boundary tests cover minimum,
maximum, negative, zero, and positive offsets for B, BL, B.cond, CBZ/CBNZ, and
TBZ/TBNZ, including register bits and non-branch words. An initial TBZ extraction
mistake leaked register bits into the byte offset; the new tests and existing
backend suite caught it. The fix passed before any candidate measurement.

Against the preceding cold-error checkpoint, one alternating pair per module
measured compile **0.988**, allocation **1.000**, total peak RSS **0.997**, and
native bytes **1.000**. Median compile ratio was 1.006; the compile difference is
small screening evidence, not a demonstrated universal speedup. All 20 native
hashes match. The executable instruction section shrank 96 bytes while the
__TEXT mapping size remained equal. The runner file grew 16,512 bytes; its file
size is not evidence of greater resident memory. The Mach-O audit is archived.

The fresh direct-main screen is compile **0.729**, allocated bytes **0.190**,
total peak RSS **0.847**, native bytes **0.924**. Neither requested 30% gate is
established by this result. Baseline remains starting origin/main `fc6af651c`.
Each image now receives one matched untimed compile of the first corpus before
one fresh-process alternating measured pair per corpus. This controls the
obvious first-use asymmetry without claiming to control the OS cache. The warm
records are preserved separately; measured RSS remains cumulative process peak,
including startup. No precision repetitions or peak subtraction were used.

This adjustment followed a separate necessary-branch-class precheck trial.
Its first new-image json-as sample was 3.47 times baseline; the other pairs had
a median near 1.000. One reversed-order follow-up of that specific pair was
0.981. The original geometric mean (1.056) and both observations are retained;
the outlier was not silently replaced or declared a proven compiler regression.
No cause for the first-use anomaly is established. The standalone precheck was
removed before the signed-decoder change. Earlier isolated new-image screens
can have similar first-sample sensitivity; do not rewrite their raw evidence or
call the changed measurement policy a compiler optimization.

Initialization tracing also showed 26,408 allocated bytes in the ARM64 backend
initializer and zero heap allocation in the Wasm initializer. These are traced
Go allocation counts, not physical-memory attribution; no eager-table/lazy-init
production change was made on that evidence.

Backend, public-runtime, regalloc-check, and profiling suites passed. All 20
corpus checks and 2,254 complete off/on workload pairs across 99 selections
passed. Stack-fence off remains deliberately invalid. Linux ARM64 cross-built;
AMD64/RISC-V source is unchanged and its prior cross-build qualification is
retained, not rerun. No Linux binary was executed.

Reviewable current source/deletion/binary manifest:
`.validation/optstudy-branch-immediates-source.zip`. Qualification and exact
paired results: `optstudy-branch-immediates-main-checkpoint.json` and matching
test/matrix files. `optstudy-branch-immediates-diagnostics.zip` preserves init
tracing, the unretained class precheck, first-sample audit, initial failing test,
Mach-O audit, and measurement scripts. Sources remain uncommitted in the isolated
worktree, the primary checkout is preserved, and the goal remains active.

## Pipeline cost and minimum-memory audit

The retained production code still matches the signed-branch checkpoint. Two
further general trials passed the backend suite with all 20 native hashes equal,
but were fully reverted after one warmed-image, alternating fresh-process pair
per corpus:

| Trial | Compile ratio | Go allocation ratio | Total peak RSS ratio |
| --- | ---: | ---: | ---: |
| Necessary bit-28 guard over the complete peephole preflight | 0.996 | 1.000 | 1.000 |
| Initialize operand storage at arena allocation instead of clearing then overwriting | 1.002 | 1.000 | 0.999 |

Neither result establishes a material resource benefit. No temporary production
API, guard, arena change, timer, or observation hook remains.

Temporary Go build overlays timed the actual public compilation phases without
turning on full diagnostic statistics. One shared-process pass over the 20
modules gave aggregate phase shares of roughly **58% body lowering, 15%
validation, 9% hint analysis, 7% peepholes, 7% function setup/tail**, with the
remaining time in module setup, decoding, metadata, and publication. Body,
peephole, and function subphases overlap their parent totals; do not add parent
and child fractions. These are instrumented aggregate-time diagnostics, not
equal-corpus baseline/candidate gates. Clock reads, shared code/cache state, and
deferred printing perturb timing. The overlay sources and raw records are saved.

A strict pure-function summary shortcut (no control, call, memory, global,
reference, or SIMD facts; at most 64 locals) covers only 780 body bytes across
27 functions in the frozen suite, less than 0.1% of its body bytes. This is a
conservative subset audit, not a claim about all possible linear fast paths.
A narrow summary shortcut is unlikely to repay a new retained representation;
full hint/validation fusion would need ordered accesses and control-weight facts.

A separate one-pair diagnostic of minimal modules measured candidate total peak
RSS of **6,384 KiB** for no functions, **6,832 KiB** for one empty function, and
**6,896 KiB** for an integer add. The original frozen-corpus peak-RSS geometric
mean is about 9,158 KiB, whose 70% target is about 6,411 KiB. The minimal modules
are not acceptance workloads or a mathematical lower bound, and these peaks
must not be subtracted to attribute memory. They indicate that common pipeline
costs need attention in addition to work proportional to body size.

An exact public-Compile diagnostic then paused one empty-function compile at
phase boundaries. vmmap observed aggregate process __TEXT residency from 7,968
to 8,832 KiB, VM_ALLOCATE residency from 1,376 to 1,424 KiB, and physical footprint
from 3,457 to 3,505 KiB. The largest observed __TEXT change was across backend
compilation. __TEXT includes shared libraries; this is not ownership attribution,
and physical footprint is not the raw peak-RSS gate. Pauses and diagnostic I/O
also change working state. The child completed normally and all vmmap snapshots
are archived. Compiler frame disassembly showed 2,320 bytes for the public
compile frame, 5,792 for compileFuncAttempt, and 1,328 for finalizeNativeCode;
there is no newly established enormous single setup frame to remove.

The evidence shifts the memory investigation toward common compiler code and
metadata residency rather than more tiny payload/header packing. Any code-layout
trial should group general compiler pipeline helpers, preserve all declarations,
initialization, imports, directives, and native output, and validate against the
full frozen suite. Do not tune a path to individual module names or alter the
acceptance corpus. Full hint/validation fusion remains a possible compile-time
route, but the current summary cannot justify dropping the exact hint scanner.

Artifacts: `.validation/optstudy-pipeline-audit.json` and
`optstudy-pipeline-audit-diagnostics.zip`. Retained resource qualification remains
compile 0.729, Go allocation 0.190, total peak RSS 0.847, native bytes 0.924 from
`optstudy-branch-immediates-main-checkpoint.json`. All retained source/deletion
hashes matched its source archive before this documentation update. The goal is
active; neither 30% gate is established by the latest qualified screen, and the
pending user memory-metric clarification has not changed the peak-RSS target.

## Rejected pipeline layouts and flag-read representations

Four further general screens were reverted in full. Each used one matched
first-input warmup per image and one alternating fresh-process pair per frozen
corpus. Every candidate preserved all 20 native hashes, native sizes, and
allocated-byte totals. No benchmark repetitions were added for precision.

| Trial | Compile ratio | Peak RSS ratio | Decision |
| --- | ---: | ---: | --- |
| One static-call-graph ordered backend file, 476-function common prefix | 1.000 | 1.005 | No resource gain |
| Tighter infrastructure-layer prefix, 184 functions | 0.985 | 1.008 | No RSS gain for a large move |
| Mask the already validated dynamic option word, checking capacity once | 0.991 | 1.007 | Too small to retain |
| Constructor-checked constant option positions and fixed-word bit reads | 0.986 | 1.009 | Extra generated/API plumbing did not repay itself |

The layout planner moved 878 complete declarations from 63 identically guarded
ARM64 files. It preserved globals, initialization, function bodies, comments,
and directives. Two files' core-runtime import references were canonically
renamed to avoid a collision with the standard runtime import; 724 package
selector bindings were checked, with no local alias capture. The two files
with narrower build constraints were left alone. Binary symbol inspection
confirmed that function ordering really changed. The broad variant passed the
backend suite; the narrow variant built and passed all 20 native-image checks.
Neither layout remains, and the temporary combined file was deleted.

The dynamic-word trial retained checked public option reads and changed only
the pre-resolved hot form, with an initialization capacity guard. Its new
128/129-entry boundary tests and optimization/backend suites passed. The
constant-position trial went further: ARM64 bindings declared checked constant
bit IDs, while the legacy named and owned-Option APIs retained their behavior.
Tests covered full capacity, a mismatched position, captured/inverted public
sense, and the existing selection/shared/backend suites. An initial helper-name
compile error was corrected before the passing tests and measurements. The
constant-position runner was 59,696 file bytes smaller, but its instruction
section shrank only 16,800 bytes; its __TEXT mapping shrank 32 KiB. Those build
observations did not produce a measured RSS benefit. No Bit APIs, BindBit
constructors, capacity guard, static constants, policy methods, or trial tests
remain. No permanent generator was added for this rejected design.

All production files and expected deletions were restored byte-for-byte to the
signed-branch checkpoint before this documentation update. Rejected trial
sources, AST plans, import-binding checks, symbol-order evidence, boundary tests,
and raw measurements are saved in `.validation/optstudy-layout-flags-screens.zip`
and `optstudy-layout-flags-screens.json`. The retained qualified estimates are
still compile 0.729, allocation 0.190, peak RSS 0.847, native bytes 0.924 from
`optstudy-branch-immediates-main-checkpoint.json`. Neither requested 30% gate is
established by that screen, and the full goal remains active.

## Streaming compiler hints from validation

The default-on `validated-hint-stream` optimization passes scalar copies of
successfully validated bytecode events to an optional backend observer. The
ARM64 consumer collects the same per-function hints, local-event counts,
loop constants, call references, and global scores while validation already
has each instruction decoded. The established scanner remains the fallback
for tree bodies, parallel workers, diagnostics, custom instructions, changed
policy, or unavailable/incomplete analysis. Proofs identify the exact module
and policy and can be consumed once. Failed validation clears the proof.

Permanent tests compare the complete hint structures for scalar/control,
branches, calls, SIMD, atomics, interval storage, and table mutations, under
three policies. The campaign diagnostic also compared all 20 inputs under
those policies; that external-path test was removed after the passing check.
The first full runtime run caught missing table-mutation flags: the original
classifier set them as a side effect, which the streaming path had bypassed.
The shared observer handling now preserves the flags, and table grow/fill/copy
parity cases cover the regression. The corrected public runtime suite passes.
Ownership, incompatible policy/module/mode, failed validation, parallel
fallback, and actual compiler consumption are also tested.

A single alternating fresh-process screen against the signed-branch candidate
showed compile ratio 0.963, allocation 1.063, RSS 0.998, and unchanged native
images. A subsequent single pair against starting main showed compile 0.723,
allocation 0.202, RSS 0.844, native bytes 0.924. Each binary received one untimed
first-input compile before measurement. These are separate noisy screens;
do not multiply their ratios or claim the 30% compile gate from rounding.
No repeated sampling was added to chase precision.

The wasm, optimization, shared/backend, public-runtime, register-allocation
check, profiling, and 20-corpus suites pass. All 2,277 complete valid off/on
workload pairs pass across 100 selections; stack-fence off remains deliberately
invalid. Linux ARM64, AMD64, and RISC-V runtime binaries cross-build, without
cross execution. The precompiled runtime builds; its ordinary source-compiling
tests cannot execute in that mode and fail at the documented unavailable-source
API, so they are not counted as a pass.

Artifacts: `.validation/optstudy-fused-validation-main-checkpoint.json`,
`optstudy-fused-validation-source.zip`, and the matching test/matrix logs and
diagnostics archive. The signed-branch source checkpoint remains available for
rollback. Next work should reduce streaming callback/frame overhead and the
remaining physical-memory cost. The new observer path is qualified but does
not satisfy either outstanding 30% gate.

## Streaming callback frame screen

Moving control-frame copies and decoded memory records into non-inlined helpers
reduced the hot callback's Go frame from 400 to 144 bytes. The backend suite
passed and all 20 native hashes stayed identical, but one paired screen gave
compile 1.008, allocation 1.000, and RSS 1.001 against the qualified streaming
candidate. The extra helper calls did not earn a retained change. The trial was
archived in `.validation/optstudy-stream-frame-screen.zip` and reverted, with
all qualified source hashes and expected deletions checked before this note.
No additional samples were taken. The retained checkpoint is still the fused
validation candidate: compile 0.723, allocation 0.202, RSS 0.844 versus starting
main; the goal remains active.

## Scalar streaming validation

The observed validation loop now decodes local get/set/tee indexes and primitive
integer/float constants directly into scalar hint events, without constructing
and redispatching temporary Instruction/directOp records. Local typing and
non-null initialization rules share one helper with ordinary/tree validation;
LEB and float decoding still use the existing checked readers. Numeric/local
fast paths remain inside the default-on `validated-hint-stream` selection,
whose disabled form keeps the ordinary direct validation loop. No native
emission or runtime instruction semantics changed.

The local-only screen gave compile 0.972, allocation 1.000, RSS 1.002 versus the
qualified fused-validation binary. Adding primitive constants gave compile
0.964, allocation 1.000, RSS 1.011. All 20 native images remained identical.
A separate one-pair main comparison gave compile 0.728, allocation 0.202,
RSS 0.850, native bytes 0.924. The direct main screen does not establish the
30% compile gate. Do not multiply ratios from different screens. No precision
repetitions were run.

New differential tests compare exact validation errors and successful function
facts with/without the observer: local reads/writes/tee, stack underflow and
wrong types, unknown/truncated indexes, polymorphic stacks, signed constants,
malformed LEB widths, floats, and truncated payloads. Permanent hint parity,
wasm/shared/backend/public-runtime suites, register-allocation checking, and
profiling tests pass. All 20 corpus tests and 2,277 complete valid off/on pairs
across 100 selections pass. Linux ARM64/AMD64/RISC-V binaries cross-build, and
the precompiled runtime builds; cross binaries were not executed.

The qualified archive is `.validation/optstudy-scalar-validation-source.zip`,
with `optstudy-scalar-validation-main-checkpoint.json` and matching test/matrix
logs. The fused-validation checkpoint remains available for rollback. A paused
owned yyjson process was inspected with full vmmap output; its image reported
3,024 KiB resident __TEXT and 644 KiB resident __DATA_CONST, while physical
footprint was 4,497 KiB. These paused-region figures neither attribute the
raw peak-RSS difference nor satisfy the memory gate. They are diagnostic only;
the unchanged startup allocation trace also provides no large allocation target.
No randomness or security mechanism was changed during the dependency audit.
The next task remains a substantial physical-memory reduction, while improving
the roughly 27–28% compilation result. Both 30% gates remain unmet.

## Additional TinyGo profile diagnostic

The local TinyGo 0.41.1 installation rejects Go 1.27.1. Using the already
cached Go 1.26.8 frontend, matching original-main/candidate probes built with
`-scheduler=tasks -no-debug -opt=z -gc=conservative`. Temporary copies of the
runner replaced the unavailable TinyGo `MemStats.StackSys` field with JSON
null; the qualified production runner and library sources were unchanged.

The matched TinyGo resource run stopped after five completed pairs when
original main crashed with SIGILL compiling yyjson. An isolated retry reproduced
the crash without diagnostic output. This is an incomplete baseline, not a
passed comparison or a candidate regression. The candidate subsequently
compiled all 20 raw inputs and passed exact oracles (one initial plus one warm
invocation) for all 102 separate stateless wasm.fyi application contracts. These
102 contracts are not the selected 20-module execution matrix.

A cross-profile diagnostic against earlier original-Go samples gave candidate
TinyGo/original-Go compile 1.673 and RSS 0.387. This was not a contemporaneous
pair, uses different runtimes/toolchains, and cannot satisfy the existing
acceptance gates. The profile offers lower memory but no demonstrated CPU
benefit, so no build profile was changed and no new benchmark repetitions were
used to refine it. CI pins TinyGo 0.42.0; the local diagnostic used 0.41.1.
Temporary sources, logs, partial pairs, successful compile/oracle records,
toolchain flags, and binary hashes are archived in
`.validation/optstudy-tinygo-profile-diagnostic.zip` and its JSON summary.
The standard-Go scalar-validation checkpoint remains authoritative; only this
README differs from its source manifest. Both 30% gates remain unmet.

## Checked opcode-byte iteration screen

Go reports Reader.Byte inline cost 88 against budget 80. Moving its error
construction to a two-result cold helper increased the cost to 94, so that
variant was not measured. An alternate checked NextByte boolean API inlined
into the lowering loop, with the original Byte call retained only for the exact
out-of-bounds diagnostic. Wasm/backend suites passed and all 20 native images
were unchanged, but one paired screen gave compile 0.995, allocation 1.000,
and RSS 1.001 against the scalar-validation checkpoint. The extra API and body
loop changes were reverted, with qualified source hashes checked (README-only
notes excluded). The trial, inlining evidence, disassembly, and paired samples
are archived in `.validation/optstudy-reader-byte-screen.zip` and its JSON file.
No precision repetitions were taken. The retained result is still the standard
Go scalar-validation checkpoint, and the goal remains active.

## Variant-packed operand storage screen

A type-checked source rewrite across default, profiling, and register-check
configurations packed storage from 24 to 16 bytes and operand nodes from 32 to
24. Constants and deferred children retained all 64 payload bits. Other variants
shared the payload words for index/borrow information, while an auxiliary word
held native slots, memory displacement, deferred metadata, or custom sidecar
identity. Custom spilled values kept their frame slot in an otherwise unused
payload word. Node addressing and bounds checks were unchanged; no unchecked
arena addressing was introduced.

The audit found 59 typed slot/index selector uses, seven writes, and 85 related
literals in 389 default files. The union of the three build configurations
rewrote 42 existing files and added one layout helper. The backend suite passed,
including the updated 16/24-byte size assertions, and all 20 compiled native
images were identical. A single alternating paired screen against the qualified
scalar-validation binary gave compile 1.012, allocation 0.990, and RSS 0.996.
Go already inlined the constructor at cost 59; constructor calls were not the
missing improvement. The runner grew 33,504 file bytes.

This trial was rejected for its weak memory gain and added packing cost. All
42 files were restored byte-for-byte and the helper removed, with qualified
source hashes and expected deletions checked (README-only notes excluded).
Typed plans, source snapshots, helper, audit, inlining evidence, backend log,
and raw measurements are saved in
`.validation/optstudy-storage-variant-screen.zip` and its JSON summary. Public
runtime/tagged execution qualification was not run for this rejected trial.
No precision repetitions were taken. The standard-Go scalar-validation
checkpoint remains authoritative, and the full goal remains active.

## Execution snapshot cost bound

A temporary Go build overlay made cloneCompiledExecutionMetadata return its
input, solely to measure snapshot-cloning cost before considering an ownership
redesign. This intentionally bypassed public mutation isolation and is not a
production-eligible optimization. No worktree source was changed for the
experiment and no runtime/isolation test pass is claimed for it.

One alternating pair per input against the qualified scalar-validation binary
gave compile 0.987, allocation 0.768, RSS 0.990, with all 20 native images
unchanged. Removing the whole clone saves allocation traffic but did not expose
a large physical-memory target. This is a screening diagnostic, not a strict
mathematical upper bound on every possible ownership architecture. The result
does not justify breaking public metadata isolation for the outstanding RSS
requirement. The overlay, build log, raw pairs, and scope caveat are saved in
`.validation/optstudy-snapshot-bypass-diagnostic.zip` and its JSON summary.
The qualified source is unchanged except for this README, and the goal remains
active with both 30% gates unmet.

## TinyGo speed and collector screens

The same temporary TinyGo 0.41.1 / Go 1.26.8 probe built with `-opt=2` rather
than `-opt=z` gave compile ratio 0.509, allocation 0.858, and RSS 1.090 across
one alternating pair per raw input. Every native image stayed identical.
Two independent `-opt=2` trials then tested removing eleven noinline directives
from four copied files and switching conservative heap tracing to the available
precise collector. Their compile/RSS ratios against the speed baseline were
1.002/1.043 and 1.007/1.002 respectively, with identical native images. Neither
trial earned a retained change. The qualified Go source was untouched; the
noinline trial used a temporary symlink tree with only those four files copied.

These are additional-profile compile-only diagnostics, without new execution
qualification or a passed matched original-TinyGo baseline. They do not satisfy
the standard-Go goal, and no production/release profile was changed. Settings,
source diffs, binary hashes, build logs, and raw pairs are saved in
`.validation/optstudy-tinygo-speed-screens.zip` and its JSON summary. No precision
repetitions were taken. The full goal remains active.

## Native Linux ARM64 qualification

The qualified scalar-validation source was cross-built with Go 1.27.1,
`CGO_ENABLED=0 GOOS=linux GOARCH=arm64`, alongside starting origin/main
fc6af651c15e286409d9cc117c94181e9ee50ff3. A dedicated container in the existing
Docker Desktop aarch64 VM used the already-local Python 3.12 slim image
sha256:ddb0207ae1f0356c2b724d740769b0c5f5f51cc54a0525178f721825f78fe74c.
Inputs and binaries were read-only, networking disabled, memory limited to
1 GiB, and both revisions ran on virtual CPU 4 with GOMAXPROCS=1. That CPU was
not exclusive; the VM and host had other active services. No other service was
stopped. The resource and execution containers have distinct recorded IDs,
because the first execution attempt revealed an omitted fixture mount; the
owned probe container was recreated with that mount and then tests passed.

One alternating fresh-process pair per frozen input, after one untimed first
input per binary, gave compile 0.661, allocation 0.204, peak RSS 0.782, and
native bytes 0.924 versus starting main. A small shell forked the actual runner
after its own exec to avoid a Python-parent RSS floor. RSS includes startup;
no peaks were subtracted or replaced. Default native hashes match the qualified
Darwin images for all 20 inputs. These are screening results, not isolated or
precision measurements; neither this platform nor Darwin meets 30% lower RSS.

All 20 selected-corpus pipeline/oracle tests pass natively on Linux. One warmed
calibrated batch per workload and revision, alternating revision order by
module, checked all 23 exact oracles and gave execution ratio 0.998. All 2,277
complete valid optimization off/on workload pairs pass across 100 selections.
Stack-fence off remains deliberately invalid. Original-main/candidate source,
input hashes, binary hashes, commands, image/container identity, raw resource
and execution records, and matrix logs are saved in
`.validation/optstudy-linux-arm64-checkpoint.json`, the matching source ZIP,
and diagnostics ZIP. No source change was needed for this extra platform.

A separate diagnostic build disabled backend inlining with `-gcflags` while
keeping the qualified source unchanged. It gave compile 1.218, allocation
1.011, and RSS 1.053 against the normal candidate, with identical native images.
Disabling inlining is rejected. An offline symbol audit found 183 selected
frontend-validation/decoding and streaming-observer functions totaling 152,656
text bytes; only 25 complete 4-KiB pages were exclusively owned by that selected
set in this ELF image. This is static ownership, not resident pages or measured
savings. No code-page reclamation was implemented.

The subsequent TinyGo allocation report listed sites in compileFuncAttempt
at named return storage, captured state/function pointers, deferred handlers,
and ordinary local/trap arrays. It did not identify the large fn literal itself
as an independent allocation site. A separate temporary source tree removed
per-function recovery and defer setup, adding explicit successful-return cleanup
at the caller. This intentionally did not preserve panic behavior and was never
production eligible. It compiled all 20 inputs with identical native images,
but one pair per input gave compile 0.973, allocation 0.992, and RSS 0.988 against
the TinyGo speed baseline. The result does not justify a module/worker recovery
redesign for the remaining gap. A writer-specific allocation report listed no
heap sites, so the suspected per-word append allocation has no supporting
evidence. No writer rewrite was performed. Reports, temporary source, logs, and
raw pairs are in `.validation/optstudy-tinygo-defer-diagnostic.zip` and its JSON
summary. The production source remains the qualified scalar-validation
checkpoint, and both platform memory results remain below the requested 30%.

## Direct private-arena addressing screen

A new runtime selection compared checked slice indexing with direct pointer
addressing for private, allocation-domain-checked operand coordinates. Its bool
fit existing stack padding. Both production worker paths selected the mode per
function, and checked indexing remained available when off. Backend/catalog
suites passed, as did strict Go checkptr=2 tests comparing every coordinate
across chunk growth, GC, sentinel/nil handling, and recycled zeroed slots.
One same-binary alternating off/on pair per input gave compile 0.995, allocation
1.000, RSS 0.999, and unchanged native images. The sub-percent result did not
justify the addressing complexity. All five existing files were restored,
the trial test removed, and all qualified source hashes/deletions verified.
No direct-addressing API or selection remains. Sources, pointer tests, logs,
and raw pairs are in `.validation/optstudy-direct-arena-screen.zip` and its JSON
summary. No precision repetitions were run. The next diagnostic separates
TinyGo allocation traffic by compile phase before selecting another change.

## Allocation phase diagnostic and inactive instrumentation screen

Temporary instrumented Go and TinyGo builds compiled all 20 inputs successfully.
Across those inputs the backend phase recorded about 1.11 MB / 5,082 allocations
in Go and 11.70 MB / 398,369 allocations in TinyGo. Public metadata construction
recorded about 0.44 MB / 525 versus 4.55 MB / 11,395. The extra ReadMemStats calls
and global counters make this allocation attribution only, not time/RSS evidence.
A separate temporary default-build specialization removed inactive profile and
register-check defers while preserving panic recovery and semantic cleanup.
The backend suite passed and all 20 native images matched. One paired screen
per input gave TinyGo compile 0.984, allocation 0.997, RSS 0.987: rejected as weak.
Instrumentation modes were deliberately not preserved in that temporary copy;
no production specialization was made. Sources, raw records, and static backend
allocation-site output are in `.validation/optstudy-phase-allocation-diagnostics.zip`
and its JSON summary. The next screen targets captured closures and fixed-array
receiver copies, again in a temporary tree.

## Copy and closure screen

A temporary tree replaced two captured lowering closures, added a pointer-based
resolved-selection read, and used pointer receivers for the type descriptor
converter. TinyGo speed-profile compilation of all 20 inputs produced identical
native images; one pair per input gave compile 0.960, allocation 1.0004, RSS 0.993.
The unchanged allocation traffic does not support those sites as the missing
backend allocation source. Catalog/backend tests passed, but the constructor
pointer introduced an extra Go allocation: the existing descriptor budget test
failed 3 versus 2 for three inputs. Rejected without production changes. Evidence
is in `.validation/optstudy-copy-closure-screen.zip` and JSON. No execution
qualification was claimed. Next temporary screen enables the existing shared
scalar path in TinyGo while retaining the profiling fallback.

## TinyGo scalar path screen

A temporary build admitted the existing shared scalar lowering under TinyGo,
retaining the ordinary profiling fallback. All 20 raw compiles succeeded.
One pair per input against the speed-profile TinyGo candidate gave compile
1.024, allocation 1.000, RSS 1.005; native code size was essentially unchanged.
Rejected without changing production tags. Native images differ as expected
from the path switch; no execution qualification was claimed. Evidence is in
`.validation/optstudy-tinygo-scalar-path-screen.zip` and JSON. Current diagnostic
work attributes allocations to individual opcode-lowering operations in a
temporary TinyGo-only tree. It must not be treated as an acceptance benchmark.

## Opcode allocation attribution and value-budget candidate

The TinyGo-only diagnostic reads allocator counters through go:extern (the
first go:linkname-variable version failed with duplicate symbols). All 20 raw
compiles passed and all native images matched. Inclusive lowering counts were
about 164,776 allocations under direct calls, 49,727 under i32.store, and 40,990
under i32.load. Ordinary memory operations commonly recorded two 8-byte objects.
These counts are attribution only; nested lowering can overlap. No instrumented
time or RSS is acceptance evidence.

The two bounded address proofs passed local recursion-budget pointers, and the
static TinyGo report identified these locals as heap sites. A temporary change
returns the remaining budget by value, preserving depth-first traversal order,
limit consumption, and conservative failure. Backend tests passed. All 20 native
images matched under each toolchain. One pair per input against the qualified
candidate gave Go compile 0.980, allocation 1.000, RSS 0.997; TinyGo speed profile
compile 0.895, allocation 0.932, RSS 0.997. This is promising for compilation but
has not yet been installed or fully qualified. Evidence/source are saved in
`.validation/optstudy-opcode-allocation-budget-screen.zip` and JSON. Current
next screen (`/tmp/wago-budget-regcopy-src`, binary build log
`/tmp/wago-budget-regcopy-build.txt`) adds removal of the captured isSource
closure in shared register-move resolution, without changing the algorithm or
callback API. Compare that binary against `/tmp/wago-optstudy-tinygo-budget`
to attribute the additional effect. Production sources still match the qualified
scalar-validation archive except these README notes.

## Qualified value-budget and register-source checkpoint

Retained two general allocation improvements in three files: recursive address
proofs return their unused node budgets by value, and shared parallel register
move resolution checks source membership directly without a closure capturing
the mutable graph. The proof limits, depth-first consumption, register scheduling
algorithm, callback API, and emitted native images are preserved.

The default compiler/wasm/optimization/shared/backend/public suites pass, as do
profiling and register-check backend suites, all 20 corpus pipeline tests, all
2,277 complete valid off/on workload pairs across 100 selections, Linux ARM64,
AMD64 and RISC-V cross-builds, and the precompiled build. Stack-fence off remains
deliberately invalid and is excluded from complete pairs. Default native images
match the previous scalar-validation source on all 20 inputs, so no new warmed
execution timing was collected to repeat identical images for precision.

One fresh alternating pair per input against starting main gave compilation
0.6993, allocated bytes 0.2017, peak RSS 0.8495, native bytes 0.9242. Treat this
as about 30% compilation / 15% RSS improvement under competing load. It does not
complete the 30% memory goal. Current runner is
`/tmp/wago-optstudy-budget-regcopy`; suite is
`/tmp/wago-suite-budget-regcopy.test`. The checkpoint JSON, full source manifest
(including expected deletions), diagnostics, and logs are in
`.validation/optstudy-budget-regcopy-*`. Earlier archives remain rollback points.

TinyGo-only sequential screens found budget-value compilation 0.895 and
allocation 0.932 against its speed-profile candidate, then removing the captured
register-source closure gave compilation 0.954 and allocation 0.961 against the
budget candidate. RSS stayed roughly flat in both. Each screen checked one pair
per input and identical native images. These use local TinyGo 0.41.1 / Go 1.26.8,
not CI's pinned TinyGo 0.42.0, and do not establish a complete matched original
TinyGo baseline or the selected 20-module TinyGo execution matrix.

Next useful lead: call lowering still dominates the TinyGo opcode allocation
trace. Nested callbacks around the fixed machineWindow, register move accessors,
and call-site root finalization may escape. Do not assume the older closure
screen disproved these distinct sites; isolate remaining callback ownership
before changing the shared API. Continue pursuing a substantial RSS reduction;
allocation improvements alone cannot close the physical-memory gate. No new
benchmark repetition is needed for precision. Source remains uncommitted.

## Pinned TinyGo 0.42 profile qualification diagnostic

The official Darwin ARM64 0.42.0 archive was downloaded only into /tmp and its
SHA-256 was checked against GitHub release metadata. The local tool reports
Go 1.27.1 and LLVM 22.1.4. First unpatched tasks builds failed duplicate
`tinygo_task_exit` symbols. CI already applies `scripts/patch-tinygo-042.sh`
(upstream #5656); no release-script change is needed. The temporary toolchain
was patched identically for subsequent task builds.

An unpatched native threads/conservative-GC comparison completed all 20 raw
compiles for starting main and current source at both opt=z and opt=2. Each image
received one first-input warmup, then one fresh sample/input, rotating four-image
order. Current/main ratios: size profile compile 0.752, allocation 0.470,
peak RSS 0.802; speed profile compile 0.721, allocation 0.431, peak RSS 0.781.
Speed versus size for current source gave compile 0.477 and RSS 1.083. These are
separate build-profile results, not standard-Go acceptance evidence. Candidate
native images are identical across both profiles and the older TinyGo candidate.
No selected-20 TinyGo execution matrix has yet been run.

Patched task builds succeeded, but the matched run stopped after five complete
four-image groups when original-main opt=2 hit SIGILL on yyjson with no output.
This remains an incomplete task baseline. A debugger attempt used an incorrect
guessed fixture path and never produced a useful stop; all owned debugger,
debugserver, and child processes were terminated. No fault cause is established.
TinyGo's native default fallback task stack is 64 KiB; stack exhaustion remains
only a hypothesis, not a diagnosis. Complete raw records, partial failures,
commands, probe sources, official asset/patch provenance, and hashes are saved in
`.validation/optstudy-tg042-profile-diagnostics.zip` and JSON.

A separate temporary one-file call-lowering screen removes the captured GC-root
finalizer and replaces the sole wrapper-call callback with its direct target
index. Go backend/public suites passed. Its TinyGo thread-profile build is
currently pending under `/tmp/wago-build-callclosure-tg042.py`; the wrapper restores
the local toolchain patch after building against the exact unpatched control.
The screen source is `/tmp/wago-callclosure-src`; qualified production sources
are unchanged. Compare `/tmp/wago-optstudy-tg042-callclosure` with
`/tmp/wago-optstudy-tg042-candidate-2` only after the build is terminal. Runtime
stack/profile changes must not be mixed into that source-change comparison.
The memory gate stays unmet under every completed matched profile.

## Qualified direct internal-call bookkeeping checkpoint

The internal-call path no longer creates a captured GC-root finalizer: an
explicit helper records the same single relocation after emission. The sole
wrapper caller passes its direct local target index instead of a closure; the
clobber check, BL relocation, argument setup, result publication, and exact GC
map decision retain the same order. No scheduler or release settings changed.

The one-file screen passed Go backend/public suites. The installed source is
byte-identical to that tested copy. Profiling and register-check suites pass,
as do the 20 corpus pipeline tests, all 2,277 complete valid off/on pairs across
100 selections, Linux ARM64/AMD64/RISC-V cross-builds and precompiled build.
Stack-fence off remains deliberately invalid, excluded from complete pairs.
All 20 native images match under both Go and the TinyGo thread profile.

One pair per input against the preceding candidate gave Go compile 0.960,
allocation 1.000, RSS 0.994; TinyGo compile 0.943, allocation 0.979, RSS 0.997.
Fresh direct-main comparisons, rather than multiplied screens, now give:
Go compile 0.6957, allocation 0.2017, RSS 0.8485, native bytes 0.9242;
TinyGo threads/opt=2 compile 0.7122, allocation 0.4218, RSS 0.7762, native bytes 0.9127.
TinyGo uses the exact unpatched thread control for this comparison; the build
wrapper restored the repo's upstream task-exit patch afterward. It does not
supply the missing selected-20 TinyGo runtime/oracle matrix. No new warmed Go
execution timing was collected for unchanged default images.

Current Go runner `/tmp/wago-optstudy-callclosure`, suite
`/tmp/wago-suite-callclosure.test`; TinyGo diagnostic runner
`/tmp/wago-optstudy-tg042-callclosure`. Full source/deletion manifest, raw main
comparisons, tests, scripts, and binaries' hashes are in
`.validation/optstudy-callclosure-*`. The prior budget/regcopy checkpoint is
available for rollback. Source remains uncommitted; the primary checkout is
untouched. The physical-memory goal remains active.

Next memory experiment: TinyGo 0.42's native default Boehm GC is not yet screened.
Compare original main and current source under identical threads/Boehm settings
and qualify native runtime/public oracles before using those resource numbers.
Keep that configuration separate from conservative-GC and Go acceptance records.
The existing profile proof shows that simply choosing opt=2 spends more RSS.
If returning to task-scheduler failure diagnosis, use the actual frozen yyjson
path from the checkpoint JSON, not the guessed c/yyjson path. A larger fixed
stack has not been tested; stack overflow remains unproven. Never substitute
allocation traffic or subtract unrelated high-water marks for the RSS gate.

## Boehm native profile diagnostic

Pinned/patched TinyGo 0.42.0 built both revisions with opt=2, scheduler=threads,
gc=boehm and no-debug. The current source's available native public suite compiled
and ran successfully (PASS; no runtime test filter). All 20 raw modules compiled
on both revisions, and every candidate native image matched the previous TinyGo
candidate. One fresh alternating pair/input gave compile 0.683, allocation
0.429, peak RSS 0.807, and native bytes 0.913. This completes a valid supplemental
profile screen, but its roughly 19% RSS reduction still misses 30%; the
conservative-GC thread screen was about 22%. No release/profile setting changed.
This does not supply the missing selected-20 TinyGo runtime execution matrix.
Sources, hashes, commands, complete raw samples, and public-suite logs are in
`.validation/optstudy-tg042-boehm-diagnostic.zip` and JSON. All owned build/test
sessions for this experiment were observed terminal; no process is still awaited.

The prior minimal-caller diagnostic already tested forced GC both ways, so that
hypothesis was checked against saved evidence without new repetitions. Next
source experiment: replace captured register-move scheduling callbacks on ARM64
with reusable private graph state yielding primitive move/swap operations.
Preserve the exact 24-operation window flush points and FP scratch-slot lifetime;
otherwise native-image identity is not a valid shortcut. Share the graph logic
with the existing callback adapter for other architectures. The codebase-design
skill was read for the small result-returning interface. Prototype in a temporary
tree first; production remains the qualified direct-call bookkeeping checkpoint.
The earlier GC-root cold split already tried a prepareGCFrameCallsite wrapper
under Go with no RSS gain; do not repeat that Go screen for precision.

## Qualified reusable register graph checkpoint

RegMoveGraph owns its private pending graph and returns primitive move/swap
operations through Reset/Add/Next. The existing shared callback interface is an
adapter over that single scheduling implementation. ARM64's four production
shuffle sites use worker/module-owned scratch and a direct physical emitter,
removing captured per-call graph/window callbacks. The exact 24-operation flush
points, deterministic destination order, GP scratch register, FP swap-slot
lifetime, and swap-chain rewrites are preserved. The existing private callback
window remains used by test/reference and checker-corruption cases.

An exhaustive assignment oracle covers roughly 8,500 small graphs, including
fan-out, cycles, no-ops, unwritten sources, high register bits, and abandoned
state reset. An encoded-stream differential covers long cycles straddling a
window flush, fan-out, repeated reuse, changing FP live-prefix floors, exact
spill extent and swap-chain counts. Standard Go shared/backend/public suites
pass. Window tests pass under default/profile/regalloc modes. Linux ARM64,
AMD64 and RISC-V builds and the precompiled build pass. All 20 corpus pipelines
and all 2,277 complete valid switch pairs pass across 100 selections. Stack-fence
off remains invalid and is excluded. All 20 native images are unchanged under
Go and TinyGo. No repeated warmed timing was needed for identical Go images.

TinyGo's full shared suite stopped on unimplemented reflect.Type.NumOut; the
focused TestRegMoveGraphSimultaneousAssignments then passed natively under the
same threads/Boehm profile. The full shared TinyGo suite is not a pass, and the
failure was not attributed to a source regression. The earlier Boehm public
suite pass is for the preceding call-bookkeeping source, not this new graph.

One direct paired source screen gave Go compile 0.920, allocation 1.000, RSS
0.997; TinyGo/Boehm compile 0.957, allocation 0.990, RSS 1.001. Runner file bytes
fell 34,432 in the Go screen and 5,376 under TinyGo; file size is not RSS.
The fresh direct-main screens give Go compile 0.7110, allocation
0.2017, RSS 0.8570, native bytes 0.9242;
TinyGo/Boehm compile 0.6981, allocation 0.4251, RSS
0.8054, native bytes 0.9127. Do not multiply prior source screens
or select an earlier timing as current evidence. The physical-memory target is
still unmet, and the current Go time screen is near but below 30% improvement.

Current Go runner `/tmp/wago-optstudy-reggraph`, suite
`/tmp/wago-suite-reggraph.test`; TinyGo diagnostic runner
`/tmp/wago-optstudy-tg042-boehm-reggraph`. Full manifests, raw samples, test logs,
failed broad TinyGo log, prototype sources and scripts are saved in
`.validation/optstudy-reggraph-*`. The exact prior call-bookkeeping checkpoint
remains a rollback point. Source is uncommitted and the primary checkout preserved.

Remaining work must target a substantial common resident footprint, not count
allocation traffic as peak RAM. Forced-GC/minimal-caller, blanket encoder
inlining, scalar dispatch recipes, GC cold wrappers, and metadata snapshot
bypass have already been screened; check their records before repeating a
configuration. New runtime profiles are supplementary and must preserve the
frozen 20-input workload and input hashes. No owned process is still awaited
when this checkpoint is saved. Goal stays ACTIVE.

## Owned-image paging diagnostics (production unchanged)

The qualified 277-file register-graph source and all 20 expected deletions were
verified unchanged before this journal addition. Startup dependencies contain
no network/WASI chain to remove, and the prior init allocation trace has no large
allocation owner. A temporary CGO diagnostic queried only its own Mach-O image.
Mach page-present/mincore flags showed every text backing page present before
Compile; they cannot be treated as process RSS. Fresh executable copies showed
about 116-117 CS_VALIDATED text pages after setup and 185-188 after Compile, at
16 KiB/page. These are code-signing-validation coverage/intersection proxies,
not proof a whole function executed or an attribution of the peak RSS delta.
The same symbol intersections occurred in json-as, yyjson and Lua; cold helpers
sharing pages/prefetch can appear in those intersections.

Review of the proposed explicit-memarg summary shortcut found that the default
ARM64 feature policy already admits multi-memory, so the body-scan restriction
would not explain this frozen-default result. No new validation flag was added.
Decoder/frontend declaration packing has already been screened and reverted;
do not repeat it merely because a cold helper appears in a validated page.

One same-CGO-binary off/on pair per input tested MADV_RANDOM on its own TEXT,
including the C call and hint in the timer. Compilation 0.996, RSS 1.001:
rejected. Separate fresh owned executable copies per process isolated backing
objects for the discard trials. MADV_DONTNEED gave compile 1.035/RSS 1.002;
MS_SYNC|MS_INVALIDATE gave 0.999/0.998. Both are rejected. All 20 inputs and
native output hashes matched the qualified source. These are supplementary
configuration tests, not fresh original-main comparisons.

Explicit same-file TEXT remapping first verified that the whole mapped text
matched its file bytes in a separate process. One pair per input then gave
compile 1.012, allocated bytes 1.000, peak RSS 0.915, native bytes 1.000.
This byte-identity preflight is only for the owned probe, not arbitrary host
images, load-time fixups, replaced executable files, debuggers or hot patches.
The prototype is not production eligible on that evidence alone.

A Mach VM-copy variant preserves the current mapped bytes, including fixups,
by creating a temporary COW alias, replacing the original mapping at the same
address, then dropping the alias. All 20 compile checks and code hashes matched.
Text-only gave compile 1.083/RSS 0.911; text plus DATA_CONST gave 1.091/0.895.
The ordinary-Go gate must not be multiplied by these CGO-probe ratios. No
production remapper, profile setting or code-protection change was installed.
No executable on disk was modified; only owned child mappings were replaced,
and their temporary executable copies were deleted after each child exited.
All diagnostic processes were observed terminal before saving.

Raw records, exact binary hashes and replayable binaries, source, commands,
page-query samples and symbol intersections are saved in
`.validation/optstudy-text-paging-diagnostics.zip` and JSON. The final multi-mode
VM-copy source and final page-query source are frozen exactly. Earlier hint/file
variants have exact binary/raw receipts; their original separate source states
were overwritten during prototyping and are not claimed as exact snapshots.

Next useful test: temporary phase hooks after validation and before publication
to see whether bounded clean-code working sets can lower the peak further while
retaining the CPU target. Keep these in a prototype until native public/exact
oracles, concurrency, repeated compilation, GC and mapping-lifetime behavior
are qualified. COW shadow-object accumulation and whole-host image scope need
explicit checks; the library's code was not changed. File remapping needs a
stronger immutable-image/identity contract. Prefer current-byte VM copies when
investigating relocated read-only metadata. The qualified ordinary-Go result
remains about 29% faster / 14% lower RSS, so the full goal stays ACTIVE.

## Rejected phase remapping: joint gate screen

A temporary source clone added phase hooks before decode, after validation,
before backend emission, after backend emission, and at publication. The Cgo
helper lives in a separate internal diagnostic package because src/wago also
contains Go assembly. Every phase is applied at most once per process. The
candidate worktree's production source is byte-identical to the register-graph
checkpoint; only this journal changed.

A four-input placement screen suggested a substantial RSS/CPU tradeoff. A full
20-input screen with one first-input warmup per configuration and one fresh
private executable per measured process gave these ratios against the same
Cgo probe with hooks disabled:

| Policy | Compile time | Peak RSS |
| --- | ---: | ---: |
| File remap at entry | 1.099 | 0.910 |
| Current-byte VM copy at entry, after validation, after backend | 1.031 | 0.791 |

A separate direct original-main comparison of the latter policy gave compile
**0.799**, peak RSS **0.663**, allocated bytes **0.202**, native bytes **0.924**.
This establishes a ballpark 34% RSS reduction for this owned-image prototype,
but only 20% faster compilation. It fails the joint 30%/30% gate and is rejected
for production. These are single paired samples, alternating order, GOMAXPROCS=1,
with timed phase work and uncontrolled host load. No ratios were multiplied.
All 20 exact input and native-code hashes match the qualified references.

No OS remapper was installed in production, and no runtime-policy qualification
is claimed. Concurrent embedding, GC, repeated compilation and COW mapping
lifetime remain unqualified. Exact final source, raw records, scripts, probe
module and both executable images are frozen in
`.validation/optstudy-phase-paging-diagnostics.zip` with a SHA256 manifest;
the adjacent JSON records the decision and limits. The initial scaffold script
alone predates the separate-internal-package migration; use the archived final
source, not that script alone, to reproduce the prototype.

The latest qualified ordinary-Go checkpoint remains about 29% faster compilation
and 14% lower peak RSS. The goal is ACTIVE. Continue with source-level reductions
in compiler working set; do not repeat phase remapping or promote allocation
traffic to the physical-memory acceptance metric.

## Rejected module context and SIMD callback screens

Two independent temporary trees tested source-level sharing against the qualified
register-graph checkpoint. Production sources and the 20 expected deletions were
verified against its source manifest before saving. Only this journal changed.

The module-context prototype moved 17 immutable module lowering inputs into one
private context, leaving function hints, GC root plan, stats and worker scratch
as separate inputs. Serial and parallel paths construct the context outside the
function loop; lowering and optional native-import attempts use the same context.
Backend tests passed, all 20 exact native/input hashes matched, and one alternating
pair per input gave compile **1.078**, allocated bytes **1.000**, peak RSS
**0.993**, native bytes **1.000**. Rejected: slower and negligible RSS benefit.

The SIMD prototype replaced three-register bound method values with primitive
instruction descriptors. 76 fixed operations and dynamic float arithmetic and
comparisons share emission, preserving source reversal and checker write events.
Unary and immediate callbacks remain unchanged. Encoder/backend suites passed.
An exhaustive descriptor differential checked 102 variants over all 32^3 register
combinations (3,342,336 comparisons) against the existing encoder interface.
All 20 corpus input/native hashes matched the qualified source. One alternating
pair per input gave compile **0.990**, allocated bytes **1.000**, peak RSS
**0.998**, native bytes **1.000**. The executable was 41,920 bytes smaller;
that is not an RSS saving. Rejected as weak; no descriptor interface was installed.

Both screens use one first-input warmup per image, fresh private executable copies,
GOMAXPROCS=1, and uncontrolled host load. No precision repeats or new execution
acceptance claims were made. Exact final prototypes, modules, binaries, scripts,
raw pairs, test logs and SHA256 manifests are archived under
`.validation/optstudy-context-screen.*` and
`.validation/optstudy-simd-binary-screen.*`. Initial builder scripts predate small
compile-error fixes; archived final source is authoritative for replay.

The joint target is still open. Qualified ordinary-Go results remain approximately
29% faster compilation / 14% lower peak RSS. Do not repeat these sharing screens
for timing precision. Future changes need a substantial footprint mechanism;
smaller files and lower allocation traffic alone do not establish the RAM gate.

## Seven weak-policy removal screen

Startup init tracing found only 26,440 backend allocated bytes / 207 allocations.
This is not a large enough allocation target by itself to explain the physical
memory gap. It is instrumented startup evidence, not a new RSS result. No lazy
initialization or environment behavior was installed.

A temporary tree screened removing the seven already weak/default-off policies:
guarded-test-ccmp, streaming-i32-reduction, dot-loop-vector, crowded-products,
linear-integer-tree, loop-trap-cell, and prepared-light-entry. All are currently
ARM64-only catalog entries. The first variant replaced 12 selection checks with
constant false: one pair/input gave compile **1.075**, allocated bytes **1.000**,
peak RSS **0.993**, native bytes **1.000**. Its 20 corpus pipelines passed.

A fuller variant additionally removed streaming driver admission, dot-loop
finalization and trap-cell state branches. One new pair/input gave compile
**0.980**, allocated bytes **1.000**, peak RSS **0.995**, native bytes **1.000**.
Both variants preserve all 20 exact native and input hashes. Single pairs and
host load make timing ballpark; no precision repetitions were run. The fuller
runner is 171,040 file bytes smaller, which is not a meaningful RSS saving.
This supports completing the user's weak-policy cleanup, not claiming that
cleanup solves the physical-memory gate.

These are screening variants only: the catalog/binding override semantics were
intentionally not changed yet and must be removed consistently with source and
test cleanup before promotion. The original source and the first variant's
executable are frozen separately; its corpus pass is not relabeled as a fuller
variant public/toggle suite pass. Initial suite build used GOWORK=off at the root
and failed to find the separate bench module. A private bench go.mod with an
absolute replacement to the prototype fixed the harness; the successful suite
was built against the first prototype, not the original checkout.

Exact sources, builders, raw records, binaries, corpus logs, private modules,
startup trace and SHA256 manifest are in
`.validation/optstudy-prune-seven-screen.zip` and adjacent JSON. The first source
is an archive inside that archive; the fuller final files are under prototype/.
No production compiler source changed. The next action is actual removal of the
seven catalog entries, bindings, dead lowering/state paths and obsolete emission
assertions in a fresh temporary tree, retaining semantic fallback coverage.
Then run backend/catalog/public suites and the reduced complete toggle matrix.
The goal remains ACTIVE: qualified Go results are still about 29% faster and
14% lower RSS, and the full physical-memory target is not established.

## Qualified seven-policy removal checkpoint

Removed guarded-test-ccmp, streaming-i32-reduction, dot-loop-vector,
crowded-products, linear-integer-tree, loop-trap-cell and prepared-light-entry
from ARM64's implementation, bindings and catalog. New compilation no longer
emits light-entry metadata; existing artifact/runtime readers remain available.
Direct and bounded prepared entries retain their normal behavior. Removed dead
loop-vector implementations, streaming driver/hints, trap-cell reservation and
loads, linear-tree lowering, product scheduling and CCMP recognition. The change
removes 3,083 net lines across 33 modified/new and 12 deleted files.

Tests for removed admission/rollback controls were replaced by explicit unknown
policy rejection checks. Semantic coverage remains for integer overflow, local
writes, pressure, discarded-load traps, aliasing, partial writes, scalar tails,
NaNs, infinities, signed zero, subnormals and public artifact round trips. Float
map/coefficient cases now compare against independent Go load/arithmetic/store
oracles rather than compiling the same fallback twice. Ordinary prepared-entry,
bounds, cancellation, trap continuation and GC progress suites pass.

Full Go wasm/optimization/shared/backend/public suites pass, as do the final
profiling and register-allocation builds. All 20 corpus pipelines and 2,116
complete valid off/on workload pairs pass across 93 selections; the deliberately
invalid stack-fence-off selection still accounts for 23 on-only workloads and is
excluded. All 20 input and default native hashes match qualified references.
Linux ARM64/AMD64/RISC-V runners and the precompiled runtime cross-build; these
new cross binaries were not executed. No new TinyGo acceptance claim is made.

One alternating fresh-process pair per input against starting main measured
compile **0.7257**, allocated bytes **0.2017**, peak RSS **0.8379**, native bytes
**0.9242**. One first-input warmup per image, GOMAXPROCS=1, exact hashes, no
precision repetitions and uncontrolled host load. The fresh result is about
27% faster / 16% lower peak RSS and does not establish either 30% target. Do not
substitute an earlier source's timing or multiply source-screen ratios.

Exact full source and 32 expected deletions are in
`.validation/optstudy-weak-policy-removal-source.zip`; checkpoint JSON, raw
records, prototype sources, scripts, test logs and exact runner/suite images are
saved in `optstudy-weak-policy-removal-*`. A separate parent archive records all
33 changed-file originals plus the deleted source/tests for reversible rollback.
The worktree is uncommitted; the dirty primary checkout is untouched.
Goal remains ACTIVE. Next work must reduce substantial physical compiler memory;
weak-policy stripping is complete for these seven, not a claim of the full goal.

## Unused loop-memory hint cleanup and current TinyGo screen

The retired vectorizer was the only consumer of loop-memory presence. Removed
its scanner marks in both AST and byte paths, high-bit definition/accessors,
per-function field and per-function copy. The normal memory count, exact-bounds
count widths/saturation, loop flags and touches-memory facts remain intact.
An existing loop test now checks memory counts and normal loop/memory flags
inside and outside loops. Five files changed, with eight net lines removed.
This finishes confirmed dead work; it is not claimed as a large RSS mechanism.

Go backend/wasm/public suites, profiling and register-allocation checks pass.
All 20 corpus pipelines and 2,116 complete valid switch pairs across 93
selections pass, with stack-fence-off still invalid/excluded. All 20 default
native/input hashes match qualified references. Linux ARM64/AMD64/RISC-V runners
and the precompiled runtime cross-build; new cross binaries were not executed.
Shared/optimization sources and tests are unchanged from the preceding pass.

One alternating fresh-process original-main pair per input gave Go compile
**0.7137**, allocation **0.2017**, peak RSS **0.8399**, native bytes **0.9242**.
The matching TinyGo 0.42.0 + repository task-exit patch / LLVM 22.1.4 / Go 1.27.1
threads/Boehm opt=2 profile gave compile **0.6676**, allocation **0.4237**, peak
RSS **0.8005**, native bytes **0.9127**. Each image had one first-input warmup;
no precision repetitions. Host load/cache state were uncontrolled. All 20
TinyGo native hashes match earlier TinyGo images. These are separate profile
comparisons, not multiplied ratios or cross-runtime baseline substitutions.
No selected-20 TinyGo execution matrix was run, so the TinyGo record remains a
compiler/profile diagnostic, not complete execution qualification.

The first TinyGo attempt used the Go-only StackSys metric and failed compilation.
The dedicated TinyGo probe now exactly matches the earlier profile harness,
including its null StackSys field. The second build hit TinyGo's normal cache
write restriction. The third authorized cache build completed successfully;
its empty build3 log and executable hash identify the measured image. No
TinyGo toolchain/source patch was changed by this turn.

Current writer disassembly independently confirms Go append emits a single
32-bit store and keeps its barrier on the slice-growth path. Existing cold-grow
and nosplit screens already rejected those variants; neither was rerun or
installed. The exact current assembly excerpt is archived to prevent a future
unfounded per-instruction-barrier rewrite.

Source/checkpoint, raw records, exact probe modules and binaries, logs, scripts
and hashes are in `.validation/optstudy-loop-hint-removal-*`. Production remains
uncommitted and the dirty primary checkout untouched. Goal ACTIVE: Go is about
29% faster / 16% lower RSS; TinyGo is about 33% faster / 20% lower RSS. Both miss
the physical-memory target. A useful next distinct screen is phase collection
of compiler temporaries after validation/before metadata under both runtimes,
including collection cost in time and peak; the earlier forced-GC screen tested
precompile harness collection, not this different phase/lifetime mechanism.
Prototype such a policy first and reject it if it loses the joint target.

## Rejected compile-phase collection screen

A temporary pure-Go prototype added identical scalar hooks after successful
validation/before requirements and after backend return/before public metadata.
An environment-selected mask compared off, validation only, backend only and
both. runtime.GC cost was inside the compile timer. Each runtime/configuration
received one first-input warmup, then one fresh private process per input with
rotated order and GOMAXPROCS=1. All 20 input and native-code hashes matched the
correct Go or TinyGo reference in every configuration. No precision repeats.

Ratios against the same diagnostic binary with collection disabled:

| Runtime | Collection phase | Compile | Peak RSS | Heap after compile |
| --- | --- | ---: | ---: | ---: |
| Go | After validation | 0.894 | 1.029 | 0.941 |
| Go | After backend | 0.943 | 1.027 | 0.790 |
| Go | Both | 1.013 | 1.036 | 0.791 |
| TinyGo threads/Boehm opt=2 | After validation | 1.017 | 0.999 | unavailable |
| TinyGo threads/Boehm opt=2 | After backend | 1.001 | 0.998 | unavailable |
| TinyGo threads/Boehm opt=2 | Both | 1.082 | 0.997 | unavailable |

Allocation and native-byte ratios stayed effectively 1.000. The Go snapshot's
roughly 21% lower heap after backend collection came with a roughly 3% higher
physical peak. TinyGo physical peaks changed less than 1%; reported heap-system
reservations fell modestly but did not establish a physical-memory saving.
These single screens have uncontrolled host load/cache state. The Go timing
improvement in two configurations is preserved as observed evidence; it does
not justify a RAM improvement claim or multiplying it into another source's
main ratios. No direct-main acceptance comparison was made for these settings.

All three policies are rejected for the joint memory goal. No runtime.GC hook,
environment control or process-wide collection policy was installed in the
library. No new execution, concurrency or embedding qualification is claimed.
Exact final prototype sources, matched probe modules, two images, raw records,
build logs, commands and SHA256 manifest are saved in
`.validation/optstudy-phase-gc-screen.zip` and adjacent JSON. Qualified source
files and all 32 expected deletions were verified before saving; only this
journal changed. All owned build/measurement handles were observed terminal.

The qualified checkpoint remains the unused-hint removal: Go about 29% faster /
16% lower peak RSS, TinyGo compiler-only profile about 33% faster / 20% lower.
Goal ACTIVE. Collection of phase garbage is not the substantial missing RAM
mechanism. Next inspect separation of cold feature setup from the large ordinary
function compiler; distinguish it from the already rejected GC-callsite wrapper,
blanket noinline, decoder layout and module-argument-context trials. Any new
outlining screen must keep full feature behavior and frame/error ordering.

## Codex recovery

The active thread goal retains the full objective. If Codex stops, reopen the
same thread and use `/goal resume`, then inspect this checkpoint and the live
campaign before continuing. Official goal lifecycle documentation:
https://developers.openai.com/cookbook/examples/codex/using_goals_in_codex

This runner survives interruption through disk checkpoints. It does not restart
the Codex service or guarantee automatic recovery from model-capacity errors.
The older source archive `.validation/optstudy-exact-prefault-source.zip` records
the preceding checkpoint. The worktree itself is authoritative for the later
trap packing and rejected-emission cleanup. Sources remain
uncommitted so the user can review the complete candidate before publication.
