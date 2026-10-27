# Final cast and field read (2026-10-21)

A final `ref.cast T; struct.get T field` resolved the same object twice for a
numeric field. Vector fields instead entered the fused Go helper. The existing
final reference-field resolver already checks the cast and field extent together.
Use it for packed integers, ordinary integers, floats, and vectors too. Load the
value immediately from its returned address, with no intervening safepoint.
Collector-reference results retain their existing exact root publication.

Static validation permits a downcast from `eqref`; it does not prove the object's
run-time type matches. The fused resolver still checks that dynamic cast once.
A successful cast proves the following field access's type. Both original
operations' null traps, foreign-reference rejection, handle/backing bounds, and
field-extent checks remain. Open targets retain the existing unfused path. The
compiler reports an error if the final field layout disagrees with its storage.

The code-generation test fails against parent production sources for every
width because the scalar/vector fusion is missing. The runtime fixture checks
all packed sign modes, integer and float bits, both vector lanes, and null/type
traps with normal, collect-every-allocation, and Tiny collectors. Values survive
three verified collections. The focused code-generation test passes with `wago_codegenstats` and
`wago_regalloccheck`. The full diagnostic-tag compiler suite also exposes existing
optimization-counter failures: a parent-production overlay reproduces
`TestCommuteSelfUpdate` and `TestEntryArgPinFires`. Those failures remain visible
in the retained local logs; they do not execute GC operations. The routine
register-check lane is recorded separately.

Five alternating 200 ms samples per version, Ryzen 7 8845HS, CPU 15,
`GOMAXPROCS=1`, collection enabled, 8192 reads per warmed invocation:

| Read | Parent median (range) | Fused median (range) | Speedup |
| --- | ---: | ---: | ---: |
| i32 | 55.855 µs (54.848–56.240) | 26.012 µs (25.802–63.191) | 2.15x |
| v128 | 623.027 µs (617.921–628.564) | 29.865 µs (29.670–30.266) | 20.86x |

One fixed i32 sample is an outlier; it is retained in the raw data. Both binaries
use identical new fixtures and benchmark code. The baseline overlay replaces
only `gc_struct.go` and `gc_direct.go` with parent production files. See raw
samples and calculated medians in `cast-field-read-results/`.

V8 13.6, Node 24.21.0, same CPU and Wasm fixtures, five groups of 1000 warmed
invocations: i32 median 2.251 µs, v128 median 2.251 µs. Wago remains 11.55x and
13.27x slower in these loops. V8 can optimize repeated invariant reads more
aggressively; these figures are loop results, not isolated instruction latency.
The comparison script is included beside the raw data.

The local combined GC stack took 59.467 seconds on a cold-cache Dewdrop compile
before this fusion and 50.551 seconds after it (one run each). Both outputs have
Wasm SHA-256 `89af055df4b2a4e44be109f10f50d98b5e29deb4810194aca234c9e019912fbb`
and cache SHA-256 `c3a9142e2f4a1186fb5d47f78dfea5adcbf1892e52ff0a803a7c3332e7236240`;
validation passes and `main` returns 42. This external workload includes other
open GC fixes, so these sequential runs are directional evidence, not an isolated
PR benchmark. V8 previously took 3.487 seconds on identical compiler/request
bytes. The full compiler still needs substantial work to reach parity.
