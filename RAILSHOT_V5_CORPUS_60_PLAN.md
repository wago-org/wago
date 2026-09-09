# Railshot V5 corpus performance plan

Status: wrapped for review on current main. Retained candidates pass correctness
and resource gates. The 1.60x absolute stretch target is not met: final paired
screens measure 1.370086x on ARM64 and 1.555391x on AMD64 versus wazero.

Original experiment base: `915615e1bc8d9000e2fe7b3e775366fc19c24fd4`
(the pre-review head of PR #570).

Measurement base: `215b02287d2528bd0a9d05636664ae549bb2cd4e`
(`origin/main` after PRs #570 and #564). The review branch is rebased onto
`2025c38e687a904527c323ce274d0096fd071f55`; its intervening release-script-only
change does not affect runtime code or the recorded measurements.

## Objective

Raise the complete paired 46-row execution geomean to at least 1.60x
wazero/Railshot on both ARM64 and AMD64. Preserve at least a 2.60x advantage
for full compilation across the 17 application rows, with at least 12x lower
compiler bytes/op on ARM64 and 9x lower compiler bytes/op on AMD64.

Correct Wasm semantics, trap order, cancellation, precise GC roots, descriptor
ownership, instance isolation, bounds checks, and Reference Lifetime rules are
hard constraints. Optimization predicates must be semantic or architectural;
module names, exports, hashes, source languages, corpus membership, and
benchmark-derived constants are forbidden.

Railshot remains a forward single-pass compiler. Bounded pointer-free hint
records and bounded finalization over already-recorded sites are allowed. A
second Wasm walk, CFG/SSA construction, arbitrary native-code decoding, and
unbounded compiler state are not.

## Measurement contract

- Compare complete paired rows on the same host, Go toolchain, corpus bytes,
  wazero version, signal-backed bounds mode, and `GOMAXPROCS=1`.
- Pin AMD64 measurements to CPU 7 and serialize timed work per host.
- Take the median of at least three samples per row, then aggregate ratios with
  a geometric mean. Use five or more for final promotion when time permits;
  report all losses and retain raw captures.
- Use a short representative promotion panel while iterating. Promote only
  after semantic tests, adversarial near-miss tests, the complete execution
  corpus, compile/resource gates, and bounded native-size checks pass.
- Every substantial optimization gets an independent Optimization Definition
  and disabled-path tests. Change one variable per benchmark comparison.

## Exact baseline

At `915615e1`, five 300 ms samples per execution row:

| Host | wazero/Railshot geomean | Wins | Raw capture |
|---|---:|---:|---|
| Apple M4 Max, Go 1.26.5 | 1.431682x | 39/46 | `/private/tmp/wago-v5-base-arm-exec-5x.txt` |
| Ryzen 7 7800X3D CPU 7, Go 1.22.2 | 1.401881x | 34/46 | `/private/tmp/wago-v5-base-amd-exec-5x.txt` |

The 1.60x gate therefore requires another 11.76% ARM64 and 14.13% AMD64
geomean uplift relative to the exact branch base.

## Ranked hypotheses

1. **Phase-sensitive local residency.** Existing shadow telemetry on ARM64
   raytrace reports 281 projected avoided loads across 166 profitable segments.
   Convert only proof-complete, bounded segments into lowering policy. Reject if
   a mixed compute panel does not improve by at least about 3% or if sync debt,
   pressure debt, code size, or compilation resources erase the benefit.
2. **AMD64 bounded call-graph entry propagation.** `dispatch.apply` measures
   about 10.5 ns on ARM64 and 92.1 ns on AMD64. Port the ARM64 leaf-first,
   depth/work-capped proof for acyclic local calls and private immutable tables;
   retain every dynamic type, descriptor kind/home, ownership, and table proof.
   Reject if focused latency does not collapse or any recursive, imported,
   mutable-table, `call_ref`, memory, EH, custom, or oversized near miss enters
   the bounded route.
3. **Call result and destination coalescing.** Reduce moves only when liveness,
   result type, ABI bank, and clobbers are exact. Reject on any GC/EH/multi-value
   discrepancy or if call-heavy and compute panels do not both benefit.
4. **Semantic machine-cover expansion.** Add only target-capability covers with
   independent-producer hits and adversarial near misses. Reject corpus-shaped
   patterns and any cover that is flat across its affected/control panel.

## Experiment ledger

Record each attempted change here with exact commits, toggles, focused results,
full-corpus promotion results, resource effects, and the reason it was retained
or rejected. Do not combine unattributed experiments.

- **AMD64 bounded immutable-table dispatch (retained, uncommitted).** Extends
  the existing `prepared-bounded-entry` proof with depth-32/work-4KiB,
  leaf-first propagation through private immutable tables. Recursive SCCs,
  imports, memory, mutable/exported tables, `call_ref`, GC/EH helpers, custom
  instructions, and representation overflows fail closed. On the Ryzen host,
  `dispatch.apply` improved from about 92.28 ns to 10.06 ns (7 x 300 ms), and a
  complete 46-row 3 x 200 ms promotion screen moved the wazero/Railshot
  geomean from 1.401881x to 1.484906x. Full five-sample gates remain pending.
- **ARM64 direct SIMD comparison results (retained, uncommitted).** Completes
  the existing `v128-direct-results` scope for three-register compare forms.
  Isolated signed/unsigned comparison rows improved about 2.3--2.6x and `ne`
  rows about 1.6--1.7x. Six alternating complete-corpus Wago-only rounds were
  neutral (candidate/base geomean 0.999925x), so this broad machine-cover win
  does not yet advance the 46-row application gate.
- **ARM64 expire-at-every-control-edge residency (rejected).** An experimental
  bounded prototype materialized every lease before all structured edges. Five
  100 ms focused samples left fannkuch unaffected and moved raytrace from a
  271.7 us median to 272.4 us (about 0.3% slower). Edge synchronization erased
  the predicted benefit; the prototype was removed. Any retry must retain the
  planner's profitable version segments and avoid whole-region writeback.
- **AMD64 adjacent local-store forwarding (rejected).** A one-op exact-local
  prototype retained one owned integer result after its canonical frame store.
  It fired in several ordinary functions but not in the Blake compression body
  that motivated it. Seven 300 ms samples were flat for arith and Blake-AS and
  about 0.2% slower for reference Blake3, so the prototype was removed.
- **AMD64 interval equal-score last-use tie-break (rejected).** Preferring the
  lease with the furthest final use as an equal-hotness eviction victim moved
  reference Blake3 about 0.4% faster but Blake-AS about 0.4% slower; arith and
  SHA-256 were flat. The mixed trade did not improve the broad gate and was
  removed.
- **AMD64 tenth signals-mode regional lease (rejected as unsafe).** Raising the
  signals-mode limit from nine to ten admitted RSI and initially appeared to
  improve Blake workloads. A later guard-page differential run proved the
  change corrupt: `blake-as` changed from 2973751372 to 877833372. The original
  nine-register limit is restored; native register-count arguments are never a
  substitute for the execution oracle.
- **AMD64 bounded local-only move forwarding (rejected).** Extending the
  adjacent-forward prototype across at most four local-only opcodes produced a
  repeatable roughly 0.9% Blake-AS win, but a 46-row 3 x 150 ms screen moved
  candidate/base geomean only 1.00056x and showed mixed sub-percent shifts.
  The extra compiler state was removed as unjustified for the aggregate return.
- **AMD64 sparse wide-loop constant cache (retained, uncommitted).** The existing
  one-pass byte scanner selects at most two non-imm32 i64 constants consumed by
  loop add/sub/mul/and/or/xor operations. Call-free functions preload them only
  into registers left idle by local/global/module roles; the sparse sidecar does
  not grow the fixed hint header. Seven 300 ms samples improved `arith` about
  13%, Blake-AS SIMD about 0.7%, and raytrace about 0.4% without a focused loss.
  A complete 46-row 3 x 200 ms screen moved AMD64 wazero/Railshot geomean from
  1.487395x to 1.499132x (35/46 wins). Raw captures are
  `/tmp/wago-v5-loopconst-{off,on}.txt` and
  `/tmp/wago-v5-amd-loopconst-full-screen.txt` on the Ryzen host.
  The first cross-platform CI run later found deterministic `xjb-mulhi.runN`
  corruption on Darwin/AMD64 and Windows/AMD64; the codegen report showed this
  cache firing twice while the other new fixed-register and BMI2 paths did not
  fire. It therefore defaults off outside its qualified Linux/AMD64 target and
  remains explicitly selectable for future platform qualification.
- **AMD64 ninth whole-function leaf pin (rejected).** Allowing RDI to hold one
  additional hot local in call-free functions preserved safety but was flat on
  Blake3, float, globals, and raytrace; Blake-AS and arith moved about 0.2--0.3%
  slower while Blake-AS SIMD moved 0.4% faster. The mixed noise-level result did
  not justify reducing transient headroom, so the change was removed.
- **ARM64 true-aligned loop phase (rejected).** Removing the established
  four-instruction post-alignment phase made nearly the entire focused panel
  slower: Blake-AS SIMD about 4.5%, globals 2.5%, float 1.9%, and most remaining
  rows 1--2%. The historical half-fetch-block phase was restored.
- **ARM64 exact countdown backedge (rejected).** A semantics-gated prototype
  retained the initial zero guard and replaced an exact terminal decrement plus
  unconditional backedge with a conditional branch past the repeated guard.
  Interruptible loops were excluded and semantic/differential corpus tests
  passed. It improved globals 1.5% and float/arith about 0.8%, but regressed
  nbody about 5% and several longer loops about 1%; target branch behavior erased
  the structural saving, so the prototype and its classifier were removed.
- **ARM64 expanded call-free FP-local pins (rejected).** Raising the existing
  `ext-fp-pins` call-free cap from 15 to 23 left nine transient vector registers
  and passed the backend and semantic corpus tests, but seven alternating 300 ms
  samples across 14 compute rows produced a 0.998303x candidate/base geomean.
  Nbody improved 1.9%, while Mandelbrot regressed 3.1% and raytrace 2.1%; the
  original 15-register cap was restored. Raw captures are
  `/private/tmp/wago-v5-arm-fp23-{base,cand}.txt`.
- **ARM64 two-instruction loop phase (rejected).** Moving eligible poll-free
  loop headers from the established four-instruction fetch phase to a
  two-instruction phase split the corpus: five alternating 200 ms rounds made
  arith 4.3%, SHA-256 2.4%, and matmul 1.8% faster, but nbody 2.0%, CoreMark
  1.8%, and Blake-AS 1.4% slower. The complete 46-row candidate/base geomean was
  0.999500x, so the established four-instruction phase was restored. Raw
  captures are `/private/tmp/wago-v5-arm-loopphase2-{base,cand}.txt`.
- **ARM64 deferred scalar-float comparison flags (rejected).** A prototype kept
  exact f32/f64 comparisons deferred so branch and integer-select consumers
  could use FCMP flags directly; standalone booleans retained CSET, the existing
  flag-neutral edge proof was reused, and semantic plus explicit/signal corpus
  tests passed. An exact same-binary, AB/BA, eight-sample 300 ms panel made
  Mandelbrot 0.7% faster and spectralnorm flat, but raytrace remained flat and
  nbody regressed 0.7% (panel geomean 0.999670x). The mechanism and its dedicated
  rollback option were removed. Raw captures are
  `/private/tmp/wago-v5-arm-fcmpflags-toggle-{off,on}.txt`.
- **ARM64 emitted no-trap prepared subset (rejected before timing).** A prototype
  derived a bounded/light/no-trap marker from the backend's emitted trap-site
  ledger so prepared calls could omit trap-buffer reset/read traffic without
  changing the foreign-stack or lifecycle boundaries. On Darwin/ARM64 the
  cooperative entry-interruption check is necessarily a trap edge because host
  asynchronous interruption is unavailable, leaving the intended benchmark
  subset empty. Skipping that edge would weaken cancellation behavior, so the
  metadata and wrapper shortcut were removed.
- **Compile/resource gate screen (passing).** A complete 17-application,
  three-sample, 100 ms `CompileFull`/`WazeroCompile` screen measured a 2.854028x
  latency advantage and 15.472285x fewer bytes/op on the Apple M4 Max, and a
  2.999261x latency advantage and 9.562290x fewer bytes/op on the Ryzen host.
  All four requirements remain above the 2.6x/12x/9x gates. Raw captures are
  `/private/tmp/wago-v5-current-{arm,amd}-compile-3x.txt`.
- **ARM64 four-register float-constant cache (rejected).** Increasing the
  allocation-free per-function floating-constant residency ceiling from two to
  four registers was safe and target-bounded, but six alternating 300 ms rounds
  across ten float and compression rows produced a 0.998459x candidate/base
  geomean. Mandelbrot regressed 1.3%, raytrace 0.4%, and no row improved 0.5%;
  the two-register pressure balance was restored. Raw captures are
  `/private/tmp/wago-v5-arm-fconst{2,4}-panel-v2.txt`.
- **ARM64 equal local-read/write pin weight (rejected).** Reducing local writes
  from two score units to one tested whether whole-function pin selection was
  overvaluing definitions. Four alternating complete-corpus 150 ms rounds
  produced a 0.994693x candidate/base geomean, including regressions in the
  branch, many-function, and mulhi probes. The established write weight was
  restored. Raw captures are
  `/private/tmp/wago-v5-arm-localweight{2,1}-full-v2.txt`.
- **AMD64 compact 32-byte loop-header alignment (retained, uncommitted).** An
  unrestricted exact-32 prototype improved tiny loops but split larger ones.
  Restricting it to functions no larger than 64 Wasm body bytes gives compact
  loops a complete fetch block while retaining the lower-padding established
  policy everywhere else. A same-binary, five-round alternating 300 ms full
  corpus run improved `globals` 1.8509x, iterative Fibonacci 1.0677x, and
  json-as SIMD serialization 1.1581x; no other row moved 2%, and the complete
  candidate/base geomean was 1.014756x. The option has an environment rollback
  and per-compilation positive/negative tests. Raw captures are
  `/private/tmp/wago-v5-amd-compactalign-{off,on}-full-v7.txt`.
- **ARM64 compact 32-byte loop-header alignment (rejected).** The matching
  small-function fetch-block rule preserved the cooperative poll and loop
  semantics, but a same-binary five-round alternating 300 ms full-corpus run
  was neutral at 1.000169x candidate/base. SHA-256 improved 7.3% and json-as
  SIMD deserialization 3.8%, while linked-list traversal regressed 3.1%; the
  architecture's established 16-byte plus poll-phase policy was restored. Raw
  captures are `/private/tmp/wago-v5-arm-compactalign-{off,on}-full-v9.txt`.
- **AMD64 stable top-K regional leases (rejected).** Capping the regional
  candidate set to the ten globally hottest locals removed ownership churn but
  also prevented profitable late-version admissions. Eight alternating 300 ms
  focused rounds regressed scalar Blake-AS 4.5% and each Blake3 mode about
  7.2%; the complete seven-row panel geomean was 0.961760x. Dynamic bounded
  reuse was restored. Raw captures are
  `/private/tmp/wago-v5-amd-interval-{dynamic,topk}-focus.txt`.
- **ARM64 static FP-pin pressure sweep (rejected).** The architecture-derived
  call-free scalar FP pin cap was swept from the established 15 to 12 and 18
  across six float/SIMD-heavy application rows. Five alternating 300 ms rounds
  produced candidate/base panel geomeans of 1.000283x and 0.999917x;
  raytrace was flat in both directions. Static register pressure is therefore
  not the missing broad gain, and the 15-pin balance was restored. Raw capture
  is `/private/tmp/wago-v5-arm-fpcap-panel.txt`.
- **ARM64 FP read-weighted pin ranking (rejected).** A bounded ARM-only hint
  sidecar preserved weighted read hotness during the existing fused scan, then
  ranked FP locals by `2R+W` instead of the established `R+2W`; integer
  allocation was unchanged. Six alternating 300 ms rounds improved Mandelbrot
  0.9%, but regressed Blake3 1.2% and raytrace 0.7% (panel geomean 0.998811x).
  The sidecar and policy were removed. Raw capture is
  `/private/tmp/wago-v5-arm-fpread-panel.txt`.
- **Exact current AMD64 gate and matched-base refresh.** A complete 46-row,
  five-sample 300 ms current run measured 1.422054x wazero/Railshot with 37/46
  wins. A fresh three-sample 200 ms rerun of exact #570 measured 1.368902x on
  the same host, confirming about 3.9% aggregate retained uplift while also
  invalidating the earlier 1.52x estimate formed by multiplying measurements
  from different thermal/frequency windows. Raw captures are
  `/private/tmp/wago-v5-amd-current-full-5x.txt` and
  `/private/tmp/wago-v5-amd-base-rerun-3x.txt`.
- **AMD64 high-offset equal-score lease tie-break (rejected).** An encoding-cost
  tie-break allowed an equally hot local above the disp8 frame window to replace
  a low-offset lease, only in the one-way direction that prevents oscillation.
  Eight alternating 300 ms focused rounds made scalar Blake-AS 1.4% slower and
  produced a 0.998933x six-row geomean, so the established hysteresis was
  restored. Raw capture is `/private/tmp/wago-v5-amd-offsettie-panel.txt`.
- **AMD64 blanket BMI2 rollback (rejected).** Disabling non-destructive RORX on
  the Ryzen made every rotate-heavy row slower: scalar Blake-AS 3.5%, SIMD
  Blake-AS 2.4%, SHA-256 4.4%, and Blake3 about 0.8%; the eight-row geomean was
  0.984466x. BMI2 remains the default, while a narrower source-equals-destination
  encoding experiment is tracked separately. Raw capture is
  `/private/tmp/wago-v5-amd-bmi2-panel.txt`.
- **AMD64 destination-known legacy rotates (rejected).** Using compact
  destructive ROR/ROL when lowering already had a requested destination made
  Blake3 about 0.8% faster, but scalar Blake-AS 5.4%, SIMD Blake-AS 6.4%, and
  SHA-256 2.0% slower. The eight-row geomean was 0.985572x, so uniform
  host-capability-selected RORX was restored. Raw capture is
  `/private/tmp/wago-v5-amd-ror-dest-panel.txt`.
- **ARM64 medium-loop fetch phase (rejected).** Extending the established
  four-instruction poll-free loop phase from functions with at most 16 locals
  to those with at most 32 improved some layout-sensitive tiny rows, but a
  complete four-round 150 ms screen regressed ordinary memory, compression,
  arithmetic, and recursive rows. The 46-row candidate/base geomean was
  0.996412x, so the 16-local footprint bound was restored. Raw capture is
  `/private/tmp/wago-v5-arm-phase32-full.txt`.
- **ARM64 cached-poll NOP hoist (rejected).** Moving the trap-cell cache's
  phase-preserving NOP before the loop target kept total code size and body
  addresses unchanged while removing one instruction from every backedge.
  A same-binary five-round 200 ms full-corpus A/B was only 1.0032x overall and
  significantly regressed `memory_tree` by 1.96%. Splitting the change by the
  two possible post-alignment header phases was flat: phase 0 measured
  0.9992x and phase 16 measured 0.9997x candidate/base. The code, catalog flag,
  and structural test were removed. Raw captures are
  `/private/tmp/wago-v5-arm-poll-layout-ab.txt`,
  `/private/tmp/wago-v5-arm-poll-layout-phase0-ab.txt`, and
  `/private/tmp/wago-v5-arm-poll-layout-phase16-ab.txt`.
- **AMD64 legacy-first regional leases (rejected).** Preferring the cheaper
  legacy-encoded `RDI`, `RSI`, and `RBP` before extended regional registers made
  all three reference Blake3 modes 0.9--1.2% faster in a seven-round panel, but
  left scalar and SIMD Blake-AS flat. A complete three-round 200 ms corpus
  screen improved only 0.07%, below the retention bar. Promoting only `RDI` and
  `RSI` instead regressed scalar Blake-AS 0.74% significantly. Both register
  orders were removed. Raw captures are
  `/private/tmp/wago-v5-amd-legacyregion-panel.txt`,
  `/private/tmp/wago-v5-amd-legacyregion2-panel.txt`, and
  `/private/tmp/wago-v5-amd-legacyregion-full.txt`.
- **AMD64 bounded next-use regional eviction (rejected).** The existing local
  event scan was converted into bounded pointer-free `uint16` next-access links
  so equal-hotness leases yielded only to a sooner reuse. Seven alternating
  300 ms rounds improved reference Blake3 by 0.5--0.6%, but regressed scalar
  Blake-AS 0.68% significantly and left the panel geomean flat. The additional
  sidecar and worker scratch would also consume the narrow AMD64 compile-bytes
  margin, so the prototype was removed. Raw capture is
  `/private/tmp/wago-v5-amd-nextuse-panel.txt`.
- **AMD64 regional-cache ablation (diagnostic).** Disabling the existing cache
  in the same binary regressed scalar Blake-AS 12.0%, SIMD Blake-AS 8.5%, and
  reference Blake3 about 17%, for a 0.8782x off/on panel ratio. Residency is
  therefore high-value; the remaining gap is not caused by cache overhead.
  Raw capture is `/private/tmp/wago-v5-amd-interval-ablation.txt`.
- **AMD64 proof-gated eleventh R8 lease (rejected).** A scan-derived fixed-R8
  proof admitted the normally reserved final regional register only in
  call-free, control-free, non-bulk, memory32 functions without tables, GC,
  atomics, or custom instructions. Seven 300 ms rounds were flat at 1.0003x,
  and explain counters confirmed no target function exceeded nine active
  leases. The unused safety-sensitive proof and flag state were removed. Raw
  capture is `/private/tmp/wago-v5-amd-r8lease-panel.txt`.
- **AMD64 hot-first compact-i32 homes (rejected).** Bounded all-i32 functions
  assigned frame homes in descending existing local-hotness order, preserving
  local identity and total frame bytes while favoring disp8 encodings. The full
  backend suite passed and explain confirmed the path fired, but seven 300 ms
  rounds were flat at 1.0006x across Blake, arithmetic, float, and raytrace.
  The sort and worker scratch were removed. Raw capture is
  `/private/tmp/wago-v5-amd-hotslots-panel.txt`.
- **AMD64 canonical i32 carriers (retained, uncommitted).** Memory-heavy native
  inspection found repeated `mov r32,r32` cleanup before address formation even
  when a regional pin or spill reload already established zero upper bits. The
  retained rule uses only storage/lowering facts: regional pins elide directly;
  call-making whole-function pins remain conservative; call-free pins wait for
  three eligible uses to amortize the code-shape change; and i32 spill reloads
  use their value width. The state is one saturated byte per active compiler
  function and the public `canonical-i32` option provides rollback. AMD64
  backend plus semantic corpus tests pass. A five-sample 300 ms end-to-end run
  measured 1.439407x wazero/Railshot across 46 rows with 38 wins, versus
  1.422054x before this change. Raw captures are
  `/private/tmp/wago-v5-amd-canonical-i32-{panel,full-screen,regressions,regional-panel,callfree-panel,threshold-panel,final-full-screen}.txt`
  and `/private/tmp/wago-v5-amd-canonical-current-full-5x.txt`.
- **AMD64 canonical-carrier layout debt (rejected).** Preserving the fetch-block
  phase that preceded removed address-cleanup instructions made quicksort about
  10% slower, worse than the unpadded prototype. The assembler/compiler debt
  mechanism was fully removed; the retained profitability boundary above avoids
  the affected call-making whole-function pins instead.
- **AMD64 direct frame-local self updates (rejected).** A semantics-gated
  prototype lowered unpinned `local.set x (local.get x op y)` forms for
  add/sub/and/or/xor to x86 memory-destination ALU instructions. After excluding
  multiply (which has no matching entry in the compact ALU encoding table), the
  full AMD64 backend and semantic corpus passed. Seven pinned 300 ms samples
  were effectively flat for scalar Blake and about 0.6% slower for SIMD Blake:
  the shorter sequence introduces a read-modify-write dependency through the
  frame slot. The encoder and compiler prototype was removed. Raw capture is
  `/private/tmp/wago-v5-amd-frame-self-update-panel.txt`.
- **AMD64 owned-source RORX reuse (rejected before timing).** Reusing an
  allocator-owned rotate source as the RORX destination appeared to remove a
  needless register allocation, but `TestCorpusSemanticExec` immediately
  produced incorrect output for all three reference BLAKE3 modes. Ownership at
  this lowering point does not prove the source has no other live tree alias.
  The prototype was removed without collecting performance numbers.
- **AMD64 direct wrapped frame reads (rejected).** A frame-only transform read
  the low 32 bits for `i32.wrap` and the high 32 bits for
  `i32.wrap(i64.shr_* x, 32)` directly from compiler-owned frame/spill storage.
  Linear-memory loads were excluded so eight-byte bounds and trap behavior
  remained unchanged, and the semantic corpus passed. Seven pinned 300 ms
  samples nevertheless regressed scalar Blake about 1.0%, with SIMD Blake and
  SHA-256 flat; shortening the large unrolled body changed its fetch phase
  adversely. The prototype was removed. Raw capture is
  `/private/tmp/wago-v5-amd-direct-wrap-read-panel.txt`.
- **AMD64 equal-hotness definition swaps (rejected).** Letting an incoming local
  definition replace an equal-hotness regional lease trades the evicted dirty
  writeback for the incoming frame store and can retain later reads. The AMD64
  backend and semantic corpus passed, but seven pinned 300 ms samples regressed
  scalar Blake about 0.8% and SHA-256 about 0.7%, with SIMD Blake flat. The
  coarse whole-function hotness score does not predict the next profitable
  version, so the policy and knob were removed. Raw capture is
  `/private/tmp/wago-v5-amd-interval-define-swap-panel.txt`.
- **AMD64 regional R15/memory-size trade (rejected before timing).** A bounded
  prototype reloaded the authoritative memory byte length at explicit checks so
  a large call-free straight-line kernel could use R15 for one more regional
  local. The reference BLAKE3 semantic cases trapped out of bounds at input
  length 65, proving R15 participates in a wider explicit-bounds convention
  than the local cache field captures. The optimization and knob were removed;
  R15 remains reserved throughout explicit-bounds code.
- **AMD64 widened call-free leaf fence elision (rejected).** The conservative
  static frame bound was raised from 4 KiB to 64 KiB, still four times below the
  runtime's 256 KiB checked headroom, only for functions with no calls. Backend
  and semantic tests passed, but seven pinned 300 ms samples were effectively
  flat: scalar Blake improved about 0.1%, raytrace about 0.2%, and other focused
  rows stayed within noise. A security-boundary policy expansion was not worth
  that return, so the original 4 KiB rule was restored. Raw capture is
  `/private/tmp/wago-v5-amd-wide-leaf-fence-panel.txt`.
- **AMD64 XMM integer-local shadow cache (rejected before timing).** A bounded
  prototype assigned one to four locals just below the GP-residency cut to idle
  XMM registers in call-free, control-free, non-SIMD integer functions. It
  initialized the cache on every entry and kept canonical frame stores coherent,
  but XMM-backed reads still corrupted all three reference BLAKE3 modes. Frame-
  only reads restored the exact oracle, isolating an implicit XMM-use/clobber
  invariant absent from the current hint model. The cache, state, and public
  knob were removed; reuse of the FP register file now requires a complete
  per-function XMM-usage proof before another prototype.
- **AMD64 adjacent frame store/load forwarding (rejected).** Native byte
  adjacency was not a sufficient predecessor proof across generated control
  flow, and a stricter consecutive-Wasm form produced no useful scalar Blake
  hits. The experiment also exposed that the proposed tenth signals-mode
  regional lease (RSI) was not safe: the guard-page differential oracle for
  `blake-as` changed from 2973751372 to 877833372. Restoring the existing
  nine-register signals-mode limit restored the oracle; the lease is removed.
- **AMD64 destructive owned shifts (rejected).** Reusing an allocator-owned
  temporary as the destructive destination for constant shifts passed the full
  semantic gates, but seven pinned 300 ms samples left scalar Blake and SHA-256
  flat while regressing `xjb-mulhi.runN` about 3.7%. The two-register form is
  restored. Raw capture is `/tmp/wago-v5-amd-owned-shift-panel.txt` on the Ryzen
  host.
- **AMD64 direct-memory interrupt polls (rejected).** Replacing the interrupt
  status load plus register test with a memory compare preserved every safety
  and semantic gate. Without padding it moved downstream code and regressed
  SIMD Blake about 6%; with exact one-byte layout preservation, repeated
  internal-only and boundary panels were flat. The original load/test sequence
  is restored. Raw capture is `/tmp/wago-v5-amd-interrupt-memcmp-panel.txt` on
  the Ryzen host.
- **AMD64 proof-gated RDX regional lease (retained, uncommitted).** A one-bit
  scan hint excludes every integer divide/remainder, and the existing regional
  eligibility excludes calls, control flow, bulk memory, and functions with
  multiple results. SIMD operations may coexist: their temporary GPRs use the
  ordinary allocator and honor the pinned-local mask. Only then may the bounded
  local cache borrow RDX; all ordinary pressure and eviction rules remain
  active. RAX alone and multi-
  scratch variants failed semantic corpus checks, while RCX was weaker; only
  the RDX-only form passed the full AMD64 backend, explicit/guard differential,
  and semantic execution suites. Seven pinned 300 ms samples improved scalar
  Blake-AS about 0.9--1.2% and SIMD Blake-AS about 2.6--3.1%, with SHA-256 and
  matmul flat to about 0.8%. The full three-sample option sweep is retained at
  `/tmp/wago-v5-amd-rdx-lease-full-3x.txt` on the Ryzen host; the exact current
  paired execution gate remains pending.
- **ARM64 compact large-loop layout (rejected).** Removing optional 16-byte
  loop alignment and historical poll-phase padding after 1 KiB of native code
  passed the backend and explicit/guard semantic suites, but the unrestricted
  form regressed fannkuch about 4.8% and sieve about 3.8%. Restricting the rule
  to functions with more than 16 locals still regressed fannkuch about 7.1%
  and produced a 0.9964x geometric mean across the 36 execution rows in the
  option harness. SHA-256 improved about 5.1%, but that isolated gain does not
  justify a generally harmful layout policy. The knob and implementation are
  removed. Raw captures are
  `/private/tmp/wago-v5-arm-compact-large-loops-{full,pressure}-5x.txt`.
- **ARM64 pressure-forced cancellation-cell pin (rejected).** Reserving the
  stable trap-cell pointer before whole-function local assignment preserved all
  cooperative cancellation polls and passed backend plus semantic gates, but
  trading away the coldest local pin was worse than retaining the basedata
  indirection. Seven 250 ms focused samples regressed fannkuch about 3.5% and
  produced a 0.9986x geomean across eight loop-heavy rows. The reservation and
  knob are removed. Raw capture is
  `/private/tmp/wago-v5-arm-pressure-trap-focus-7x.txt`.
- **ARM64 vectorized small dynamic memory.copy (rejected).** A semantics-gated
  prototype copied the 16-byte prefix of sub-64-byte dynamic moves with NEON,
  preserving overlap direction and retaining the exact 8-byte/byte tail. Seven
  250 ms samples improved json-as deserialization about 4.8%, but regressed
  fannkuch about 3.1% and scalar Blake-AS about 0.9%; the extra inline dispatch
  also moved downstream code. The mixed 1.0016x focused geomean is insufficient
  for the code-size/layout cost, so the option and lowering are removed. Raw
  capture is `/private/tmp/wago-v5-arm-small-copy-v128-focus-7x.txt`.
- **ARM64 vector-register cancellation-cell cache (rejected).** A prototype
  reserved V31 for the stable trap-cell pointer only when the existing GP cache
  could not acquire a register. All ordinary float/SIMD allocation honored the
  reservation and every poll remained intact. Runtime-config telemetry showed
  the existing GP cache already fires for the representative loop corpus,
  including fannkuch, arith, float, globals, and SHA-256, so the fallback had no
  useful coverage. The apparent 0.9981x eight-row A/B result was noise from
  identical code; the fallback and option are removed. Raw capture is
  `/private/tmp/wago-v5-arm-vector-trap-focus-7x.txt`.
- **ARM64 default-option ablation screen (diagnostic only).** A three-sample,
  60 ms screen across 14 representative rows found no latent broad rollback;
  the short windows were visibly layout/frequency noisy. The most plausible
  candidate, `load-pair`, was promoted to a complete 36-row five-sample 150 ms
  A/B. Keeping it measured 1.0023x versus disabling it (the reported off/on
  geomean was 0.997703x), with 18 wins and 18 losses. It remains enabled. Raw
  captures are `/private/tmp/wago-v5-arm-ablate-*.txt` and
  `/private/tmp/wago-v5-arm-load-pair-full-5x.txt`.
- **ARM64 sampled countdown-loop cancellation polls (rejected).** A structural
  proof recognized exact single-writer i32 decrement loops and retained the
  entry and return checks while polling the loop header every eight iterations.
  Backend, semantic, guard-page, cancellation, interrupt, and deadline tests
  passed, but seven pinned 250 ms samples regressed fannkuch about 7.7% while
  the other seven representative rows were effectively flat. It also weakened
  worst-case cancellation responsiveness from one potentially expensive loop
  iteration to eight. The proof, lowering, option, and catalog entry are fully
  removed. Raw capture is
  `/private/tmp/wago-v5-arm-sampled-poll-focus-7x.txt`.
- **ARM64 preserved bulk-helper scratch pins (rejected).** A table-free,
  call-free bulk-memory function could lend X12/X13 to two additional hot locals
  while the fixed inline helper stored and restored those locals around its
  clobber window. Full semantic and guard-page corpus tests passed, but seven
  pinned 300 ms samples regressed fannkuch about 6.3%; json-as and both Blake-AS
  rows remained effectively flat. The option, pin-pool expansion, and helper
  preservation are removed. Raw capture is
  `/private/tmp/wago-v5-arm-bulk-scratch-pins-focus-7x.txt`.
- **ARM64 compact cached loop polls (rejected).** Removing the one-word
  layout-preservation NOP kept every cancellation check intact and reduced each
  cached loop header by four bytes. Seven pinned 300 ms samples were neutral
  overall, however: fannkuch regressed about 1.6% and memory.sum about 1.7%,
  outweighing roughly 0.5--1.5% wins in xjb, float, and globals. The compact-poll
  option is removed and the established fetch phase is restored. Raw capture is
  `/private/tmp/wago-v5-arm-compact-loop-poll-focus-7x.txt`.
- **AMD64 early RDX regional lease (rejected).** The existing div/rem-free
  scratch proof was unchanged, but the regional cache tried RDX before ordinary
  registers so expression temporaries could not consume the potential tenth
  lease first. Backend and semantic/guard corpus gates passed. Seven pinned
  300 ms samples improved SIMD Blake-AS about 1.3% but regressed scalar
  Blake-AS about 1.5%; the remaining rows were flat. The early preference and
  temporary option are removed. Raw capture is
  `/tmp/wago-v5-amd-rdx-first-panel-7x.txt` on the Ryzen host.
- **AMD64 i64 associative-tree gate (rejected).** Keeping associative covering
  for i32 while routing every i64 tree through ordinary ordered lowering passed
  backend and semantic tests. A complete 36-row five-sample 150 ms same-binary
  sweep improved the geometric mean by only 0.056% and regressed
  `swar-pack-parse.runN` by 6.0%. The type boundary and option are removed; the
  existing all-type associative cover remains enabled because its independent
  full ablation was strongly positive. Raw capture is
  `/tmp/wago-v5-amd-assoc-i64-full-5x.txt` on the Ryzen host.
- **AMD64 direct register-ABI argument lowering (rejected).** A bounded physical
  register proof lowered deferred integer arguments directly into an otherwise
  free ABI target, eliminating the subsequent parallel-copy move. Backend,
  explicit-bounds, guard-page, and semantic execution tests passed, but a
  nine-workload seven-sample 300 ms sweep regressed 0.21% geometrically. The
  largest loss was json-as SIMD serialization at 2.44%, while `many_funcs` was
  unchanged. The path, option, and tests are removed. Raw capture is
  `/tmp/wago-v5-amd-call-arg-focus-7x.txt` on the Ryzen host.
- **AMD64 RCX regional lease (rejected for correctness).** A provisional second
  scratch lease admitted RCX only in straight-line, call-free, table-free,
  GC-free, custom-free regions and explicitly demoted its owner before variable
  shifts. Backend tests passed, but the exact BLAKE3 hash, keyed-hash, and
  derive-key semantic vectors all misexecuted, including after moving the
  variable-shift reclamation before operand evaluation. This proves another
  fixed RCX consumer exists inside the admitted surface. The lease and all
  supporting code are removed without performance measurement.
- **AMD64 regional i64 residency weighting (retained, uncommitted).** Regional
  eviction now prices a packed full-width local reload 1.5x above an otherwise
  equally hot i32 reload. This changes only bounded cache victim selection; the
  canonical frame remains authoritative, and the optimization has an explicit
  rollback option. Backend and semantic/guard gates pass. A complete 36-row
  five-sample 150 ms same-binary sweep improved the geometric mean by 0.167%,
  led by quicksort at 4.2% and branches at 2.1%; scalar Blake-AS improved about
  0.3%, and observed losses remained below roughly 1.15%. Raw capture is
  `/tmp/wago-v5-amd-i64-weight-full-5x.txt` on the Ryzen host.
- **ARM64 large-function branch-fold gate (rejected after stronger rescreen).**
  An early five-sample screen appeared to favor retaining guarded two-branch
  edges in bodies larger than 1 KiB. Native-PC profiling later showed those
  extra branches dominating Fannkuch's hot loop. A complete 46-row three-sample
  rescreen with the same current binary showed direct folding ahead by 1.0255x
  geometrically, including Fannkuch 1.150x, CRC32 1.148x, SHA-256 1.109x, and
  raytrace 1.038x. A subsequent ABBA comparison of separately compiled binaries
  confirmed a 1.0144x gain across a 23-row panel, led by Fannkuch at 1.170x,
  spectral norm at 1.050x, CRC32 at 1.027x, and raytrace at 1.018x. The body-size
  gate, option, schema entry, and test are removed; the established `branch-fold`
  option again controls the optimization uniformly. Raw captures are
  `/private/tmp/wago-v5-arm-largefold-full-{off,on}-3x.txt` and
  `/private/tmp/wago-v5-arm-gate-abba-{a1,a2,b1,b2}.txt`.
- **AMD64 widened carry arithmetic (rejected).** The historical exact
  `i64.extend_i32_u(iN.lt_u/gt_u)` into `ADC`/`SBB` transform was ported behind
  an opt-in immutable selection and passed focused full-width/near-miss tests
  plus the executable semantic corpus. Compiler statistics found matches only
  in Lua, SQLite, and Ruby; scalar/SIMD Blake, BLAKE3, xjb, SHA-256, raytrace,
  and the call microbenchmarks had none. A 36-row five-sample 150 ms sweep
  appeared to improve 0.44% geometrically, but its largest apparent wins were
  SWAR micro rows with zero matches. Dumping the enabled and disabled SWAR
  products produced byte-identical native images (SHA-256
  `9dfcfbc1a65e6ef2b1b8b11356ae9fa29ee9b6d3ccacc326f5cba5271652dfbb`),
  proving the movement was temporal benchmark bias rather than generated-code
  improvement. The transform, option, and tests are fully removed. Raw capture
  is `/tmp/wago-v5-amd-carry-full-5x.txt`.
- **ARM64 speculative post-call local load pairs (rejected).** A prototype used
  one integer or FP `LDP` when the demanded pinned local and an adjacent pinned
  local were both memory-resident after a call. Raytrace exposed seven pair
  sites, but eagerly recovering the companion also defeated 15 existing
  dead-reload eliminations. Seven 300 ms samples moved raytrace only about 0.2%
  at the median, far below the extra state interaction and instruction-encoding
  surface. The option, lowering, and encoder addition are fully removed. Raw
  capture is the terminal run from this worktree; the matching explain report is
  `/private/tmp/wago-v5-arm-explain-raytrace-pair.txt`.
- **PR #567 AMD64 dispatch refinements (rejected).** Porting the proposed static
  immutable-table length immediately broke managed-table dispatch: a managed
  table may be populated beyond the module's declared minimum even though Wasm
  code cannot mutate it, so the minimum is not an exact runtime length. Keeping
  the runtime length check restored correctness. The independently safe pieces
  were too narrow: removing redundant private-table descriptor ownership checks
  improved `dispatch` about 0.7%, zlib about 0.2%, and Zstandard about 0.0%;
  retaining an immediately returned call result in RAX improved `memory_tree`
  about 1.0% while leaving `dispatch` flat. Neither can materially advance a
  46-row geometric mean, so the complete port and its temporary toggles/tests
  are removed. Raw captures are `/tmp/wago-v5-amd-pr567-{trim,result}-panel.txt`
  and `/tmp/wago-v5-amd-pr567-semantic-{off,on}.txt`.
- **AMD64 local-version final-read transfer (rejected for correctness).** A
  bounded one-pass sidecar identified the last textual `local.get` before the
  next definition and transferred the regional register to the operand tree.
  The reference BLAKE3 hash, keyed-hash, and derive-key vectors all produced
  incorrect digests. Restricting candidates to one straight-line control region
  did not restore correctness, proving that the ownership transition itself
  violates a deeper allocator/local-state invariant. Disabling only this option
  restored the complete semantic and differential corpus. The transfer,
  sidecar, option, and tests are fully removed without performance measurement;
  the failure explain capture is
  `/tmp/wago-v5-amd-version-final-fail-explain.txt`.
- **ARM64 descending local load pairs (rejected).** A bounded lookahead paired
  adjacent descending `i32.load; local.set` sequences only when both addresses
  used the same unchanged local, the operand stack contained no older deferred
  work, the high lane trapped first in the original program, and the existing
  linear-memory indexed-pair primitive was encodable. Focused execution,
  rollback, near-miss, and semantic corpus gates passed, but the pattern fired
  only once in Fannkuch. Combined AB/BA seven-sample panels measured a 0.9986x
  candidate/base geomean, with Fannkuch effectively flat. The lowering, option,
  tests, and temporary benchmark are fully removed. Raw captures are
  `/private/tmp/wago-v5-arm-local-load-pair-panel{,-reverse}.txt`.
- **ARM64 memory-copy-only X14 local pin (rejected).** A diagnostic relaxed the
  coarse bulk-memory exclusion for X14 in the table-free Fannkuch kernel. The
  generated function used one additional hot-local pin (16 instead of 15), but
  seven alternating 300 ms samples measured 0.998986x candidate/base and native
  size remained exactly 4216 bytes. Because the extra pin was neutral before
  paying for an exact copy-versus-fill/init first-pass clobber fact, the
  relaxation was removed. Raw capture is
  `/private/tmp/wago-v5-arm-bulk-x14-fann-panel.txt`.
- **AMD64 XMM cache non-entry-initialized retry (rejected).** The earlier
  corruption was narrowed to locals whose first textual access is a definition:
  excluding that entry-initialization-elision class restored every BLAKE3 vector
  and the complete semantic corpus under AMD64 Rosetta. The remaining proof-safe
  subset had no coverage in the ordinary corpus before reference BLAKE3, and a
  seven-sample 200 ms Rosetta direction screen made the three BLAKE3 modes about
  2--4% slower. The extra MOVD transfer chain is not a useful cache on this
  surface, so the complete retry and temporary benchmark were removed. Raw
  captures are `/private/tmp/xmm-safe-rosetta-{off,on}.txt`.
- **Prepared call-block live-argument ABI (rejected).** A correct alternative
  kept only immutable entry state in the call block and passed the four live
  arguments through the Go ABI0 frame, removing four Go stores. Ten 500 ms
  samples on the M4 improved the enabled path only 1.0248x (5.5815 ns versus
  5.7200 ns), weaker than the retained struct-backed call block's prior 1.0340x
  result. The smaller ABI0 frame did not repay its additional marshaling, so the
  faster measured struct layout was restored. Raw capture is
  `/private/tmp/wago-v5-arm-callblock-mixedargs.txt`.
- **ARM64 guard-mode X27 local pin (rejected).** A call-free function with no
  explicit memory-size cache could safely add otherwise-idle X27 to its local
  pool. Backend and semantic corpus tests passed, but seven 300 ms Fannkuch
  samples were exactly neutral (1,240,730 ns enabled versus 1,241,077 ns
  disabled). This independently confirms that another whole-function pin does
  not address Fannkuch's bottleneck, so the experimental admission was removed.
  Raw captures are `/private/tmp/wago-v5-arm-x27pin-{off,on}.txt`.
- **ARM64 stable-window indexed-base elision (retained, uncommitted).** The
  existing proof already recognizes an unchanged `linearBase + index` across a
  bounded four-instruction straight-line window, but emitted a NOP in place of
  the redundant ADD. Since branch relocations are finalized after emission, the
  NOP is not required for correctness. Omitting it removes executed instructions
  without extending the proof window or retaining new state. Seven 300 ms
  samples improved Fannkuch by 2.04% and raytrace by 0.66%; focused encoder,
  backend, memory, and execution tests pass. Raw captures are
  `/private/tmp/wago-v5-arm-indexreuse-compact-{off,on}.txt`.
- **ARM64 loop-entry phase-debt deferral (rejected).** Native-PC sampling showed
  a large block of replacement NOPs executing on Fannkuch's loop-entry path. A
  first prototype moved all whole 16-byte alignment blocks to the cold tail, but
  an ABBA 23-row screen regressed 0.37% geometrically and Fannkuch 4.6%, proving
  the larger instruction phase matters. A second version preserved exact phase
  within 64-byte fetch windows and moved only complete cache lines; ten 300 ms
  samples were still slightly negative (Fannkuch -0.38%, raytrace -0.24%). Both
  versions and their tests are removed. Raw captures are
  `/private/tmp/wago-v5-arm-phase-abba-{a1,a2,b1,b2}.txt` and
  `/private/tmp/wago-v5-arm-phase64-{a1,a2,b1,b2}.txt`.
- **ARM64 eight-instruction indexed-base window (rejected).** The existing
  straight-line address proof was extended from four to eight recognized
  instructions to reach two additional sampled redundant ADDs in Fannkuch.
  Focused and semantic execution gates passed, but a ten-sample 300 ms ABBA
  comparison made Fannkuch 0.46% slower and left raytrace flat. The wider
  window and its test are removed; the retained four-instruction elision keeps
  the better code layout. Raw captures are
  `/private/tmp/wago-v5-arm-index8-{a1,a2,b1,b2}.txt`.
- **ARM64 immutable-local derived memory base (rejected).** A bounded first-pass
  proof selected a call-free loop's entry-initialized, never-redefined i32 local
  and cached `linearMemory + local` in otherwise-idle X27. Backend and semantic
  corpus gates passed. Removing the repeated address ADDs changed instruction
  layout enough to regress a focused screen, while preserving layout with NOPs
  moved the complete 46-row ABBA geometric mean only 1.0008x and produced
  inconsistent Fannkuch results across repetitions. Meaningful execution-corpus
  coverage was limited to Fannkuch and nbody, with nbody neutral. That signal
  does not justify the additional provenance, register-reservation, and memory
  lowering state, so the feature, toggle, and catalog entry are fully removed.
  Raw captures are `/private/tmp/wago-v5-arm-derived-{a1,a2,b1,b2}.txt`,
  `/private/tmp/wago-v5-arm-derived-stable-{a1,a2,b1,b2}.txt`, and
  `/private/tmp/wago-v5-arm-derived-full-{a1,a2,b1,b2}.txt`.
- **AMD64 wrapped high-half BMI2 extraction (rejected).** A semantic machine
  cover replaced `i32.wrap(i64.shr_u(x, 32))`'s copy, destructive SHR, and
  canonicalization with 64-bit RORX plus canonicalization. It passed AMD64
  backend and complete semantic-corpus tests and removed 80 native bytes from
  scalar Blake. A ten-sample 300 ms ABBA panel nevertheless measured 0.9992x
  overall: scalar Blake regressed 0.6%, all three reference BLAKE3 modes
  regressed 0.3--0.4%, and only SIMD Blake improved 1.3%. The cover, option,
  catalog entry, and test are fully removed. Raw capture is
  `/private/tmp/wago-v5-amd-wrapshr-abba.txt`.
- **AMD64 fixed-register commutative self-updates (retained, uncommitted).**
  The existing in-place `x = f(y) op x` lowering formerly rejected RAX, RCX,
  and RDX unconditionally. It now admits a fixed-register accumulator only when
  the bounded Valent tree proves that every operation honors the pinned set;
  division, remainder, variable shifts, calls, and unsupported leaves fail
  closed. This removes spill/reload pairs when the proof-safe RDX regional
  scratch lease holds the updated local. AMD64 backend and uncached enabled and
  disabled semantic corpus runs pass under explicit and guard-page bounds.
  Coverage is limited to scalar Blake, SIMD Blake, two reference-BLAKE3
  functions, and compile-only Ruby. A focused ten-sample ABBA panel improved
  1.0038x; the five affected execution rows in the complete ABBA sweep improved
  1.0064x with every row positive, equivalent to roughly 1.0007x across all 46
  rows. Unaffected QOI outliers in one full block were confirmed as benchmark
  noise rather than generated-code changes. Raw captures are
  `/private/tmp/wago-v5-amd-fixed-selfupdate-{abba,full-abba}.txt`.
- **AMD64 loop trap-cell cache (rejected for target coverage).** A layout-stable
  port of ARM64's call-free loop trap-cell cache was prototyped with an idle RSI
  reservation and a direct memory comparison. Linux/AMD64 uses the host
  asynchronous-interruption mechanism, so normal Ryzen compilation emits no
  cooperative entry or loop polls and the feature had zero target coverage.
  The prototype, encoder form, option, and tests are fully removed.
- **AMD64 regional memory-size lease (retained, uncommitted).** In explicit
  bounds mode, a large call-free, control-free, bulk-free register-ABI function
  may lend R15 to the existing bounded local-residency cache. Bounds checks read
  the stable current byte size directly from basedata and the internal return
  reloads R15, preserving the module register ABI. Backend plus explicit and
  signal-backed semantic corpora pass. The scalar Blake hot function gained a
  tenth active lease, removed 18 activation loads and 7 writebacks, and shrank
  107 bytes. Seven alternating 300 ms Ryzen samples improved scalar Blake-AS
  2.83%, SIMD Blake-AS 0.87%, and BLAKE3 4.97--5.68% (five-row geomean
  1.038868x). A complete 46-row screen was 1.000757x naively; explain proved the large
  apparent json-as SIMD loss was noise from identical code because the option
  fires only in Blake-AS, BLAKE3, and compile-only Ruby. Raw captures are
  `/private/tmp/wago-v5-amd-memlease-{off,on}.txt` and
  `/private/tmp/wago-v5-amd-memlease-full-{off,on}.txt`.
- **AMD64 unused module-global regional lease (retained, uncommitted).** A
  qualifying memory-touching regional function may save one module-pinned
  global register that its sparse per-function hint proves it never references,
  borrow it for local residency, and restore it on normal return and before
  every terminal trap writeback. Spill slot zero is reserved, so operand spills
  cannot overwrite the saved incoming value. Backend plus explicit and
  signal-backed semantic corpora pass. Scalar Blake gained an eleventh active
  lease, removed another 11 evictions and 11 writebacks, and shrank from 6143
  to 5850 bytes. Seven alternating 300 ms Ryzen samples improved scalar
  Blake-AS 6.43%, SIMD Blake-AS 2.01%, and BLAKE3 0.73--0.97% (five-row
  geomean 1.021775x). The same shape-only coverage is Blake-AS, BLAKE3, and
  compile-only Ruby. Raw captures are
  `/private/tmp/wago-v5-amd-modlease-{off,on}.txt`.
- **AMD64 narrowly proof-gated RAX regional lease (rejected for correctness).**
  A retry restricted RAX to void, memory32, call-free, control-free, table-free,
  GC-free, custom-free, non-SIMD functions with no scanned fixed-scratch use.
  The backend suite passed, but all three exact BLAKE3 semantic vectors
  corrupted immediately. This proves an implicit RAX consumer remains outside
  those coarse facts. The lease is fully removed; RAX stays unavailable to
  regional locals until every clobber is represented explicitly.
- **Current paired execution refresh after regional leases.** Under the full
  signal-backed, `GOMAXPROCS=1`, five-sample 300 ms contract, current AMD64 on
  Ryzen CPU 7 measures 1.539529x wazero/Railshot with 36/46 wins. ARM64 on the
  M4 Max measures 1.447411x with 41/46 wins. AMD64 remains 3.93% short and
  ARM64 10.54% short of the 1.60x gate. Raw captures are
  `/private/tmp/wago-v5-amd-current-leases-paired-5x.txt` and
  `/private/tmp/wago-v5-arm-current-paired-5x.txt`.
- **ARM64 prepared call-block hot-path split (retained candidate, uncommitted).**
  The bounded prepared-call success path now lives in a small dedicated Go
  helper instead of sharing the direct-call function's 176-byte frame with its
  lock, scheduler, and error variants. Ten alternating focused samples moved
  the prepared add-one call from roughly 5.35 ns to 5.24 ns, about 2% faster.
  This remains pending full-corpus and lifecycle gates. Raw capture is
  `/private/tmp/wago-v5-arm-callblock-split.txt`.
- **ARM64 native-PC Fannkuch diagnosis.** A 60-second native profile and exact
  generated-code dump found the hottest loop at guard-code offsets `0xa30` and
  `0xb3c`--`0xb50`. The latter was a scalar `if (result i32)` join that stored
  either arm to a frame slot and immediately reloaded it for the next compare.
  The compiler had spent canonical X15 on a function-wide local, globally
  disabling its existing register-merge path. Captures are
  `/private/tmp/wago-fann.sample.txt`, `/private/tmp/wago-fann-guard.bin`, and
  `/private/tmp/wago-fann-guard.asm`.
- **ARM64 weighted scalar-merge reservation (retained in isolated form).**
  The bounded forward pre-scan now records one packed header bit when scalar
  block/if joins accumulate at least 100 units of loop and branch-path weight.
  A call-free qualifying function reserves X15 for the existing merge lowering
  instead of assigning it to a final whole-function local. The retained header
  remains 32 bytes and the control-depth fallback remains conservative. Exact
  executable coverage is Fannkuch, LZ4 compression, zlib inflate, one utf-as
  helper, and two utf-as-simd helpers; compile-only Ruby also qualifies. Seven
  alternating 300 ms affected-row samples improved Fannkuch 8.05%, LZ4
  compression 2.19%, zlib 1.53%, and the seven-row geomean 1.738%, with the
  remaining rows effectively neutral. Explicit and guard-page semantic corpora
  pass. Raw captures are
  `/private/tmp/wago-v5-arm-merge-weighted-leaf-{base,cand}.txt`.
  After rebasing onto PR #564's compiler-state rewrite, the original version's
  separate local-state convergence changes corrupted JSON initialization and
  SQLite and were removed. Reintroducing only the packed hotness hint and X15
  reservation leaves structured-control convergence untouched. The current
  isolated form passes the ARM64 backend, JSON/SQLite/FASTA/IFS execution
  oracles, and the complete signal-backed differential and semantic corpus.
  Five alternating two-second samples improve Fannkuch 22.71% and zlib 7.66%.
  Raw captures are `/private/tmp/wago-v5-arm-weighted-long-{off,on}.txt`.
- **ARM64 counted-loop latch retry (rejected).** Reintroducing the exact latch
  without the unsafe convergence rewrite passed focused execution,
  asynchronous interruption, backend, and signal-backed semantic tests. Seven
  alternating 500 ms samples improved the five affected rows only 1.15%
  geometrically while regressing arithmetic 2.33% and `memory.sum` 3.13%.
  The mixed default trade was removed. Raw captures are
  `/private/tmp/wago-v5-arm-latch-current-{off,on}.txt`.
- **ARM64 pinned-local result merge (rejected for correctness).** A prototype
  tried to carry a scalar block/if result directly in the pinned register of an
  immediately following `local.set` or `local.tee`. Backend tests passed, but
  the complete semantic gate found corrupted BLAKE3 vectors and QOI output plus
  a zstd out-of-bounds trap. Reusing the destination register changed live
  local state before the Wasm assignment and violated control-edge convergence.
  The prototype is fully removed; no such register aliasing remains.
- **ARM64 local.tee zero-branch flag copy (rejected).** A type-proven,
  branch-target-safe post-assembly fold replaced `MOV Xd,Xs; CMP Wd,#0` before
  an EQ/NE branch with `ANDS Wd,Ws,Ws` plus either a compacted or preserved NOP.
  Explicit and guard semantic corpora passed, but eleven alternating 300 ms
  Fannkuch samples regressed roughly 0.65% with compaction and 2.6% with stable
  layout. Apple MOV elimination and CMP+branch behavior are better than the
  apparently shorter dependency, so the fold and all test/state additions are
  fully removed. Raw captures are
  `/private/tmp/wago-v5-arm-tee-fann-{base,cand}.txt` and
  `/private/tmp/wago-v5-arm-tee-fann-stable-{base,cand}.txt`.
- **Current ARM64 paired refresh after weighted merges.** A fresh complete
  signal-backed, five-sample 300 ms comparison measures 1.450886x
  wazero/Railshot with 40/46 wins. This is 0.24% above the prior current ARM64
  run; Fannkuch is now 0.8408x and raytrace 0.8957x. ARM64 remains 9.32% short
  of the 1.60x gate. Raw capture is
  `/private/tmp/wago-v5-arm-current-weighted-paired-5x.txt`.
- **Architecture-specific prepared call-block default (retained candidate,
  uncommitted).** Native measurement found opposite optimal thunks: the retained
  struct-backed call block helps ARM64 by about 2%, while Ryzen's existing
  value-argument ABI0 thunk measures about 7.47 ns versus 8.17 ns for the call
  block. `WAGO_PREPARED_INT_CALL_BLOCK=0/1` still overrides either architecture;
  the default is now on for ARM64 and off for AMD64. Nine alternating 300 ms
  Ryzen samples improved the seven affected execution rows 1.07675x
  geometrically: tiny 13.44%, dispatch 7.78%, branches 6.07%, xjb-mulhi 7.03%,
  SWAR pack 10.01%, SWAR parse 9.51%, and many_funcs 0.34%. Explicit and
  signal-backed AMD64 semantic corpora pass. Raw captures are
  `/tmp/wago-v5-amd-callblock-{off,on}-panel.txt`; the complete paired refresh
  is pending.
- **AMD64 prepared helper split (rejected).** Moving the call-block case into a
  dedicated helper helps ARM64's large direct-call frame, but does the opposite
  on AMD64. Nine alternating 300 ms samples with the faster value-argument thunk
  selected measured the split at 0.98443x geometrically over the seven affected
  rows, including 4--5% SWAR regressions. AMD64 is restored to the unified
  helper; only ARM64 retains the architecture-specific split.
- **Conservative AMD64 paired snapshot after call-block selection.** A fresh
  complete five-sample 300 ms run measured 1.530217x wazero/Railshot with 38/46
  wins. Both engines moved slightly between full runs and the provisional AMD64
  helper split was still present in this capture, so this is a conservative
  progress snapshot rather than the final no-split gate. Raw capture is
  `/private/tmp/wago-v5-amd-callblock-off-paired-5x.txt`.
- **AMD64 logical-shift wrap elimination (rejected for correctness).**
  Bounded value facts now prove that `i64.shr_u` by an effective constant count
  of 32--63 has a zero upper half; a following in-place `i32.wrap_i64` therefore
  omits only its redundant 32-bit self move. Masked counts, signed shifts, and
  non-in-place conversions retain the old lowering. The independent
  `shift-wrap-elim` option and environment rollback switch keep the change
  directly A/B-able. It removes eight moves from scalar Blake, eight from the
  shared SIMD Blake scalar helper, four from each of two SIMD kernels, and one
  from each of two BLAKE3 functions. Nine alternating 300 ms Ryzen samples show
  scalar Blake neutral (+0.03%), SIMD Blake +1.68%, and the six-row crypto panel
  +0.45% geometrically; SHA-256 is unaffected and serves as a noise control.
  However, the complete process-fresh corpus oracle immediately corrupted the
  SIMD Blake result (`0x04c923ef0` instead of `0x01945001`). The provenance is
  therefore insufficient to justify eliminating a Wasm width-conversion node,
  regardless of the locally plausible machine invariant. The option, facts,
  consumer change, and tests are fully removed. Raw diagnostic captures are
  `/private/tmp/wago-v5-amd-shift-wrap-{off,on}.txt` and
  `/private/tmp/wago-v5-amd-shift-wrap-explain-pairs.txt`.
- **AMD64 SIMD regional-scratch exclusion (required safety repair).** The full
  process-fresh corpus gate exposed a pre-existing interaction once all three
  regional leases were enabled: SIMD Blake returned `0x4c923ef0` instead of its
  independently pinned `0x01945001` oracle. Disabling any one lease removed the
  pressure overlap; specifically disabling the RDX interval-scratch lease fixed
  it while leaving the other regional registers available. SIMD integer scratch
  uses are not all represented by the scalar fixed-scratch scan, so every
  function in a module carrying any exact `hintHasSIMD` scan fact now keeps RDX
  unleased. A two-function regression proves that this includes a scalar
  regional helper when a different function makes the containing module SIMD-
  capable. This is a feature-level safety gate, not a corpus or function-name
  exception. The full process-fresh explicit and signal-backed corpora pass
  after the repair.
- **Current correctness-gated AMD64 paired refresh.** With the rejected wrap
  experiment absent, the module-wide SIMD scratch exclusion active, and the
  faster architecture-specific prepared thunk selected, the pinned Ryzen CPU 7
  five-sample 300 ms contract measures 1.532107x wazero/Railshot with 36/46
  wins. This is 0.12% above the preceding 1.530217x snapshot despite restoring
  the missing safety boundary. AMD64 remains 4.23% short of the 1.60x gate.
  Scalar Blake is the principal deficit at 0.6909x; BLAKE3 is 0.8996--0.9257x,
  raytrace 0.9563x, and SIMD Blake 0.9660x. Raw capture is
  `/private/tmp/wago-v5-amd-safe-current-paired-5x.txt`.
- **ARM64 clean control-region read leases (rejected).** A bounded prototype
  selected only locals for which every non-empty straight-line version had at
  least two reads, kept the canonical frame value authoritative, and expired
  the clean cache at definitions, calls, invalidations, and structured-control
  boundaries. Both explicit and guard-page semantic corpora passed after exact
  ownership checks prevented regional expiry from touching whole-function
  pins. Seven 300 ms focused samples were effectively flat for raytrace, nbody,
  spectral norm, Mandelbrot, matmul, quicksort, and SHA-256, while Fannkuch
  trended slower. The metadata, option, cache, and temporary harness were fully
  removed. Raw capture is
  `/private/tmp/wago-v5-arm-control-read-pins-panel-v2.txt`.
- **ARM64 four-byte dynamic `memory.copy` tail (retained candidate).** Dynamic
  forward and backward copies now consume one four-byte chunk before entering
  the existing byte tail when at least four bytes remain. The option is limited
  to functions no larger than 4096 body bytes with at most 128 scanned memory
  operations, and `WAGO_ARM64_NO_MEMCOPY_TAIL4=1` restores the prior lowering.
  A ten-sample, 300 ms signal-mode interleaved panel improved the eleven
  affected rows 1.00927x geometrically, led by Fannkuch at +6.11%; scalar JSON
  deserialization improved 1.53%, SIMD JSON serialization 0.90%, and the only
  loss was Zstandard at a noise-level -0.13%.
  The full 4/2/1 prototype was rejected because its broader code expansion
  moved the complete 46-row mean only 1.0008x. Exact overlap/disjoint execution
  tests cover dynamic sizes 0 through 31 with the option both enabled and
  disabled, and both full semantic corpus modes pass. Focused raw capture is
  `/private/tmp/wago-v5-arm-tail4-guard-{off,on}.txt`; the earlier explicit-mode
  screen is `/private/tmp/wago-v5-arm-memcopy-tail4-abba.txt`.
- **ARM64 call-region floating constant cache (rejected).** Keeping floating
  constants live across call-making regions improved raytrace about 1.47% but
  regressed Mandelbrot about 1.89%; the ten-row geometric mean was 0.9990x.
  The option, metadata, compiler state, and harness were removed. Raw capture is
  `/private/tmp/wago-v5-arm-call-fconst-abba.txt`.
- **ARM64 duplicate floating-immediate reuse (rejected).** A size-preserving
  peephole replaced repeated immediate materialization with register reuse and
  proved 26 matches in raytrace, but a twelve-row interleaved panel was only
  1.0005x overall and raytrace regressed 0.13%. The peephole, option, and tests
  were removed. Raw capture is
  `/private/tmp/wago-v5-arm-fpimm-reuse-abba.txt`.
- **AMD64 immutable-state/live-argument prepared thunk (retained candidate).**
  Compiler-proven bounded prepared handles bind their immutable native entry,
  linear-memory base, foreign-stack top, and fixed trap re-entry address once;
  the per-call arguments remain live ABI values. This removes the repeated
  trap-stack store and shortens the Go-to-assembly argument frame without
  putting mutable arguments in a shared call block. A single
  `WAGO_PREPARED_INT_PREBOUND_CONTEXT=0` rollback restores the old bounded
  entry. The signal-mode candidate-versus-original ten-sample, 500 ms panel
  improved the six bounded-call rows 1.05034x geometrically: tiny +7.10%,
  parse4 +6.25%, mulhi +4.86%, pack +4.67%, branches +4.23%, and dispatch
  +3.14%. The amortized `runN` controls were within 0.05%; `many_funcs` was the
  noisy exception at -0.78%, leaving all nine rows at 1.03251x geometrically.
  Runtime, prepared-call, trap, close, GC-progress, and both full semantic
  corpus gates pass. Raw captures are
  `/private/tmp/wago-v5-amd-prebound-context-guard-{off,on}.txt`; the earlier
  explicit-mode screen is `/private/tmp/wago-v5-amd-hybrid-exact-{off,on}.txt`.
- **AMD64 prepared-thunk arity specialization (rejected).** Separate argument-
  count assembly entries increased symbol/code-layout cost and regressed the
  focused geometric mean 1.41%, so all arity variants were removed. Raw capture
  is `/private/tmp/wago-v5-amd-arity-panel.txt`.
- **Bounds-mode measurement audit (invalid runs).** An AMD64 sweep reported
  1.468341x and an ARM64 sweep reported 1.406955x, but both benchmark binaries
  were accidentally built without `-tags wago_guardpage` and therefore used
  explicit checks instead of the required signal-backed mode. `WAGO_EXPLAIN=1`
  confirmed the AMD64 binary emitted extra bounds branches and trap stubs; for
  example one JSON helper grew from 344 to 417 native bytes. These captures are
  retained only as invalid-run evidence and are not performance gates. Raw
  captures are `/private/tmp/wago-v5-amd-hybrid-paired-5x.txt` and
  `/private/tmp/wago-v5-arm-tail4-paired-5x.txt`.
- **Corrected current paired gates after call-context and tail4 changes.** Fresh
  `wago_guardpage`, `GOMAXPROCS=1`, five-sample 300 ms runs measure 1.532412x
  wazero/Railshot with 36/46 wins on Ryzen CPU 7 and 1.441071x with 40/46 wins
  on the M4 Max. AMD64 needs another 4.41% aggregate uplift to reach 1.60x;
  ARM64 needs 11.03%. The AMD64 snapshot is stable with the preceding 1.532107x
  gate. ARM64's Wago-only aggregate moved about 0.5% across the two full runs
  despite the independently positive tail4 A/B, so the lower absolute snapshot
  is retained rather than normalized away. Raw captures are
  `/private/tmp/wago-v5-amd-prebound-context-guard-paired-5x.txt` and
  `/private/tmp/wago-v5-arm-tail4-guard-paired-5x.txt`.
- **Compact immutable prepared-entry mode (retained candidate).** Replacing the
  existing AMD64 call-block boolean with a three-state byte records the legacy,
  call-block, or prebound-context entry choice once per prepared handle. This
  leaves `PreparedFunction` size and the embedded call descriptor offset
  unchanged while removing repeated option and bounds-selection work from the
  hot wrapper. Ten alternating 500 ms Ryzen samples improved the six affected
  rows 1.03080x geometrically and the complete nine-row latency panel 1.02491x;
  `tiny.add` moved from 8.247 ns to 7.829 ns. The same representation was
  neutral-positive on ARM64 at 1.00327x. Raw captures are
  `/private/tmp/wago-v5-{amd,arm}-mode-byte-{base,cand}.txt`.
- **Cached result metadata and uniform argument-shape shortcuts (rejected).** A
  cached result slice/width reduced ARM64 wrapper work but regressed AMD64 and
  grew every prepared handle by 24 bytes, so it was removed. A layout-neutral
  all-i32/all-i64 argument canonicalization shortcut regressed the Ryzen panel;
  narrowing it to all-i64 still measured only 0.99387x geometrically despite
  isolated gains for `xjb-mulhi.mulhi` (5.46%) and SWAR's long loop (2.46%).
  Tiny and dispatch regressed 5.23% and 4.54%, respectively, so all argument-
  shape state and branches were removed. Raw captures are
  `/private/tmp/wago-v5-{arm,amd}-cached-meta-{base,cand}.txt`,
  `/private/tmp/wago-v5-amd-uniform-args-{base,cand}.txt`, and
  `/private/tmp/wago-v5-amd-all-i64-{base,cand}.txt`.
- **ARM64 paired-Q large-copy lowering (retained candidate).** Functions no
  larger than 4096 body bytes and with no more than 128 scanned memory
  operations use `LDP Q`/`STP Q` in the existing 32- and 64-byte dynamic
  `memory.copy` loops; larger or uncertain functions keep the independent
  instruction sequence. The paired path retains complete-load-before-first-
  store ordering for overlapping memmove, and
  `WAGO_ARM64_NO_MEMCOPY_QPAIRS=1` restores the old lowering. Encoder goldens
  and dynamic overlap/disjoint execution tests through 160 bytes pass, as do
  the focused backend and semantic corpus gates. A four-sample-per-side,
  300 ms affected-row ABBA screen improved Fannkuch 3.42% and the 13-row
  geometric mean 0.21%; an independent eight-sample, 500 ms Fannkuch run
  measured +3.54%. Exact process-fresh code hashes confirm smaller native code:
  Fannkuch is 4276 -> 4180 bytes and scalar JSON is 67848 -> 67224 bytes.
  Raw timing captures are `/private/tmp/wago-v5-arm-qpairs-bounded-{off,on}.txt`
  and `/private/tmp/wago-v5-arm-qpairs-fann-{off,on}.txt`; code captures are
  `/private/tmp/wago-v5-arm-qpairs-bounded-code-{off,on}.txt`.
- **ARM64 paired-Q writeback addressing (rejected).** A follow-up used pre/post-
  indexed pair operations to absorb source and destination pointer updates.
  Although it removed more instructions, the eight-sample Fannkuch improvement
  fell from 3.54% to 1.31%, indicating that pair writeback costs more than the
  independent arithmetic on the measured M4 Max. Only the faster offset-pair
  form remains. Raw capture is
  `/private/tmp/wago-v5-arm-qpairs-postidx-fann-{off,on}.txt`.
- **ARM64 exact-chunk early exits (rejected).** Candidate-only `CBZ` exits after
  the 64- and 32-byte phases avoided smaller-tail tests for exact multiples,
  but an eight-sample Fannkuch run measured +3.41% versus +3.54% for paired
  loads/stores alone. The extra branches therefore had no demonstrated value
  and were removed. Raw capture is
  `/private/tmp/wago-v5-arm-qpairs-earlyexit-fann-{off,on}.txt`.
- **ARM64 paired-Q verification correction.** The first code-identity utility
  run produced empty files because it was built without the required
  `wago_guardpage` tag and silently skipped compile errors. The utility now
  reports failures, the corrected builds contain 42 successful modules and no
  errors, and only those nonempty captures are cited above. The original
  prototype
  replaced the independent 128-bit loads and stores in the existing 32- and
  64-byte dynamic `memory.copy` loops with `LDP Q`/`STP Q`, retaining the
  complete-load-before-first-store ordering required for overlapping memmove.
  Its initial eleven-row screen measured Fannkuch +5.74% and the panel +0.59%;
  these lower-repetition figures are retained as exploratory evidence only in
  `/private/tmp/wago-v5-arm-qpairs-{off,on}.txt`.
- **Current ARM64 paired gate after register-state and Q-pair changes.** A
  process-fresh signal-mode refresh was split into two deterministic 23-row
  halves to stay within the command-session limit; each engine/row has three
  200 ms samples under `GOMAXPROCS=1`. Across all 46 rows Railshot measures
  1.496412x wazero/Railshot with 43/46 wins, versus the preceding 1.441071x
  five-sample snapshot. The remaining losses are Fannkuch at 0.9206x,
  `xjb-mulhi.runN` at 0.9649x, and `float.run` at 0.9714x. This refresh leaves
  ARM64 6.92% short of the 1.60x aggregate gate. Raw halves are
  `/private/tmp/wago-v5-arm-qpairs-paired-half{1,2}-3x.txt`; parsed evidence is
  `/private/tmp/wago-v5-arm-qpairs-paired-summary-3x.txt`. The earlier
  monolithic capture `/private/tmp/wago-v5-arm-qpairs-current-paired-5x.txt`
  terminated mid-wazero stream and is explicitly invalid.
- **ARM64 compact cached loop polls (rejected).** Removing the hot-path NOP
  retained when a call-free loop caches its cancellation-cell pointer saved one
  instruction and four native bytes per eligible loop without changing poll
  frequency or semantics. However, the resulting fetch-phase changes regressed
  a 16-row loop-heavy panel 2.93% geometrically: linked-list lost 33.8%,
  Fannkuch 4.68%, and spectral norm 3.64%, outweighing float +0.99% and SHA-256
  +3.44%. The option, catalog entry, and test changes were removed; the existing
  phase-preserving NOP remains intentional. Raw captures are
  `/private/tmp/wago-v5-arm-compactpoll-{off,on}.txt`; exact code-identity
  captures are `/private/tmp/wago-v5-arm-compactpoll-code-{off,on}.txt`.
- **ARM64 one-shot 32-byte copy phase (rejected).** Proving that a dynamic
  `memory.copy` could execute its 32-byte phase at most once removed the loop
  backedge for that chunk, but the affected panel measured 0.99383x
  geometrically and Fannkuch regressed 1.07%. The phase remains a regular loop.
  Raw captures are `/private/tmp/wago-v5-arm-qpairs-once32-{off,on}.txt`.
- **ARM64 polled counted-loop latch (rejected on current main).** An
  exact, call-free, zero-arity `i32` countdown loop whose header is
  `local.get c; i32.eqz; br_if 1` and whose tail is
  `local.get c; i32.const 1; i32.sub; local.set c; br 0` now decrements and
  polls at the latch, then branches directly to the body while the counter is
  nonzero. This removes the repeated header zero-test without removing a safe
  point: the initial header still polls, every executed iteration reaches the
  latch poll, and the final zero transition is polled before exit.
  `WAGO_ARM64_NO_COUNTED_LOOP_LATCH=1` restores the old lowering. Exact-shape,
  execution, option-stat, and real-engine asynchronous interruption tests pass;
  both explicit-bounds and signal-backed semantic corpora pass too. Eight
  alternating 500 ms samples improve the five affected rows 1.04007x
  geometrically: `memory.sum` +12.44%, `float.run` +5.81%, globals +2.96%,
  iterative Fibonacci +0.06%, and arithmetic -0.71%. Exact code identity shows
  only those five modules change. Raw timing captures are
  `/private/tmp/wago-v5-arm-latch-{off,on}.txt`; code captures are
  `/private/tmp/wago-v5-arm-latch-code-{off,on}.txt`.
  The rebased current-main execution oracle then failed the FASTA and IFS
  regression workloads even with the latch rollback selected, showing that the
  combined control changes were not behaviorally identical to current main.
  The ARM64 latch and associated control changes were removed rather than
  qualifying an unsafe integration. The AMD64 latch remains independently
  retained and tested.
- **ARM64 ascending-loop latch (rejected).** A second exact recognizer covered
  top-tested signed ascending loops with an `i++` latch. Correctness and
  signal-backed corpus tests passed, but the only initially positive target was
  `xjb-mulhi.runN` at less than 1%, while follow-up fresh-process interleaving
  varied from roughly 1.2 to 3.3 microseconds and could not separate the code
  change from host thermal state. The extra proof surface was not justified by
  reproducible evidence, so the recognizer, metadata, and tests were removed.
  Raw capture is `/private/tmp/wago-v5-arm-ascending-latch-interleaved.tsv`.
- **Superseded pre-main ARM64 gate after the countdown latch.** A fresh three-sample paired
  signal-mode corpus measures 1.503339x wazero/Railshot across all 46 rows with
  44 wins. This is a 0.46% aggregate improvement over the preceding 1.496412x
  snapshot and leaves ARM64 6.43% short of 1.60x. The remaining losses are
  Fannkuch at 0.9527x and `xjb-mulhi.runN` at 0.9618x. Raw capture is
  `/private/tmp/wago-v5-arm-latch-current-paired-3x.txt`.
- **AMD64 signal-only R8 regional lease (retained candidate, uncommitted).**
  The normally fixed R8 register is admitted as the eleventh signal-mode
  regional lease only for call-free, control-free, non-bulk memory32 functions
  in non-SIMD, non-shared, single-memory modules without tables, GC layouts, or
  custom instructions. A follow-up audit traced every R8 use: ordinary
  signal-mode memory32 loads/stores use the normal allocator and honor the
  pinned-local mask; actual implicit R8 users are already excluded by the bulk,
  memory64/multi-memory, table, call/control, shared-memory, SIMD, GC, and custom
  predicates. A deliberately over-conservative prototype homed R8 at every
  scalar-memory boundary and passed correctness, but reduced the five-row gain
  to 1.79%. It was removed because it protected a role scalar memory does not
  have. The original full backend and explicit/signal differential corpus gates
  remain the correctness evidence. Seven alternating 300 ms Ryzen rounds for
  the retained form improve scalar Blake-AS 9.40%, all three BLAKE3 modes
  4.31--4.52%, and the five-row geometric mean 4.53%, while SIMD Blake-AS is
  neutral-positive at 0.26%.
  `WAGO_AMD64_NO_INTERVAL_R8_LEASE=1` restores the old lowering. Raw timing is
  `/tmp/wago-v5-amd-r8retry-panel.txt`; the conservative diagnostics are
  `/tmp/wago-v5-amd-r8-{safe-panel2,evict-panel}.txt` on the Ryzen host.
- **AMD64 proven i32.wrap elimination (rejected).** Provenance from an
  `i64.shr_u` by 32--63 allowed a following `i32.wrap_i64` to reuse the same
  zero-upper register. Backend and differential semantic gates passed and the
  scalar Blake hot function removed eight self-moves, but seven alternating
  Ryzen rounds were flat at 1.000115x across six compression rows; scalar Blake
  and SHA-256 were slightly slower. The option, fact propagation, and tests
  were removed. Raw capture is `/tmp/wago-v5-amd-wrapelim-panel.txt`.
- **AMD64 dynamically reclaimed RAX lease (rejected for correctness).** A void,
  signal-mode regional leaf could provisionally borrow RAX, with explicit
  eviction before centralized result and div/rem consumers and preservation of
  borrowed memory addresses. The complete backend suite passed, but scalar
  Blake corrupted immediately in the differential corpus. Another implicit
  RAX clobber therefore remains outside those centralized paths. The lease,
  eviction machinery, option, and catalog entry were removed before timing.
- **AMD64 unsigned-conversion cleanup (rejected).** Exact storage provenance
  proved that the copy materializing an i32 local already zeroed the upper half,
  allowing removal of the following self-move before unsigned i32-to-f64
  conversion. This removed one instruction from every iteration of the focused
  float loop and passed the backend suite, but eight alternating 500 ms Ryzen
  rounds regressed the row 0.32%. The option and lowering were removed. Raw
  capture is `/tmp/wago-v5-amd-u32convert-float.txt`.
- **AMD64 exact counted-loop latch (retained candidate, uncommitted).** A
  non-interruptible, call-free, zero-arity loop with an exact first-instruction
  `local.get c; i32.eqz; br_if 1` exit and exact terminal
  `local.get c; i32.const 1; i32.sub; local.set c; br 0` now uses the
  decrement flags to branch directly to the body. The initial zero check is
  retained, and interruptible loops are rejected so no cooperative poll can be
  skipped. Counter and body metadata reuse loop-only cold fields and do not grow
  control frames. Exact execution, disabled-path, non-exact-exit, interruptible,
  backend, and full differential corpus tests pass. Seven alternating 300 ms
  Ryzen rounds improve `memory.sum` 14.55%, arithmetic 5.74%, iterative
  Fibonacci 3.44%, globals 2.63%, and float 0.32%; the five-row geometric mean
  is 1.052259x. `WAGO_AMD64_NO_COUNTED_LOOP_LATCH=1` restores the old lowering.
  Raw capture is `/tmp/wago-v5-amd-counted-latch-panel.txt`.
- **Superseded AMD64 gate after the countdown latch.** A three-sample paired
  signal-mode corpus measures 1.537549x wazero/Railshot across all 46 rows with
  38 wins, leaving AMD64 4.06% short of the 1.60x aggregate gate. This absolute
  snapshot is below the preceding 1.544403x run despite the independently
  positive 1.052259x affected-row A/B, so it is treated as whole-corpus host
  variance rather than evidence that the exact latch regresses. The remaining
  losses are scalar Blake-AS at 0.7496x, the three BLAKE3 modes at
  0.9409--0.9588x, float at 0.9417x, raytrace at 0.9493x, SIMD Blake-AS at
  0.9666x, and SWAR `runN` at 0.9774x. Raw capture is
  `/tmp/wago-v5-amd-latch-current-paired-3x.txt`; parsed evidence is
  `/tmp/wago-v5-amd-latch-current-summary-3x.txt`.
- **AMD64 over-conservative R8-eviction diagnostic.** With unnecessary
  scalar-memory eviction in the R8 lease and all later rejected prototypes
  removed, a fresh
  three-sample 200 ms paired corpus measures 1.569788x wazero/Railshot across
  all 46 rows with 38 wins. That snapshot is 1.92% short of the 1.60x aggregate
  gate. The remaining losses are scalar Blake-AS at 0.7426x, the three BLAKE3
  modes at 0.9043--0.9241x, SWAR `runN` at 0.9526x, raytrace at 0.9543x, SIMD
  Blake-AS at 0.9559x, and float at 0.9607x. The eviction was removed after the
  opcode audit proved it unnecessary, so this is diagnostic rather than the
  final retained-tree gate. Raw capture is
  `/tmp/wago-v5-amd-r8-evict-current-paired-3x.txt`; the local copy is
  `/private/tmp/wago-v5-amd-r8-evict-current-paired-3x.txt`.
- **AMD64 destructive rotate sinking (rejected).** Using a legacy destructive
  rotate whenever the surrounding expression supplied the final destination
  kept RORX for borrowed inputs and passed focused backend checks, but seven
  alternating Ryzen rounds regressed every hash row: the seven-row geometric
  mean was 0.98423x, with scalar/SIMD Blake-AS about -1.2%, BLAKE3 about
  -1.9--2.4%, and SHA-256 -2.4%. The option, lowering, and tests were removed.
  Raw capture is `/tmp/wago-v5-amd-rotate-sink-panel.txt`.
- **AMD64 VEX integer-to-float merge source (rejected).** Reusing an already
  preloaded scalar constant as VCVTSI2S{S,D}'s architecturally ignored upper-
  lane source removed the separate dependency-breaking VPXOR without reserving
  a register. Encoder and focused backend checks passed and the optimization
  fired in float-heavy real modules, but eight alternating Ryzen rounds were
  neutral-negative on all five affected rows (0.99847x geometric mean). The
  encoder, option, and lowering were removed. Raw capture is
  `/tmp/wago-v5-amd-i2f-panel.txt`.
- **AMD64 low-register-first regional leases (rejected).** Reordering the
  existing eligible lease set to give early lifetimes RBP/RDI before extension
  registers removed 93 bytes from scalar Blake's 6256-byte hot function and 42
  REX prefixes without adding state or changing register eligibility. Despite
  the code-size win, eight alternating Ryzen rounds were flat-negative at
  0.99928x across seven integer/hash rows; scalar Blake and SHA-256 each lost
  about 0.25%. The option, alternate order, and tests were removed. Raw timing
  is `/tmp/wago-v5-amd-lowregs-panel.txt`; code evidence is
  `/tmp/wago-v5-amd-lowregs-{off,on}-explain.txt`.
- **AMD64 32-byte large straight-line alignment (rejected).** Aligning large,
  call-free, control-free functions to a full fetch block changed layout only
  and added no runtime work. Eight alternating Ryzen rounds were nevertheless
  flat-negative at 0.99936x across seven hash/integer rows: scalar Blake and two
  BLAKE3 modes improved slightly, while SHA-256, BLAKE3 derive-key, and SWAR
  regressed enough to outweigh them. The option, wider padding, and tests were
  removed. Raw capture is `/tmp/wago-v5-amd-align32-panel.txt`.
- **AMD64 per-function SIMD relaxation for R8 (rejected).** Replacing the
  module-wide SIMD exclusion with the existing exact per-function SIMD flag
  passed serial/parallel backend checks and both differential corpus modes,
  while admitting R8 only into scalar helpers. Eight alternating Ryzen rounds
  nonetheless regressed the five SIMD-module rows 0.21% geometrically, led by
  SIMD Blake-AS at -1.11%. The stricter module-wide exclusion remains. Raw
  capture is `/tmp/wago-v5-amd-r8-simd-panel.txt`; firing evidence is
  `/tmp/wago-v5-amd-r8-simd-explain.txt`.
- **ARM64 open-coded multiply-high matcher (rejected before timing).** A bounded
  prototype recognized the standard four-limb unsigned 64x64 multiply-high
  arithmetic and reached the real xjb loop prefix. Replacing the arithmetic
  with UMULH would also erase the source program's writes to intermediate locals;
  proving those scratch values dead across the loop backedge requires a real
  local-liveness contract, not a fixture-shaped byte matcher. The prototype was
  removed before timing rather than weakening Wasm local semantics. Prefix-only
  diagnostic evidence is `/private/tmp/wago-v5-arm-mulhi-explain.txt`.
- **AMD64 strict RSI regional lease (rejected).** Lending RSI under the same
  call/control/bulk/memory64/SIMD/table/GC/custom-free proof as R8 first appeared
  neutral only because an over-conservative scalar-memory gate disabled the
  target functions. The corrected twelfth, last-choice lease reached 12 active
  locals and passed the backend suite, but immediately corrupted scalar Blake
  and every BLAKE3 semantic vector under guard-page execution. RSI therefore
  has an implicit role outside the modeled exclusions. The prototype was
  removed without timing; `/tmp/wago-v5-amd-rsi-panel.txt` is retained only as
  invalid earlier evidence and is not a performance result.
- **AMD64 hot scalar local homes (rejected).** Swapping equal-type single-slot
  local homes by existing scan hotness moved frequently referenced values into
  the signed disp8 window without changing frame size or representation. It cut
  scalar Blake's hot function from 5600 to 5336 bytes and passed the backend and
  semantic corpus, but seven alternating Ryzen rounds across ten affected and
  control rows measured 0.998794x overall, including arithmetic at -0.82% and
  SWAR `runN` at -0.39%. The prototype was removed. Raw timing is
  `/tmp/wago-v5-amd-hothomes-panel.txt`; code evidence is
  `/tmp/wago-v5-amd-hothomes-{off,on}-explain.txt`.
- **Exact-current compile/resource refresh (passing).** After rebasing onto
  current main and removing the unsafe ARM64 control integration, a fresh
  three-sample 100 ms screen across 42 paired compilation rows measures a
  3.829586x Wazero/Railshot compile-time geomean and 14.703388x fewer compiler
  bytes/op on the Apple M4 Max. The pinned Ryzen 7 7800X3D run measures
  3.884518x faster compilation and 11.152619x fewer compiler bytes/op.
  These clear the requested 2.60x latency, 12x ARM64 memory, and 9x AMD64 memory
  floors. Raw captures are
  `/private/tmp/wago-v5-main-final-{arm,amd}-compile-3x.txt`; the Ryzen-side
  original is `/tmp/wago-v5-main-final-amd-compile-3x.txt`.
- **Final retained AMD64 execution gate.** After removing the rejected RSI,
  hot-local-home, destructive-rotate, alignment, and over-conservative R8
  diagnostics, the exact synced tree passes the complete AMD64 backend and
  signal-backed differential/semantic corpus. A fresh three-sample 200 ms
  paired run on pinned Ryzen CPU 7 measures 1.555391x wazero/Railshot across
  all 46 rows with 38 wins, leaving a 2.87% shortfall to the 1.60x stretch
  target. The losses are scalar Blake-AS at 0.751x, the three BLAKE3 modes at
  0.939--0.964x, float at 0.947x, raytrace at 0.949x, SIMD Blake-AS at 0.958x,
  and SWAR `runN` at 0.970x. Raw capture is
  `/private/tmp/wago-v5-main-final-amd-paired-3x.txt`; the Ryzen-side original
  is `/tmp/wago-v5-main-final-amd-paired-3x.txt`.
- **Matched current-main execution delta.** Three-sample 200 ms paired runs of
  `origin/main` and this exact tree in the same final window measure a 1.006433x
  ARM64 candidate/main geomean with 26/46 improved rows and a 1.109867x AMD64
  candidate/main geomean with 32/46 improved rows. AMD64 `dispatch.apply` is
  9.8528x faster than current main. The corresponding absolute wazero/Railshot
  geomeans move from 1.359837x to 1.370086x on ARM64 and from 1.414729x to
  1.555391x on AMD64. Raw base captures are
  `/private/tmp/wago-v5-main-base-{arm,amd}-paired-3x.txt`; candidate captures
  are `/private/tmp/wago-v5-main-final-{arm,amd}-paired-3x.txt`.
- **Broad-suite status.** The final current-main tree passes a fresh complete
  local `go test ./... -count=1`, including compiler, runtime, regression,
  semantic, spectest, and Wine bootstrap coverage. The exact synced Ryzen tree
  also passes the complete AMD64 backend plus signal-backed differential and
  semantic corpus gates.
