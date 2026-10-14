# Checked affine integer table leaves

Retained ARM64 lowering at `49145ddf` removes indirect-call overhead for a bounded
private immutable table of pure numeric leaves. A shared proof analyzes the
actual final active-element mapping, including later overrides, and establishes
that each leaf returns `Scale*x + Bias + Step*index` modulo 2^32. All target types
must agree exactly; holes, imports, table initializers, table64, nonlinear or
trapping code, and unsupported expression forms fall back to ordinary dispatch.
The proof uses fixed storage and caps table entries and leaf instruction count
at 16. No workload identity appears in the compiler.

Lowering requires the existing private/immutable-table proof and enabled
inlining, with ordinary caller state (no interruption, EH, GC-root frame, inline
remapping, or custom instructions). It evaluates arguments and pending traps in
order, checks the original unsigned index range, and computes the result with
wrapping i32 arithmetic. The table and its function descriptors remain intact;
no runtime ABI assumption is required for the inlined pure expression.

The inspector selects exactly two workloads from the complete 266-workload
inventory. Both were measured, together with two unchanged controls, against
retained Wago `34b940db` and SIMD w2c2 Clang `f303ed27` on Apple M4 Max. Two runs
reverse Wago capture order: 24 total captures, four phases, three launches × five
samples per successful phase, RSS and code measurements. There were no collector
errors or resource failures. Exact sources, contracts, raw captures, manifests,
and validation evidence are in [affine-i32-table.json](affine-i32-table.json).

| Steady workload | First change | Reversed change | Current / peer time |
| --- | ---: | ---: | ---: |
| core-tab/indirect-call | −70.94% | −71.21% | 0.527× / 0.520× |
| threads/table-indirect-call | −11.03% | −17.53% | Peer profile unsupported |
| compact-imports/ordinary-baseline (control) | −0.10% | −0.74% | 3.563× / 3.617× |
| wago/dispatch/apply (control) | 0.00% | 0.00% | 0.126× / 0.144× |

Ordinary indirect calls moved from approximately 318–327 us to 92–94 us, versus
175–181 us for the peer. Their native image shrank from 1088 to 656 bytes; the
shared-memory case shrank from 1232 to 800 bytes. This resolves the measured
ordinary indirect-call gap on M4. Other feature and main-workload gaps remain;
this is not completion of the parity goal or an AMD64 speed claim.

Three alternating compiler-cost pairs covered 218 Core 3 modules and 27 thread
modules, 100 passes each, GOMAXPROCS=1. Median elapsed time changed −1.15% /
+0.88%; Core 3 code bytes were unchanged and allocation bytes rose 0.041%.
Thread code bytes fell 2.68% and allocation bytes fell 0.155%. Median peak RSS
changed +2.07% / −1.97%. These small timing differences do not establish a
compiler-speed improvement; no material compiler cost increase was observed.

Validation: 250 tagged proof/selection/execution checks and 166 ordinary public
execution checks passed without skips. Five further identity-result checks
verify high input bits do not escape into i32 results or live wide prefixes.
The matrix covers private/exported and mutable-table selection, both register
ABI modes, fresh/reloaded artifacts, wrapping arithmetic, live values and dirty
loop locals, null/out-of-bounds handling, wrong signatures, and argument-load
trap precedence even when the target discards its argument. Core 3 and official
atomic conformance passed (3.482 s). Native x86_64 Remote passed the portable
execution and ABI fallback tests, exercising existing scalar dispatch.

The bare inspector lacks configured GC helpers for four GC modules. An independent
section audit of their exact artifact hashes verifies that all four have zero
defined tables; the new entry proof requires exactly one, so none can select this
lowering. Official conformance is independently passing.
Private paired captures will only reach the local site through a complete
newer source snapshot. No production publication or push occurred.
