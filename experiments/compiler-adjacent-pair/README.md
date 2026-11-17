# Completed adjacent-pair polling: unqualified source PoC

No builds/tests/benchmarks yet. Existing bounded-vector experimental tag is still
required; ordinary production builds remain unchanged. This branch builds on
B=4 polling (B=8 with the additional vector-poll8 experimental tag).

Exact GEMM artifact 8ba9ee8cafd9bf19b1547fe9127c861505daeecfe84c0516801776fddfcabd79
has an innermost 42-instruction loop updating two adjacent cells, incrementing a
byte offset by 16 until 560. The original source already contains the two updates.
The existing adjacent-output matcher preserves their FP operand trees and proves
matching adjacent accesses. This is not scalar invariant-cell recurrence.

Admission runs existing classification first, then accepts interruptible adjacent
plans only with one packed output pair and contiguous stride-16 moving accesses.
Scalar recurrences and multiple output pairs stay excluded. Full-range and alias
checks retain the original checked fallback and trap order on failure. No new
reassociation, FMA transformation or source unrolling is introduced.

Interruptible adjacent plans are XMM-only initially. iterationsPerVector()==1:
one group is one original source iteration containing two f64 outputs. Existing
remaining-group countdown and B4/B8 cadence therefore count source iterations,
not individual output lanes. Both lanes are stored before the poll. Final-zero
polling remains mandatory, as do entry and checked-fallback checks. Odd counts,
including GEMM's 35, need no YMM remainder path.

The four existing GP temporaries and existing FP allocation are retained. Polls
save gp[0] in a dedicated spill and recreate counter flags afterward. XMM adjacent
exit publication uses slots26/27; the added interruption spill is slot28, so it
does not overlap. No deferred scalar accumulator publication is admitted.

Cancellation cadence is intentionally bounded work, not exact per-iteration or
wall-clock behavior. The trap sees a fully completed source-iteration prefix.
Shared memory, EH, GC-root and profile exclusions remain unchanged.

Tests added (NOT RUN): exact GEMM planner classification and differential results;
SSE2/AVX target settings with XMM adjacent emission; deterministic injected entry,
header, batch, final-only and fallback interruption; B/2B boundaries and35;
partial-overlap, OOB and wrapping fallback; memory-prefix/trap-origin checks and
engine reuse. Existing adjacent fixtures provide precise scalar final-lane
results. Full default/experimental/checker suites, B8 runs, exact145 oracles,
image audit, true cold compilation, RSS and paired workload execution remain
required before acceptance. No claim of satisfying any joint performance gate.

Additional unrun GEMM-specific checks require the emitted fourth packed-batch
poll in the single-loop fixture AND successful execution with diagnostic
fallback trapping enabled. This prevents scalar fallback equality from passing
as packing proof. Actual GEMM deterministic injection checks completed prefixes,
entry/final/fallback traps and reuse. FP cases compare finite outputs and zero
sign bit-for-bit; noncanonical quiet NaN inputs permit Wasm arithmetic-NaN
payload/sign choice while requiring quiet NaN results and unchanged other memory.

## Diagnostic qualification plan, separate from correctness

Existing CodegenStats now records bounded labels: region-attempt,
region-reject-interruptible-disabled/context/blocktype/plan/output-class,
region-plan-candidate, region-reject-interrupt-checkpoint, and
region-emitted-interruptible-adjacent. Existing no-GP and exit-reject counters
remain. These are coarse gate reasons, NOT a claim to distinguish every planner
rejection. No shadow parsing or extra scans are added; normal-build stats are
compile-time disabled and allocate no diagnostic maps. Use `wago_codegenstats`,
NOT `wago_profile` (profiling itself excludes interruptible region loops).

Before qualification capture these per-function counters and native images for
the exact full GEMM artifact with experimental polling disabled and enabled;
separately retain the semantic oracle outcome. The baseline's interruptible gate
rejects before planning, so its counter does not imply which matcher would have
accepted it. The enabled run must show actual emitted-adjacent count/code change,
not just a plan candidate. Add the same diagnostic-only counters to older
baseline source if comparing a separate checkout, without changing admission.
No full-artifact diagnostic run has yet been performed for this source checkpoint.

Issue #1060's Rust matmul example is a different artifact, not this PolyBench
GEMM. This experiment does not address its broader f32, integer-memory or ARM
coverage and does not claim to resolve that issue. No matcher budget widening.
