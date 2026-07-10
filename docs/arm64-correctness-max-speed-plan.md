# ARM64 correctness recovery and maximum-speed plan

## Implementation status — 2026-07-09

This plan has been carried through its correctness gate and the highest-return
parts of B1-B6. The accepted changes are:

- process-watchdog regressions for `spectralnorm`, `fannkuch`, and `sha256` in
  explicit and guard modes;
- fixed-register ownership for bulk-memory helpers (X9-X11 cannot hold pinned
  locals in a function using `memory.copy`/`memory.fill`);
- unary/conversion destination propagation, direct select-to-local sinking, and
  bounded pure-if arm sinking;
- table/import-free self-recursive memory functions retain local pins across the
  same-module register ABI (`memory_tree` median improved from about 18.0 µs to
  14.1 µs; wazero is 12.25 µs and WARP's recorded value is 14.87 µs);
- redundant extend/wrap chains are consumed directly by W-form ALU operations;
  and
- the native ARM64 entry/resume trampolines preserve the identical 176-byte save
  ABI while using paired GP/FP stores and loads. Tiny/branches/many-functions
  improved about 5%; dispatch improved about 15% in repeated sampling.

A direct host-to-internal-entry prototype was measured and removed: it regressed
tiny calls to ~42 ns and introduced an 8-byte allocation. A suffix live-local
scan around recursive calls was also removed after showing no runtime win. These
are recorded rejections, not dormant feature flags.

Current focused medians (500 ms ×5, Apple M4 Max, guard-page Wago):

| Workload | Wago | wazero | Remaining interpretation |
|---|---:|---:|---|
| `tiny.add` | 29.48 ns | 20.32 ns | exported-entry floor remains |
| `memory_tree.run` | 14.13 µs | 12.25 µs | Wago now beats WARP; more call-state work remains |
| `dispatch.apply` | 32.08 ns | 21.69 ns | body residual is small; entry floor dominates |
| `branches.classify` | 29.10 ns | 18.51 ns | entry floor dominates |
| `many_funcs.run` | 28.72 ns | 21.17 ns | entry floor dominates |

Correctness acceptance completed successfully:

```text
go test ./...
WAGO_CORPUS_TIMEOUT=15s go test -tags wago_guardpage -run '^TestCorpus$' -count=1 .
go test -tags wago_guardpage -run '^TestCorpusDifferential$' -count=1 .
make test-guard
git diff --check
```

The optional `wago_wasi` differential still has its pre-existing unreachable
traps in `crcsum`, `jsonproc`, and `regexmatch`; `bignum` passes with the accepted
pin gate. This optional suite is not part of the repository's `make test-guard`.

Date: 2026-07-09
Host used for the measurements below: Apple M4 Max, Darwin/arm64
Wago baseline: commit `9415112` plus the current uncommitted ARM64 WIP
WARP baseline: the intentionally patched `warp/build-bench/bin/vb_bench`

## Outcome

Work in two strictly ordered tracks:

1. restore correctness for `spectralnorm`, `sha256`, and `fannkuch`; then
2. optimize the remaining measured gaps using WARP and wazero as competing
   references, retaining the best ideas from each and adding ARM64-specific
   specialization where Railshot's one-pass model can do better.

No performance change lands while the three correctness failures remain. Every
optimization remains one-pass, bounded in analysis memory, A/B gated while it
is being evaluated, and limited to at most roughly 30% ARM64 compile-time cost.

## Measurement snapshot

Corpus values are medians of five 500 ms runs. Wago uses guard-page bounds.
WARP's patched harness also runs each export for 500 ms; its reported number
includes its public name lookup/call wrapper. Wazero's Go benchmark resolves the
function handle outside the timed loop, while Wago calls its cached `Invoke`
path. Because the public APIs differ, body-code decisions must also use a raw
native-entry benchmark and disassembly, not only these end-to-end numbers.

| Corpus export | Wago | wazero | WARP | Fastest | Main implication |
|---|---:|---:|---:|---|---|
| `tiny.add` | 31.40 ns | 20.03 ns | **15.19 ns** | WARP | Export entry/runtime floor |
| `memory_tree.run` | 18.073 us | **12.107 us** | 14.869 us | wazero | Recursive live-state/call handling |
| `dispatch.apply` | 36.98 ns | 21.25 ns | **16.04 ns** | WARP | Entry floor first, compact indirect dispatch second |
| `branches.classify` | 30.76 ns | 18.29 ns | **15.75 ns** | WARP | Almost entirely entry floor; body is only 132 bytes |
| `many_funcs.run` | 30.75 ns | 20.84 ns | **15.39 ns** | WARP | Calls are already inlined; this is also entry floor |
| `sieve.count` | **49.677 us** | 100.253 us | 52.067 us | Wago | Protect this win |
| `memory.sum` | **173.3 ns** | 290.0 ns | 219.0 ns | Wago | Protect guard-page memory lowering |
| `globals.accumulate` | **555.4 ns** | 658.2 ns | 1.730 us | Wago | Protect global residency/branch fold |
| `linked_list.sum` | **5.014 us** | 7.564 us | 10.195 us | Wago | Protect dependent-load performance |

The fixed Wago entry floor is independently visible in
`BenchmarkInvokeAddOne`: 29.81 ns median, zero allocations. `tiny`, `branches`,
and the fully inlined `many_funcs.run` all land at approximately that floor.

Selected ISA rows use the current full Wago/wazero sweep plus five WARP runs:

| ISA export | Wago | wazero | WARP | Best reference |
|---|---:|---:|---:|---|
| `i32.clz` | 28.001 us | 16.956 us | **16.827 us** | WARP |
| `i64.clz` | 28.013 us | 17.018 us | **16.834 us** | WARP |
| `i32.ctz` | 27.706 us | 25.652 us | **25.511 us** | WARP |
| `i64.ctz` | 27.722 us | 25.623 us | **25.534 us** | WARP |
| `select` | 31.313 us | 26.111 us | **26.058 us** | Both agree |
| `if_else` | 9.253 us | **8.352 us** | 16.969 us | wazero |
| `br_if` | 17.063 us | **16.612 us** | 17.082 us | wazero; small gap |
| direct call | **22.840 us** | 34.202 us | 41.680 us | Wago |
| indirect call | **34.292 us** | 48.491 us | 42.398 us | Wago |
| local get/set | **16.975 us** | 19.231 us | 24.927 us | Wago |
| `i64.extend_i32_u` | 22.976 us | **17.076 us** | unavailable | wazero |

WARP cannot compile `isa_cvt.wasm` because that module also contains
non-trapping float-to-int conversions WARP does not implement. For conversion
work, use wazero measurements and inspect WARP's applicable source lowering;
do not invent a WARP number.

## Track A: restore correctness

### A0. Freeze and characterize

The current parent/child corpus runner proves these failures without allowing a
bad native loop to hang the test process:

| Module | Explicit result observed now | Guard result observed now |
|---|---|---|
| `spectralnorm.run(128)` | `1000000000`, expected `1274222120` | same wrong value |
| `sha256.hashN(8)` | OOB trap | bad address can escape the managed reservation and reach Go's signal path |
| `fannkuch.run(8)` | hard timeout/infinite native loop | do not invoke in-process; use the parent watchdog |

The failures are present with the three newest branch/UXTW/store-forwarding
features disabled and clean `9415112` passes. The suspect set is therefore the
older uncommitted ARM64 WIP, not the empty-edge branch fold.

Before changing behavior:

1. Add these three cases as a dedicated `TestARM64WIPRegressions` parent/child
   group with a 5 s hard deadline and explicit expected outcomes. Migrate the
   existing in-process `TestCorpusExplicitFannkuch` to this watchdog path; in its
   current form that test can wedge its entire test binary.
2. Never run `fannkuch` through the in-process differential test while broken.
3. Capture `CodegenStats`, function code bytes, pin assignments, and a Wago
   disassembly for all three in explicit mode. Capture guard disassembly only
   after explicit mode is fixed; the common failure says this is not primarily
   a guard-page problem.
4. Confirm clean `9415112` and the current tree with identical corpus binaries
   and arguments. Save the output in the regression test/log, not as folklore.

### A1. Add isolation switches, then bisect mechanisms

Do not bisect whole files: `control.go`, `emit.go`, and `compile.go` each contain
both proven wins and suspect WIP. Add temporary, byte-inert fallback switches
around these individual changes and test all three modules after every switch:

1. **Expanded integer pin pool:** call-free functions now add `X24/X25`.
   `sha256` and `fannkuch` each report exactly 11 integer local pins, the new
   maximum, so this is the first suspect. Compare against the old pool.
2. **Expanded float pin pool:** `V8-V11` became `V8-V14`.
   `spectralnorm` reports 15 total local pins and is the strongest candidate for
   a float-register lifetime/alias failure. Compare against the old four-register
   pool.
3. **Three-operand local sink:** gate `tryThreeOperandLocalSink` independently.
   It fires 39 times in spectralnorm, 30 in sha256, and 47 in fannkuch.
4. **Broadened local overwrite skip:** separately gate the change from
   `!tee && (binary || shift)` to tee/unary-aware `skipFrom`. This changes the
   `local.get` snapshot invariant and must not be conflated with the instruction
   encoding optimization.
5. **Compare/tee/branch fusion:** retain the existing `WAGO_NO_STFLAGS` oracle,
   but separate `tryTeeCompareBrIf` if necessary so disabling all flag fusion is
   not the only diagnostic.
6. Keep `WAGO_ARM64_NOBRFOLD`, `WAGO_ARM64_NOUXTW`, and
   `WAGO_ARM64_NOSTLDFWD` off during this bisect. Loop-region pins are already
   opt-in and must stay off.

Run switches in a fresh child process because compiler environment variables are
parsed at package initialization. The first switch that restores a module gives
the mechanism; combinations are allowed only after single-switch runs, because
two incorrect mechanisms may mask each other.

### A2. Find the first wrong state, not only the final symptom

After isolating a mechanism:

- **spectralnorm:** export selected internal functions/iteration checkpoints and
  compare Wago with wazero. Inspect FP local values at the first divergent loop
  iteration. Focus on V-register pin ownership, low-64-bit preservation, and
  local overwrite realization.
- **sha256:** use the corpus runner's debug memory hash/range support and export
  the address-producing state immediately before the first bad load/store.
  Compare the i32 address and its zero-extension separately. An address outside
  the linear reservation indicates codegen corruption, not a missing bounds
  check.
- **fannkuch:** reduce the argument (`run(1)` upward) under the hard watchdog,
  then export loop counters and array indices. Find the first counter that stops
  progressing or the first wrong backedge condition.

For each, construct a small WAT/wasm regression that retains the necessary
register pressure: at least 11 hot integer locals for the GP-pool case or at
least five hot FP locals for the FP-pool case. A tiny test without pressure is
not sufficient.

### A3. Repair the invariant

Fix the actual ownership/liveness invariant instead of globally disabling the
optimization:

- a pinned register must be excluded from every transient allocator and from
  every other module/local role for its entire live range;
- `local.get` must retain the value at get-time across a later `local.set/tee`;
- a destination may alias a source only if the source's last read occurs in the
  same emitted instruction;
- 32-bit values must have an explicit W-register/zero-extension invariant at
  every 64-bit address consumer;
- every structured-control edge must reconcile the same local state.

Keep the isolation switch until the focused test, all three regressions, and the
full corpus pass. Then remove only switches whose fallback is no longer useful.

### A4. Correctness acceptance gate

Required before Track B changes may land:

```sh
go test ./...

cd bench
WAGO_CORPUS_TIMEOUT=15s go test -run '^TestCorpus$' -count=1 .
WAGO_CORPUS_TIMEOUT=15s go test -tags wago_guardpage -run '^TestCorpus$' -count=1 .
go test -tags wago_guardpage -run '^TestCorpusDifferential$' -count=1 .

cd ..
make test-guard
git diff --check
```

The guard `sha256` case must return a normal wasm trap for deliberately invalid
addresses and the correct hash for the corpus input; no Go runtime signal crash
is acceptable.

## Track B: maximum-speed optimization

### Working method for every workload

Use the same loop for every optimization:

1. benchmark Wago, wazero, and WARP with five matched samples;
2. subtract/measure the exported-entry floor with a raw native-entry benchmark;
3. disassemble the hot Wago internal entry and WARP body, and obtain wazero's
   machine/SSA dump where available;
4. count instructions, loads/stores, taken branches, call-site bytes, and live
   registers in the repeated region;
5. implement one gated change;
6. rerun correctness first, then the focused benchmark and control rows;
7. retain the change only for a stable execution win with acceptable code size
   and no more than 30% compile-time regression.

Do not copy either engine blindly. WARP is the better reference for Railshot's
deferred trees, target hints, compact wrappers, and one-pass register handling.
Wazero is the better reference for live-range coalescing, if-conversion, and
keeping values live across control/call boundaries. Wago should combine those
ideas without adopting an SSA tier.

### B1. Remove the exported-entry floor

**Targets:** `tiny`, `branches`, `many_funcs`, then `dispatch`.

Current Wago register-ABI exports are emitted as an offset-zero adapter which:

1. establishes module state;
2. pushes LR/results;
3. loads arguments;
4. `BL`s the internal entry;
5. restores LR/results and stores the result.

The runtime trampoline additionally saves/restores every AAPCS64 callee-saved GP
and FP register on every invocation, even when a tiny function uses none. WARP's
public path is roughly twice as fast, while wazero sits between WARP and Wago.

Implement in increasing ambition:

1. Add ARM64 raw `Engine.Call` and cached-`Invoke` component benchmarks so the
   Go marshalling, trap setup, trampoline, adapter, and body are separately
   measurable.
2. Move invariant per-instance work out of `Invoke`: the engine stack fence and
   stable trap-cell pointer should be installed once when safe; each call still
   clears the trap code. Preserve re-entry and concurrent-instance semantics.
3. Add a **fused export-only leaf entry**. A module scan already knows internal
   references. For a call-free export not referenced by wasm, emit one wrapper
   body instead of adapter + duplicated internal convention. Load wrapper args,
   run the body, store the result, return. Start with scalar, single-result,
   no-memory/no-global/no-table/no-trap functions.
4. Elide the native frame, stack-fence check, linMem setup, and unused module
   state for that strict pure-leaf class. `tiny.add`, `branches.classify`, and
   the inlined `many_funcs.run` are initial proofs.
5. Add a minimal `enterNativeLeaf` trampoline for code certified to use only
   caller-saved registers and not trap/call. It still switches to the foreign
   stack, preserves Go's `g`, and has a conservative fallback. This is Wago's
   original advantage: compile metadata lets the runtime avoid saving registers
   the function provably cannot touch.
6. If the binary-size trade remains favorable, generalize from two trampoline
   classes (minimal/full) to a small fixed family of save masks. Do not generate
   an unbounded trampoline cache.

Acceptance: `tiny.add <= 16 ns`, `branches.classify <= 16.5 ns`, and
`many_funcs.run <= 16 ns`, with zero allocations and unchanged trap/stack safety.

### B2. Propagate destinations through unary expression trees

**Targets:** `i32.clz`, `i64.clz`, then `ctz` and conversion expressions.

All three engines emit native `CLZ`; the opcode is not the problem. WARP passes
a target hint through `emitDeferredAction`, and wazero's allocator coalesces the
SSA result with its consumer. Wago's CLZ kernel is 484/500 bytes versus 228/244
bytes for the corresponding simple binary kernel and records only two
three-operand sinks: the nested unary RHS prevents the local sink from firing.
That leaves an extra move in each repeated `sub(other, clz(value))` chain.

Implement a bounded recursive destination-hint path:

- allow a local-set binary sink to condense a deferred unary RHS directly into
  a protected scratch register;
- emit `CLZ tmp, src; SUB dst, left, tmp` without first copying `left` to `dst`;
- for CTZ emit `RBIT tmp, src; CLZ tmp, tmp; SUB dst, left, tmp`;
- reuse the destination for the unary result only when alias/last-use analysis
  proves neither other source is destroyed;
- express this as a small `condenseInto`/target-hint capability, not a CLZ-only
  special case, so conversions and `abs/neg`-like unary nodes can benefit.

This takes WARP's local target hints and wazero's coalescing result, but uses
Railshot's existing Valent tree as the live-range proof.

Acceptance: CLZ within 2% of 16.83 us and CTZ within 2% of 25.51 us, with no
regression in add/sub/shift/popcnt or the new pressure regressions from Track A.

### B3. Sink `select` and if-convert simple result arms

**Targets:** `isa_ctl.select`, then `isa_ctl.if_else`.

WARP and wazero independently converge at about 26.1 us for `select`. Both use
flags plus `CSEL`; WARP also passes a destination hint, while wazero can retain
the compare flags and coalesce values globally. Wago already emits CSEL but is
20% slower, so inspect for result moves and repeated materialization around the
pinned `$acc` destination.

1. Let `local.set $acc (select ...)` pass `$acc`'s pin as the select target.
2. Materialize both arms with the destination protected and emit CSEL directly
   into it; recognize shared borrowed sources so `acc+7`, `acc-3`, and `acc&1`
   do not cause defensive copies.
3. Preserve compare-to-flags fusion when the condition is relational.
4. Then add bounded **if-conversion** for a single-result integer `if` whose two
   arms are short, side-effect-free, non-trapping Valent trees. Emit both arms +
   CSEL only when a simple Apple-M4 cost model predicts fewer/more predictable
   cycles than branching. Stores, calls, memory traps, division traps, and
   multi-value arms remain on structured control flow.

This keeps WARP's deferred select semantics, takes wazero's profitable
if-conversion, and adds a one-pass ARM64 cost gate.

Acceptance: select within 2% of 26.06 us; if/else within 5% of wazero's 8.35 us;
no change to NaN/trap ordering or multi-value control tests.

### B4. Preserve only live state across recursive calls

**Target:** `memory_tree.run`.

Wazero wins this workload even though Wago wins the isolated direct-call ISA
row. Wago's recursive body reports two register-ABI calls, call-adjacent flushes,
and pinned-local state traffic. WARP's local-save model is faster than Wago but
still behind wazero's live-range allocation.

Use a bounded bytecode suffix scan to classify pinned locals live after each
direct call:

1. spill only dirty pins that are read after the call;
2. mark dead pins clobbered without writing them to the frame;
3. pair adjacent GP/FP saves with `STP/LDP` where profitable;
4. retain the current spill-all STACK_REG path for indirect calls, imports,
   ambiguous control flow, or analysis budget exhaustion;
5. compare with an alternative callee-save placement (save once in the callee
   prologue rather than around every caller site) only if the first disassembly
   shows repeated equivalent saves. Choose the lower dynamic instruction count
   and smaller code shape.

Acceptance: first beat WARP's 14.87 us, then reach within 5% of wazero's
12.11 us, without regressing `fib_rec`, direct/indirect-call ISA, or code size by
more than 10% on call-heavy corpus modules.

### B5. Compact the same-instance `call_indirect` hot path

**Target:** residual `dispatch.apply` after B1.

Do not start here before subtracting the entry floor: Wago already beats both
engines on repeated ISA indirect calls. The corpus call site is currently 448
bytes and the 16-site ISA indirect function is 4044 bytes because every site
contains both the same-instance internal path and a large wrapper/cross-instance
fallback.

Reference designs:

- WARP: compact table bounds/type/null checks followed by an internal BLR;
- wazero: load a function-instance pointer, check its type ID, then call its
  executable pointer with a uniform ABI;
- Wago today: tagged internal-entry/home metadata plus duplicated hot/cold paths.

Best-of-both design:

1. Keep the existing 32-byte table entry and full wasm checks, but pack the hot
   same-instance flag with aligned code-pointer metadata and use `TBZ/TBNZ`
   rather than `AND+CMP+B.cond` where possible. Clear any pointer tag before
   BLR and audit Darwin arm64 pointer-authentication assumptions.
2. Arrange hot fields for `LDP`/adjacent loads: code, packed type/flags, and home
   context. Preserve the canonical type check on every call.
3. Outline cross-instance/host/wrapper handling into one shared module thunk or
   cold stub instead of cloning it at every call site. Trap stubs remain shared.
4. Avoid the current double canonicalization: stage args once and let both paths
   consume the same state without cloning allocator/local metadata.
5. Consider an inline cache only afterward, guarded by a bounded table mutation
   epoch. It must show a real repeated-target workload; `dispatch.apply` alone
   does not justify cache memory.

Acceptance after B1: `dispatch.apply <= 17 ns`; keep indirect-call ISA at least
10% faster than both competitors; materially reduce bytes per indirect site.

### B6. Track 32-bit cleanliness and fold extensions

**Target:** `i64.extend_i32_u` and address/conversion consumers.

Wazero carries width facts through SSA and folds zero-extension into suitable
ARM64 consumers. Wago's UXTW-add peephole is correct but nearly neutral and
matches only one tree shape.

Add a compact fact to integer storage/deferred nodes: low-32 canonical, known
zero-extended, known sign-extended, or unknown. Propagate it through W-register
producers, locals, comparisons, and safe masks. Then:

- remove redundant explicit zero-extends;
- select W-register producers when their architectural zeroing establishes the
  fact for free;
- fold `UXTW/SXTW` into add/address modes and conversions;
- invalidate facts at merges/calls unless every incoming value agrees.

The fact is O(values/locals), uses the existing one-pass control state, and is
more general than adding isolated peepholes.

Acceptance: `i64.extend_i32_u` within 2% of wazero's 17.08 us, no address-width
regressions, and all explicit/guard memory differentials green.

## Priority and stop conditions

Execute in this order:

1. A0-A4: correctness and safety;
2. B1: export-entry floor (benefits four losing corpus rows at once);
3. B2: unary destination propagation (largest ISA regression);
4. B3: select sink and guarded if-conversion;
5. B4: recursive live-state preservation;
6. B5: compact indirect dispatch, only after measuring its post-B1 residual;
7. B6: width/extension facts;
8. rerun all corpus and all ISA, then reprioritize only measured gaps.

Stop or revert an experiment when:

- it cannot beat the fastest applicable reference after disassembly-equivalent
  code shape is reached;
- it shifts cost into a larger corpus regression;
- compile time rises more than 30%;
- per-module metadata/cache memory becomes unbounded or materially larger than
  the code it saves;
- the correctness fallback cannot be stated and tested simply.

## Benchmark and reporting protocol

Focused Wago and wazero corpus runs:

```sh
cd bench
WAGO_BOUNDS=signals go test -tags wago_guardpage -run '^$' \
  -bench '^BenchmarkExec/(tiny\.add|memory_tree\.run|dispatch\.apply|branches\.classify|many_funcs\.run)$' \
  -benchmem -benchtime=500ms -count=5 .
go test -run '^$' \
  -bench '^BenchmarkWazeroExec/(tiny\.add|memory_tree\.run|dispatch\.apply|branches\.classify|many_funcs\.run)$' \
  -benchmem -benchtime=500ms -count=5 .
```

Full ISA comparison:

```sh
WAGO_BOUNDS=signals go test -tags wago_guardpage -run '^$' \
  -bench '^BenchmarkExec/isa_' -benchmem -benchtime=500ms -count=5 \
  -wago.bench.isa .
go test -run '^$' -bench '^BenchmarkWazeroExec/isa_' \
  -benchmem -benchtime=500ms -count=5 -wago.bench.isa .
```

WARP focused execution uses:

```sh
warp/build-bench/bin/vb_bench bench/corpus/<module>.wasm <export> <args...>
```

Run it five times and take the median. Preserve `warp/` exactly as-is: its dirty
patched harness and code-dump support are intentional.

For every accepted phase, record:

- five-sample median and min/max for Wago, wazero, and applicable WARP rows;
- raw entry cost and body-adjusted interpretation;
- generated bytes and repeated-loop/call-site instruction count;
- loads, stores, taken branches, spills/reloads/flushes, and pin count;
- compile time and peak/allocated bytes;
- complete correctness commands and any tests not run.

The final target is not merely aggregate parity. Every applicable remaining
row should match or beat the fastest reference unless a documented safety or
memory constraint makes that impossible—and Wago's existing sieve, memory,
globals, linked-list, calls, and locals wins must remain wins.

## 2026-07-10 execution-gap closeout

The focused Darwin/arm64 pass completed the corpus half of this plan. Numbers
below are medians from fresh-process `-benchtime=1s -count=5` runs on an Apple
M4 Max, guard-page Wago versus wazero compiler mode. Negative delta means Wago
is faster. Wago reported zero allocations on every row; wazero reported one.

| workload | Wago | wazero | Wago delta |
|---|---:|---:|---:|
| `tiny.add` | 19.76 ns | 19.60 ns | +0.8% |
| `memory_tree.run` | 7.163 us | 11.766 us | **−39.1%** |
| `globals.accumulate` | 536.2 ns | 621.2 ns | **−13.7%** |
| `dispatch.apply` | 21.34 ns | 20.79 ns | +2.6% |
| `branches.classify` | 19.97 ns | 17.63 ns | +13.3% |
| `many_funcs.run` | 20.05 ns | 20.64 ns | **−2.9%** |

`branches.classify` is now effectively Wago's host-entry floor: it is only
0.21 ns slower than `tiny.add`, so the remaining cross-engine percentage is not
a material generated-branch cost. `dispatch.apply` is within 0.55 ns of wazero;
its residual is the dynamic table bounds/null check plus `BLR`.

Accepted changes and isolated measurements:

- Bind the trap cell once for instance-owned memory and call `Engine.CallPrepared`.
  Bypassing the non-inlined generic `callNative` router after that ownership
  proof changed `BenchmarkInvokeAddOne` from 25.81 ns to 21.09 ns (−18.3%).
- Use backing-pointer identity before string content comparison in the tiny
  export cache. Repeated literal-name invokes changed from 21.09 ns to 20.40 ns
  (−3.3%); dynamically rebuilt equal strings retain the content fallback.
- Specialize immutable, private, local, import-free tables to the internal
  register ABI. Exported tables are excluded because another importing instance
  can mutate their shared descriptor.
  `dispatch.apply` measured 27.45 ns versus 28.16 ns with the specialization
  disabled (−2.5%) before the entry-floor work. A uniform active-element type
  proof also removes two redundant instructions; its isolated timing was neutral
  (21.34 versus 21.35 ns) but reduces code and register pressure.
- Forward an exact full-width linear-memory store into a following same-local,
  same-offset load across at most three `local.get` leaves. The window is limited
  to functions with at most eight locals and 256 body bytes, is invalidated by
  every other opcode, and has `WAGO_ARM64_NOMEMFWD=1` as an oracle. This removes
  the serialized store/load dependency in every `memory_tree` loop iteration:
  7.07 us versus 12.85 us disabled (about −45%). The initial unbounded window
  exposed register exhaustion in Ruby function 10555; the bounded lookahead and
  pressure gate fixed it, and the full corpus then passed.
- Small immediate frame adjustments, register-only frame elision, and planning
  functions whose calls all inline as call-free remain enabled. They primarily
  reduce code size and entry/call overhead; see their individual kill switches
  in `arm64/compile.go`.

The forwarding lookahead did not consume the allowed compile-time budget:
`BenchmarkCompile/memory_tree` was 10.33 us enabled versus 10.51 us disabled
(noise-level −1.7%), with the same 30,194 bytes and 90 allocations per compile.

Measured and rejected:

- Replacing the immutable table-length load with an immediate comparison moved
  the descriptor load behind the bounds branch and regressed dispatch by about
  1.9% (26.45 versus 25.95 ns); reverted.
- Returning the trap code from the assembly trampoline regressed the entry path
  by about 3.5%; reverted in favor of the prepared Go-side cold-cell read.

Correctness at closeout:

```sh
GOCACHE=/tmp/wago-gocache go test ./...
cd bench
GOCACHE=/tmp/wago-gocache WAGO_CORPUS_TIMEOUT=20s \
  go test -tags wago_guardpage -count=1 -run '^TestCorpus$'
```

Both passed. The root suite needs permission to bind a localhost `httptest`
listener for `cli/wagocli`; its first sandboxed run failed only that environmental
operation and the unrestricted rerun passed. Focused tests cover mixed-signature
table fallback, table-mutation proof rejection, store-forward exact matching and
address-change invalidation, prepared-call traps, and explicit/guard table and
memory semantics.
