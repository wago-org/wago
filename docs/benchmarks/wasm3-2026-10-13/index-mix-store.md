# Checked index-mixer initialization loops

Retained source `f8cbc31b` vectorizes a general paired i32 store loop whose
values mix a running index with XOR, unsigned shifts, wrapping multiply, and
mask. This improves application input generation included in wasm.fyi timing;
it does not substitute a synthetic FIR-only kernel or change the measured API.
The source proof checks high-first paired stores, local aliasing, loop bound,
and ordinary unshared memory32. ARM64 checks the complete range before using
four-lane SIMD; unsafe, odd, zero, or wrapping ranges execute the exact source
loop, preserving traps and partial writes. Requires four available GP and six
caller-saved vector registers and excludes calls, interruption, EH and GC roots.

Compared retained `6b46132b` with `f8cbc31b` and SIMD w2c2 Clang `f303ed27`
on Apple M4 Max under the shared device lock. All 17 application workloads
selected by this optimization and an unchanged video-dct control were included.
Two runs reverse Wago capture order, each containing all 54 engine/workload
captures, four phases, three launches × five samples, RSS and code measurements.
All 108 captures completed without errors or resource failures. The complete
raw results, exact source and contract identities, manifests, and compiler-cost
measurements are preserved in [index-mix-store.json](index-mix-store.json).

| Workload | First change | Reversed change | First / reversed time relative to peer |
| --- | ---: | ---: | ---: |
| audio-autocorrelation | -14.90% | -15.47% | 0.676× / 0.665× |
| audio-fir | -7.09% | -17.46% | 1.163× / 1.039× |
| game-cellular-automata | +2.75% | +1.69% | 1.859× / 1.486× |
| image-blur | -8.64% | -8.67% | 1.355× / 1.341× |
| image-composite | -16.51% | -14.50% | 1.281× / 1.293× |
| image-median | -2.33% | +1.58% | 0.457× / 0.572× |
| image-resize | -45.75% | -45.81% | 0.740× / 0.730× |
| ml-convolution | -11.52% | -25.06% | 1.490× / 1.280× |
| ml-inference | -6.82% | -6.90% | 1.281× / 1.246× |
| ml-max-pooling | -46.07% | -45.44% | 0.733× / 0.743× |
| stats-bootstrap | +11.12% | -0.93% | 1.762× / 1.552× |
| stats-quickselect | -13.66% | -16.39% | 0.919× / 0.898× |
| video-dct | -0.90% | -1.64% | 2.145× / 2.150× |
| video-motion | -0.39% | -4.19% | 1.073× / 1.189× |
| video-temporal-denoise | -5.99% | -7.50% | 1.276× / 1.471× |
| vision-dilation | +4.95% | -9.99% | 1.334× / 1.318× |
| vision-fast-corners | +0.21% | -0.48% | 1.231× / 1.360× |
| vision-sobel | -18.53% | -17.19% | 1.227× / 1.247× |

Equal-workload steady-time geomean current/before was 0.8852 and 0.8570.
Resize and max-pooling improved about 45–46% in both runs. FIR improved 7.1%
and 17.5%, but remains above peer time. Cellular automata was 2.75% and 1.69%
slower; that small repeated regression is retained in this decision. Bootstrap
(+11.1% then −0.9%) and dilation (+5.0% then −10.0%) varied across runs, so their
first-run slowdowns do not establish a stable regression. Native images grow
because the safe scalar fallback remains; FIR grows from 5188 to 5540 bytes.
These targeted gains do not establish corpus-wide or each-feature parity.

Three alternating compiler-cost pairs covered 218 Core 3 modules and 27
thread modules, 100 passes each, GOMAXPROCS=1. Median compiler time increased
1.21% / 2.18%; emitted code and allocation totals were unchanged apart from
measurement noise. Median process peak RSS changed +0.69% / −2.35%.

Validation: 394 tagged codegen/register-allocation checks passed without skips;
2051 ordinary public execution checks passed without skips. Core 3 and official
atomic conformance passed (3.513 s). Native x86_64 Remote public index-mixer
execution passed, exercising the scalar fallback. The new SIMD lowering is
ARM64-only; no AMD64 performance claim is made. Rejected earlier sliding-sample
and pinned-counter experiments are excluded. Candidate captures remain private
until a complete newer feature snapshot is ready for local publication.
