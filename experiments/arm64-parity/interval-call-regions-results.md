# Regional local caching around calls: retained arm64 optimization

Retained and default on as `interval-call-regions`. Disable per compilation with `WithOptimization("interval-call-regions", false)`, or process-wide with `WAGO_ARM64_NO_INTERVAL_CALL_REGIONS=1`. Historical prototype results below include rejected policies and mitigations. Extend existing bounded regional integer-local caching to functions with calls. Preserve canonical homes and flush live operands before every guest call, tail call, memory growth, and prefixed bulk/GC instruction. Existing structured control boundaries remain unchanged. Next-use event lists include these new barriers. Cached constant lease release restores the regional register limit.

Admission excludes interruptible/shared-memory functions, GC roots/layouts, EH, SIMD, table mutations/tables, custom instructions, and partially inlined functions. Existing body/local bounds and hotness rules apply; no corpus-specific admission. Fixed-scratch bulk operations can be admitted because regional local leases are cleared before their lowering.

Profiler evidence: retained fastfloat and PCRE2 show substantial stack-local traffic. A static disassembly count found only 2 safe redundant indexed ADDs in register allocation and 3 in fastfloat, so that narrower lead was not implemented. Adjacent copy/shift elimination had already been rejected after allocation and layout mitigation; it was not retried.

The new mathematical execution test holds 32 locals across a call and optional memory.fill, with a live intermediate sum crossing the helper. Caching on/off, signal/explicit bounds, zero/one/overflow inputs, and diagnostic admission are covered. Validation is queued through the shared device lock. No performance or correctness claim yet.

The first queued build failed before execution: event-list setup referenced out-of-scope `hints`. Fixed by passing the already-computed call-boundary decision as an argument; existing event-list tests pass `false`. Original build failures remain in `interval-call-regions-tests.txt` and `interval-call-regions-enabled-tests.txt`. No oracle, timing, or profiler run occurred in that failed batch. A single stop-on-failure retry script now runs default-state diagnostics, enabled-state qualification, then focused timing/profile capture.

## Initial qualification and measurements

The corrected default-state and enabled diagnostic suites passed. All 148 signal contracts passed, plus all 46 cached-core explicit-bounds contracts. Only fastfloat and kissfft native images changed; PCRE2 is unchanged and its timing variation is not an optimization effect.

The first score-based policy regressed fastfloat execution 3.70% and paired compilation 6.67%; native size grew 41,504→48,392 bytes. Its fresh profiler capture shows repeated loads from temporary spill slots. Before rejection, next-use admission was extended to the smaller call-making regions: native size drops to 42,096 bytes (kissfft 33,476→33,588). All contracts qualify again. Reversed timing suggests roughly 9% fastfloat execution improvement, with paired compile costs +8.92% (fastfloat) and +3.35% (kissfft).

A fresh-instance-per-sample, same-thread interleaved execution harness confirms fastfloat -7.61% and kissfft +0.10%, over 12 rounds at 350ms per state. It requests user-interactive QoS and locks an OS thread; it does not pin a physical core. Oracle checks are included equally in both states, so these absolute times should not be compared to the historical w2c2 captures.

The separate immediate-skipping helper preserves all 148 native images but worsens fastfloat compilation by 4.90% relative to the next-use prototype; paired on/off cost grows to +12.97%. That helper has been replaced with inline common-immediate reads. Inline decoding and packed 16-bit event metadata trials are queued. Packing relies on the existing 16-KiB source-body bound, one event per opcode, and includes terminal-link and maximum-offset tests. No default enablement decision yet.

## Inline decoding and compact event metadata completed

Both trials pass diagnostic tests, all 148 signal contracts, and byte-for-byte native equality to the next-use prototype. Inline decoding improves the cross-process compile median relative to the separate helper by roughly 1.1% (fastfloat) / 1.6% (kissfft), but paired enablement costs remain noisy (+11.32% fastfloat in that capture). Packed events lower fastfloat compile allocation volume from about 496,504 to 473,974 bytes (same 300 allocations), and kissfft from about 302,995 to 295,566 bytes (same 299). Paired enablement costs are +8.40% fastfloat and +3.72% kissfft. There is no execution-code change from either mitigation.

Fresh native profiling still shows temporary spill-slot loads, so the current source raises the transient allowance from 3 to 7 registers only for admitted call-making regions. This floor trial is queued, with exact checks and paired compile/exec measurements on the two changed core workloads. Existing call-free regional policies keep their original floor. Source snapshots before the floor change are saved as `interval-call-packed-before-floor-*.txt`.

Go CPU profiles are dominated by runtime signal/wait functions and are not reliable compiler-cost weights here. Disassembly does independently show duplicate bounds checks in `nextIntervalEvent`; a staged native-int index trial is queued after the floor trial. It verifies generated code remains identical on the changed subset and records before/after Go disassembly. The shared lock is held by a live other-agent benchmark; our coordinators continue polling.

## Final retained implementation and validation

The call-making cache reserves seven transient registers, keeps bounded next-use events in 4-byte records, consumes common immediates inline, and compares event indices to native slice lengths. Go disassembly confirms the event-query function shrinks from 73 to 41 lines and both redundant panic checks disappear. A focused native dump comparison confirms that bounds-query cleanup changes no guest instructions.

Final same-thread paired comparisons:

| Contract | Exec change | Compile change |
|---|---:|---:|
| fastfloat decimal-parse | -8.67% | +7.90% |
| kissfft complex-roundtrip | +0.21% (flat) | +3.18% |

Execution samples are from the transient-floor trial; the later bounds-query cleanup produces identical guest images. Compile samples are from the final bounds-query trial. These are separate captures on the same device with interleaved immutable on/off options, not contemporaneous w2c2 comparisons. Fastfloat compile medians are 1,074.567→1,159.443 µs; execution is 0.742900→0.678516 µs including equal oracle checks. Roughly 1,300 repeated invocations repay the measured compile increment. This is a moderate compile cost with a confirmed recurring execution benefit, not the severe compile-only regression the user asked us to discard.

Retained-state diagnostics, encoder/runtime/catalog checks, and ordinary dedicated-arm64 backend tests pass. All 148 unchanged contracts pass with both signal and explicit bounds. With this option disabled, all 148 native images exactly match the previously retained scoped-constant baseline. Enabled images change only fastfloat and kissfft; no application image changes. Exact result checks, overflow inputs, live locals across calls and bulk operations, nested lease budget restoration, maximum event coordinates, and oversized-body fallback are covered.

All benchmark reservations were released normally. The broader parity goal remains incomplete; many recorded w2c2 gaps remain.
