# Railshot V4 corpus performance plan

Status: ARM64 performance gates met; correctness and AMD64 qualification in progress.

Branch base: `a83fc0040c8fd85edebeaa1e59fd291f76712aa6` (`origin/main`,
including PR #568).

## Objective

Make Railshot at least 30% faster than wazero on the geometric mean of paired
execution rows across the major executable corpus, while retaining at least a
2.7x geometric-mean full-compilation advantage and using 6-7x less compiler
heap per operation. The final claim requires qualified ARM64 and AMD64 results;
one architecture or a selected workload subset is not sufficient.

Correct WebAssembly behavior, trap ordering, cancellation, GC roots, descriptor
ownership, instance isolation, and bounds checks are hard constraints. A speedup
that weakens one of them is rejected rather than counted toward the target.

## Measurement contract

- Compare one exact candidate commit with one exact current-main commit.
- Use the same machine, Go toolchain, bounds mode, corpus bytes, wazero version,
  CPU affinity where available, and `GOMAXPROCS=1` within every pair.
- Alternate base/candidate order and retain complete raw samples.
- Compute per-row medians first. Aggregate execution, compilation, and heap
  ratios with geometric means so large programs do not numerically drown out
  small ones.
- Report every paired major-corpus row, including regressions. Unsupported or
  unavailable rows are identified rather than silently removed.
- Treat changes below the measured noise floor as ties.

The initial ARM64 application screen uses signal-backed bounds because that is
the production guard-page configuration on supported hosts. Explicit-bounds
correctness and performance remain mandatory gates.

## Generality rule

An optimization is admissible only when its predicate is expressed in semantic
or machine facts that can occur in unrelated programs, such as value ranges,
aliasing, structured control, liveness, register pressure, instruction effects,
CPU features, or proven descriptor ownership.

The following are forbidden:

- module, export, source-language, producer, or corpus-name checks;
- matching a constant sequence whose only justification is one benchmark;
- changing benchmark inputs, iteration counts, host setup, or result checking;
- unsafe direct entry or dispatch selected without complete state, ABI, bounds,
  type, ownership, and lifecycle proofs;
- removing cancellation, GC, bounds, null, signature, or cross-instance checks.

Each retained optimization must show hits in multiple independently sourced
modules or implement a generally useful target lowering with direct semantic
tests. Corpus-neutral negative controls are part of qualification.

## Compiler shape

Railshot remains a forward single-pass semantic compiler. Existing decode-time
and fused function-hint scans may collect bounded pointer-free facts. The only
new post-emission work allowed by default is a bounded finalization step over
explicitly recorded sites; it must not decode arbitrary native instructions,
revisit the Wasm body, build a CFG or SSA graph, or recompile a function.

The tuning step may:

- choose among pre-recorded legal encodings or layouts;
- relax branches and remove recorded dead reservations or NOP holes;
- apply proof-carrying local rewrites whose operands, clobbers, trap behavior,
  and byte ranges were recorded during emission;
- consume bounded metadata stored in reusable worker scratch or the uncommitted
  tail of the native buffer.

It must fail soft to the already-correct emitted form on overflow, ambiguity,
unsupported control, or insufficient scratch.

## Portfolio lanes

1. **Baseline and attribution**: establish paired ARM64/AMD64 execution,
   CompileFull, heap, allocation, and native-size ledgers; use `bench/cmd/explain`
   to connect losses to physical codegen counters.
2. **Bounded native finalization**: extend the existing recorded-site finalizer
   only where a compact proof record can select or delete an encoding safely.
3. **Physical register debt**: turn existing local-event and residency telemetry
   into bounded, architecture-aware decisions without a second Wasm walk.
4. **Expression lowering**: improve generally recurring address, immediate,
   three-operand, flags, compare/select, rotate, multiply-high, and SIMD forms.
5. **Memory proofs**: co-design address formation and bounds discharge while
   preserving overflow, trap order, memory growth, calls, and alias invalidation.
6. **Calls and control**: remove redundant transfers and improve fallthrough or
   cold layout only under exact ABI, effect, ownership, and branch facts.

Every major active optimization is registered in the optimization catalog and
is independently configurable through Wago's existing optimization-selection
surface. Diagnostic-only counters need not become user-facing switches.

## Promotion gates

A candidate may remain in the giant branch only after:

1. focused semantic and structural tests pass with the option both enabled and
   disabled;
2. relevant trap, cancellation, GC, cross-instance, codec, and bounds-mode tests
   pass;
3. the executable semantic corpus passes on the affected architecture;
4. paired multi-row measurements show a general benefit without a material
   unrelated regression;
5. compile-time and heap ratios remain above the portfolio floors;
6. native size and compiler scratch stay bounded and measured.

Rejected experiments and their measurements are recorded so they are not
quietly reintroduced later.

### Rejected experiments

- **Compact cached loop polls (ARM64):** deleting the one-word phase placeholder
  improved `float.run` in a focused screen but shifted later function entry
  addresses and regressed unrelated `swar-pack-parse` exports by up to about 2%.
  Keep the dependency-breaking trap-cell cache, preserve native layout, and
  revisit instruction deletion only with module-level layout control.
- **Universal 32-byte loop alignment (ARM64):** a 16-row focused screen scored
  `0.998x` geomean, did not recover the dependent-load row, and regressed tiny
  SWAR/multiply-high rows by roughly 2--3.5%. Retain the 16-byte policy.
- **Universal indexed-displacement folding (ARM64):** applying the existing
  dense-memory cover to every sparse memory function made dependent pointer
  chasing `0.732x`. Excluding full-width self-load recurrences removed that
  failure, but the remaining ten-row screen was effectively flat. Retain the
  existing density threshold.
- **Countdown-loop latch rewrite (ARM64):** the exact size-preserving rewrite
  improved `fib_iter` but was geometrically flat/slightly negative over five
  paired 46-row screens, while removing it improved application compilation by
  `1.011x`. It was removed from the portfolio rather than keeping a narrow win.
- **Reusable loop-constant candidate pointer (ARM64):** moving the bounded
  candidate array behind a module-reused pointer was `0.983x` in an isolated
  compile A/B because pointer aliasing and indirection outweighed zeroing savings.
- **Pointer-backed loop-constant result views (ARM64):** shrinking `funcHintView`
  by moving its four selected constants behind a pointer was `0.996x` in an
  isolated application compile A/B and was reverted.
- **Standard-library instruction-word append (ARM64):** replacing the encoder's
  four-byte append with `binary.LittleEndian.AppendUint32` preserved bytes but
  compiled at `0.995x`; retain the existing allocation-free byte append.
- **Lower global-residency admission bar (AMD64):** lowering the general second
  global threshold improved scalar BLAKE-AS by `2.38%`, but regressed SIMD
  BLAKE-AS by `1.57%` and made the complete 46-row geomean `0.08%` slower over
  six alternating production guard-page rounds. Retain the existing threshold.
- **Eleventh interval-region register (AMD64):** increasing the regional limit
  from ten to eleven was flat on a focused production guard-page screen
  (`0.04%` slower geomean) and did not improve BLAKE3, BLAKE-AS, CoreMark, JSON,
  SHA-256, or zlib. Retain the existing pressure cap.

## Current baseline

ARM64 baseline captured on an Apple M4 Max with Go 1.26.5, wazero 1.9.0,
`GOMAXPROCS=1`, and signal-backed bounds at base `a83fc004`:

- all executable rows (46 pairs): Railshot/wazero execution geomean `0.935x`;
- application rows (19 pairs): Railshot/wazero execution geomean `1.136x`;
- application CompileFull rows (17 pairs): wazero/Railshot geomean `2.825x`;
- application compiler heap (17 pairs): wazero/Railshot B/op geomean `16.066x`;
- Wago execution rows: `0 B/op`, `0 allocs/op`.

The first retained experiment lets compiler-proven memory-free direct entries
use the prepared integer ABI under signal-backed bounds without changing the
guarded route for any memory-touching function. Its complete 46-row screen is
`1.142x` faster than the exact-base Wago medians and scores `1.036x` against the
wazero medians captured in the same run. Focused prepared-call latency falls
from `82.9 ns` to `28.8 ns` (median of seven, `2.88x` faster). This closes the
known signal-router regression but does not yet satisfy the `1.30x` execution
gate.

The current retained ARM64 portfolio additionally:

- moves redundant i32 address canonicalization out of hot paths while retaining
  an explicit rename for proven full-width self-load pointer recurrences;
- extends the registered loop trap-cell cache to call-free memory-free loops,
  except caller-register-preserving leaves;
- selects direct CBZ/CBNZ forms for `eqz` branches while preserving later code
  phase with cold-tail padding;
- registers a caller-clobber-proven light prepared entry thunk that preserves
  `entersyscall`/`exitsyscall` and all required Go state; and
- registers cold-call local pinning when every call is direct, local, and
  outside loops;
- folds shifted-register ALU operands and exact floating-point immediates using
  target encodability rather than workload patterns;
- reuses adjacent indexed bases only under exact unchanged-register and
  non-writeback instruction proofs;
- keeps up to four costly loop integer constants in otherwise-idle registers,
  selected from an eight-candidate bounded streaming sample; and
- uses sparse per-function markers to avoid searching loop-constant metadata in
  functions that cannot own it, while skipping load-provenance reconstruction
  for functions that never touch linear memory.

The latest complete 46-row paired screen before cold-call pinning and default
register merge scores `1.090x` against wazero and `1.204x` against exact-base
Wago, with 31 wins and 15 losses. Cold-call local pinning separately improves
QOI encode/decode `1.240x`/`1.131x` and scores about `1.012x` across all 46 Wago
rows. Six alternating subprocess rounds qualify the already-registered scalar
`reg-merge` path at `1.0165x` across affected and control rows, including JSON
serialization at `1.038--1.044x`, SIMD JSON decode at `1.073x`, and fannkuch at
`1.025x`; it is now default-on.

A complete 46-row checkpoint after those changes and the initial call-free
bounded-entry path scores `1.163x` against wazero with 36 wins and 10 losses.
The registered `prepared-bounded-entry` optimization keeps only byte-backed,
loop-free, call-free functions on the Go scheduler while they execute native
code; its compiler proof is capped at 96 Wasm body bytes. It skips only the
`entersyscall`/`exitsyscall` pair, not any Wasm trap, bounds, ownership, or ABI
check. The full-register and caller-clobber-only native thunks remain separate.
Repeated GC under the race detector and a five-second CPU-profiled hot loop
passed; Go 1.26.5's runtime rejects asynchronous preemption when SP is outside
the goroutine stack and records an unknown JIT PC as external code.

The bounded module finalizer now admits acyclic local calls and private
immutable-table dispatch only after every reachable target is independently
bounded. It starts from leaves, so recursive SCCs fail closed, and caps the
transitive proof at 32 calls deep and 4 KiB of conservative Wasm work. Dynamic
`call_ref`, helper calls, loops, imported functions, mutable/exported tables,
AST-only bodies, and custom instructions retain scheduler-releasing entry.
Focused signal-bounds medians moved `tiny` from about `32.6 ns` to `20.0 ns`,
declared-local `mulhi` from `33.4 ns` to about `20--21 ns`, and immutable-table
`dispatch` from about `34.2 ns` to `22.2 ns`, all at zero allocations.

## Current ARM64 qualification

The final working-tree candidate, including the direct-entry stale-trap reset,
was requalified on Apple M4 Max, Go 1.26.5,
wazero 1.9.0, `GOMAXPROCS=1`, and signal-backed bounds:

- 46 paired execution rows, five 300 ms samples: wazero/Railshot median-row
  geomean `1.4099x`, 38 wins and 8 losses;
- every Railshot execution row: `0 B/op`, `0 allocs/op`;
- 17 application CompileFull rows, five 200 ms samples: wazero/Railshot
  median-row geomean `2.8952x`;
- compiler bytes/op geomean: wazero/Railshot `15.9745x`;
- compiler allocations/op geomean: wazero/Railshot `13.8276x`.

Raw evidence is retained in `/private/tmp/wago-v4-final2-arm-exec-5x.txt` and
`/private/tmp/wago-v4-final-arm-compile-5x.txt` until publication.

## Current AMD64 qualification

On a Ryzen 7 7800X3D with Go 1.22.2, pinned to CPU 7, the AMD64 extension adds
the same registered bounded prepared-entry policy to the backend's existing
small register-ABI leaves. The proof rejects loops, calls (including calls
removed only by inlining and GC/atomic helper calls), linear memory, bulk
operations, table mutation, EH, custom instructions, bodies over 96 bytes, and
functions with more than eight locals.
Tiny call-heavy host-boundary rows improve by roughly `3.1x` against the exact
candidate with the scheduler transition retained.

A five-sample, 300 ms production guard-page comparison against wazero scores a
46-row median geomean of `1.4170x`, with 34 wins and 12 losses. Representative
ratios are `3.322x` for tiny, `3.051x` for branches, `3.224x` for SWAR pack,
`0.674x` for scalar BLAKE-AS, `0.861--0.888x` for BLAKE3, and `0.963x` for SIMD
BLAKE-AS. On the same exact source, 17 application CompileFull rows score
`3.1008x` latency, `9.7265x` bytes/op, and `12.6172x` allocations/op against
wazero. Raw evidence is retained in `/private/tmp/wago-v4-final2-amd-exec-5x.txt`
and `/private/tmp/wago-v4-final-amd-compile-5x.txt` until publication.

Direct prepared entries now perform the same interruption-preserving stale-trap
reset as ordinary wrapper entry. The successful-call path is one atomic load;
the cold path clears prior trap metadata without erasing a concurrent interrupt.
Cross-export regressions prove that a memory trap in one export cannot abort a
later direct export before it executes.

Disabling BMI2 exposed a pre-existing interval-residency correctness defect in
the destructive rotate lowering. The retained fix materializes and protects the
entire deferred source before allocating a distinct rotate destination. Published
BLAKE3 vectors now pass with `bmi2-rorx` explicitly disabled; the default RORX
path remains faster and byte generation on BMI2 hosts is unchanged.
