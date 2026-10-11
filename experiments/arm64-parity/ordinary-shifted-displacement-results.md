# Shifted immediate displacements on Apple M4 Max

Retain the narrow ARM64 displacement lowering after rejecting the broader ordinary shifted-immediate prototype. The retained path uses the existing exact shifted ADD/SUB encoding in addDisp for read-only displacement arithmetic. Ordinary value arithmetic and comparisons keep their established admission. Source admission uses operand shape and encodable displacement only; no corpus name, artifact hash or manifest input enters the compiler.

## Prototype and mitigation

The full prototype extended shifted-immediate admission throughout ordinary arithmetic and comparisons. Six alternating150ms pairs found ADPCM execution36.428 to38.430 microseconds, **+5.45%** (IQR +5.00..+6.16%), with compilation +1.71%. Its glob/components gains did not justify that regression. Reject the broad admission and narrow the change to addDisp.

The narrowed candidate restores ADPCM, FIR and BK-tree native images byte-for-byte to the qualified pre-prototype baseline. This is an actual image comparison, rather than treating a small timing difference as recovery. The existing CompactNative path remains available. See [initial observations](ordinary-shifted-immediate-focused-summary.md) and [native change inventory](ordinary-shifted-displacement-native-changes.txt).

## Correctness and native scope

Backend, encoder, catalog and runtime checks pass with code-generation statistics and register-allocation diagnostics. Native fixtures cover i32/i64 positive and negative constants, shifted encoding limits and a nonencodable4097 control, add/subtract/equality/signed comparisons, overflow/high-bit inputs, per-compilation on/off and both explicit/signal bounds modes. These are correctness fixtures; broad generic operations intentionally need not enter the narrow cover.

The narrowed signal build passes all **148 exact contracts**:102 applications and46 cached core contracts, including vector, memory, initialization and host profiles. Actual native differences from the qualified indexed-key baseline affect **29 applications and8 core contracts**. The remaining111 images are unchanged. No full timed corpus sweep was run. Final combined default qualification covers the other bounds mode separately.

## Repeated changed-image timings

Ten alternating200ms pairs confirm the useful application gains. Negative deltas mean faster; compilation includes release, execution uses the unchanged declared input and exact result checks.

| Workload | Execution paired delta | Paired execution IQR | Compilation paired delta |
|---|---:|---:|---:|
| Triangle raster | -8.37% | -8.84..-7.45% | -0.56% |
| Edit distance | -5.97% | -6.34..-5.85% | -0.23% |
| Optical flow | -4.37% | -4.54..-3.28% | -0.40% |
| Wave equation | -3.91% | -4.10..-3.33% | -0.52% |
| Glob matching | -2.99% | -3.37..-2.16% | -0.67% |
| Connected components | -1.85% | -2.61..-0.42% | +0.18% |
| LZW | +0.19% | -0.02..+0.29% | -0.24% |

The earlier four-pair screen covers all29 changed applications. LZW's initial +0.94% shrinks to +0.19% on confirmation; compilation confirmation remains essentially flat. Components has +1.25% pooled compilation versus +0.18% median paired delta, so the raw interleaved observations remain necessary. See [confirmation summary](ordinary-shifted-displacement-confirm-summary.md), [execution JSONL](ordinary-shifted-displacement-confirm-exec.jsonl), [compilation JSONL](ordinary-shifted-displacement-confirm-compile.jsonl), and [all changed-application screen](ordinary-shifted-displacement-focused-summary.md).

## Core screen and compilation limits

Every changed core contract enters a small execution screen: two reversed100ms pairs, separate benchmark processes, using the original exact contracts and reset behavior. Median paired deltas are PCRE2 +0.37%, LZ4 +0.12%, dr_wav +0.43%, utf8proc -0.21%, SHA-256 +0.70%, NanoSVG +0.30%, zlib +0.24%, and Monocypher -0.28%. These observations show no severe loss, but two pairs cannot exclude smaller execution regressions; process startup and sampling differ from the same-thread application harness. No core speedup is claimed. See [raw core screen](ordinary-shifted-displacement-core-screen.jsonl) and [screen driver](ordinary-shifted-displacement-core-screen.py).

Initial **full-prototype** core compilation was essentially flat in six150ms pairs: PCRE2 +0.15% and LibTomMath modular arithmetic +0.21%. These are not compilation measurements of the narrowed candidate; LibTomMath's native image is restored under the mitigation and is outside its eight changed-core scope.

A subsequent screen fills the narrow compilation gap using the same frozen narrowed test binary and eight full contract IDs, four reversed100ms process pairs per workload:

| Core contract | Compilation paired delta | Paired IQR |
|---|---:|---:|
| PCRE2 | +1.00% | +0.23..+1.75% |
| LZ4 | +0.19% | +0.11..+0.41% |
| dr_wav | -0.85% | -1.25..-0.24% |
| utf8proc | -0.11% | -1.01..+0.73% |
| SHA-256 | +0.15% | -0.16..+0.20% |
| NanoSVG | -0.99% | -2.12..-0.23% |
| zlib | -0.02% | -0.91..+0.64% |
| Monocypher | -0.29% | -0.55..+0.05% |

PCRE2 compilation9218.572 to9317.556 microseconds is the modest measured cost; the other seven cases show no large compilation increase. B/op is broadly unchanged; median allocation counts are mostly unchanged (NanoSVG621 to622, PCRE2 1336 to1336.5). These are short separate-process observations with GC/scheduling variability, not proof that every smaller effect is absent. This phase times compilation and release without guest execution; correctness of these original artifacts is established by the148-contract signal qualification and separate execution checks. See [narrow compilation summary](ordinary-shifted-displacement-core-compile-summary.md), [raw JSONL](ordinary-shifted-displacement-core-compile.jsonl), and [driver](ordinary-shifted-displacement-core-compile.py). Each process output is also retained.

Benchmarks lock an OS thread/request interactive QoS where supported; neither that nor separate-process reversal establishes physical-core affinity. All heavy jobs use the shared benchmark lock. Default-on retention is followed by the parent run's final combined validation.
