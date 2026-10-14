# Native AMD64 invariant-global sum

Source `bc9d71f1` extends the integer-sum proof to call-free functions reading a defined mutable global when no value register was allocated to that global. Imported globals, writes, recurrence, fences, and interruptible functions retain their existing guards. The reduction reads the live global cell; it does not substitute its initializer. Public API tests update exported globals between calls and cover i32/i64, register ABI choices, unsigned extrema, and artifact round trips.

Hub's Ryzen 7 7800X3D, Linux AMD64, CPU 7, Go 1.27.1 and SIMD w2c2-clang `f303ed27` / Clang 18.1.3 match the previous native audit. In fresh and reversed SDK source orders, exported mutable-global reads improved from 70.7–71.2 µs to 190 ns. The peer takes 73.5–73.7 ns: Wago remains approximately 2.58 times slower on this contract. Public API alternating measurements independently confirmed the gain with zero allocations. Generated code grew from 287 to 319 bytes.

The full audit covers 798 captures and 3,192 phase rows across baseline `d4a5d2ff`, candidate and peer. Another 72 focused captures and 48 outlier-repeat captures passed exact source/version, artifact, contract, platform, sample-count and resource checks. Wago's functional outcomes are unchanged; five native JSPI profile failures remain nonnumeric. No browser counterpart was added.

| Candidate / baseline | Main | Features | All |
|---|---:|---:|---:|
| Positive steady pairs | 148 | 107 | 255 |
| Steady geometric ratio | 1.00002 | 0.94278 | 0.97559 |
| Compile geometric ratio | 1.00189 | 1.00480 | 1.00311 |
| Compile RSS geometric ratio | 1.00044 | 1.00000 | 1.00026 |

The exact successful peer cohort contains 202 steady pairs. Candidate/peer is 1.33695 overall and 1.65148 across its 54 feature pairs; the main cohort is 1.23776. These peer cohorts differ from the 255 Wago before/current pairs.

Actual SDK adapter code-image exports cover all 255 positive compile workloads. Only the intended exported-global workload changed; 254 control images are byte-identical. Twelve workloads were repeated in both source orders. The target gain repeats, ordinary threads remain within 0.6%, and compact imports within 0.7%. Several full-pass outliers reverse direction. Bitmap index (1.010/1.215) and utf8proc (1.127/1.007) remain recorded rather than dismissed. Audio improves against this immediate baseline (0.983/0.957); the earlier native-leaf audio regression against its original baseline remains unresolved.

The adjacent JSON records the focused, full, repeated, and image evidence. Retain this improvement with those limitations. Queue only its complete candidate pass for the next local refresh; the pinned peer is already present. Apple results remain a separate source/platform. No production deployment or push is performed. Overall parity remains incomplete.
