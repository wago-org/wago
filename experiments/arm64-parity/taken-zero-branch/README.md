# Retained arm64 taken zero-branch threading

Retain `taken-zero-branch` on Apple M4 Max, arm64 only, against source baseline `37174169`. Boolean ANF execution improves about 11% in both repeated option pairs and an actual pristine-compiler comparison. Compilation has small measured costs on several controls, recorded below. Admission uses native instruction semantics; no corpus name, manifest input, or benchmark identity enters the compiler.

The production policy is default ON. `WAGO_ARM64_NO_TAKEN_ZERO_BRANCH=1` disables it globally, and `Optimizations["taken-zero-branch"] = false` preserves the per-compilation rollback. The temporary `WAGO_ARM64_EXPERIMENT_TAKEN_ZERO_BRANCH` hook in archived scripts describes how the candidate was measured; it is removed from production.

## Semantic proof and bounded implementation

The taken edge of a direct `CBZ` or `CBNZ` establishes whether its tested register is zero at the direct destination. If that destination begins with another `CBZ`/`CBNZ` of the **same register and same W/X width**, its outcome is already known:

| First/second polarity | New destination |
|---|---|
| Same | Second branch's taken target |
| Inverse | Instruction after the second branch |

Only the first branch's displacement changes. Its untaken path and the second test remain intact for other entrants. No intervening effects are executed or speculated, no register survival through a skipped body is assumed, and neither zero-test changes NZCV. The new destination is added to the shared branch-target map before later word-replacing passes run.

Opaque fragments, exception-handling modules, and custom instructions are excluded. Destinations must stay inside the emitted function, and the replacement displacement must fit signed imm19 (in instruction units). Self edges and the immediate two-branch cycle are rejected. There is no recursive target chasing, so chains cannot cause an unbounded compiler traversal. Native fixtures cover both widths, both polarities, dirty upper halves of W carriers, unchanged alternative paths, target-map updates, mismatch rejection, cycles, and range overflow. The [initial source](../taken-zero-branch-initial-taken_zero_branch.go.disabled), [native fixtures](../taken-zero-branch-initial-taken_zero_branch_arm64_test.go.disabled), and [initial integration patch](../taken-zero-branch-initial.patch) preserve the original prototype.

The final mitigation adds a cheap candidate preflight to the existing branch-target inventory loop. Functions without a direct same-register, same-width zero-test pair skip the separate threading scan. This changes compiler work, not admitted rewrites: [all 148 final ON images match the original qualified ON images](../taken-zero-branch-mitigated-native-proof.txt).

## Correctness and native scope

The [native proof](../taken-zero-branch-native-proof.txt) inspects 46 core and 102 application images. Only `hardware-boolean-anf.bin` changes: its size stays **1904 bytes**, and exactly **seven CBZ displacements** are retargeted. The other **147 images are byte-identical**. Every disabled image matches the pristine `37174169` inventory.

| Recorded qualification | Core contracts | Application contracts | Result |
|---|---:|---:|---|
| OFF, SIGNAL bounds | 46 | 102 | Exact oracles pass; pristine native images |
| ON, SIGNAL bounds | 46 | 102 | Exact oracles pass; seven ANF retargets only |
| ON, explicit bounds | 46 | 102 | Exact oracles pass |
| Mitigated ON, SIGNAL bounds | 46 | 102 | Exact oracles pass; all 148 ON images unchanged |

The explicit-bounds qualification applies to the original emitter; the preflight preserves all qualified ON images. Backend diagnostics with `wago_codegenstats,wago_regalloccheck,wago_guardpage`, optimization catalog tests, and arm64 encoder tests also pass. Evidence is in the [diagnostics log](../taken-zero-branch-mitigated-diagnostics.txt), [qualification script](../taken-zero-branch-qualify.sh), [OFF SIGNAL core](../taken-zero-branch-off-signal-core.txt) and [application](../taken-zero-branch-off-signal-app.txt) logs, [ON SIGNAL core](../taken-zero-branch-on-signal-core.txt) and [application](../taken-zero-branch-on-signal-app.txt) logs, and [ON explicit core](../taken-zero-branch-on-explicit-core.txt) and [application](../taken-zero-branch-on-explicit-app.txt) logs. The [mitigated core](../taken-zero-branch-mitigated-core.txt) and [application](../taken-zero-branch-mitigated-app.txt) logs record the final preflight qualification.

An initial qualification used the wrong application benchmark selector and produced no application inventory. The inventory assertion failed with `('app', 0)`; see the [original failed run](../taken-zero-branch-qualify-initial-wrong-selector.txt). This was a selector failure, not an application correctness failure. No usable application timings or qualification are claimed from that attempt. The corrected `^BenchmarkParity/.*/Exec$` selector runs each application once (`-test.benchtime=1x`), validates its exact oracle, and supplies the 102-image inventory. Those one-execution records establish correctness, not steady execution performance.

## Execution evidence

Negative deltas mean faster. “Paired” is the median of the per-pair ratios; “pooled” is the ratio of independently pooled medians. They are distinct estimates. Option pairs alternate ON/OFF order in one locked OS thread. Source comparisons reverse separate-process baseline/candidate order and select the common `loop-memory-base` ON state in both binaries. QoS and an OS-thread lock do not establish physical-core affinity.

| Boolean ANF run | Pairs × budget per state | OFF median µs | ON median µs | Median paired delta | Paired IQR | Pooled delta |
|---|---|---:|---:|---:|---|---:|
| Initial option screen | 8 × 200 ms | 75.9389 | 67.9869 | -9.98% | -12.03% to -9.48% | -10.47% |
| Option confirmation | 10 × 300 ms | 74.1124 | 65.8571 | -11.15% | -11.56% to -10.12% | -11.14% |
| Actual pristine-source comparison | 4 × 150 ms | 73.0690 | 65.1079 | -10.94% | -11.33% to -10.44% | -10.90% |

The [screen summary](../taken-zero-branch-screen-summary.md), [screen execution JSONL](../taken-zero-branch-screen-exec.jsonl), [confirmation JSONL](../taken-zero-branch-confirm-exec.jsonl), and [source comparison summary](../taken-zero-branch-cross-summary.md) retain these results. [Source-comparison raw JSONL](../taken-zero-branch-cross-build.jsonl) and [native proof](../taken-zero-branch-cross-native-proof.txt) verify both process images against the qualified inventories. No execution benefit is claimed for unchanged control images, and no full timing sweep was run.

## Final compilation tradeoff

The mitigated candidate's same-binary option comparison is **8 pairs × 150 ms** per state. These costs include compilation and are independent of the execution table above.

| Workload | OFF median µs | ON median µs | Median paired delta | Paired IQR | Pooled delta |
|---|---:|---:|---:|---|---:|
| hardware-boolean-anf | 66.4065 | 66.6010 | +0.08% | -0.14% to +1.04% | +0.29% |
| audio-adpcm | 23.8530 | 23.9622 | +0.03% | -0.67% to +0.62% | +0.46% |
| compiler-register-allocation | 67.6089 | 67.4301 | -0.05% | -0.42% to +0.27% | -0.26% |
| files-glob-match | 26.7526 | 26.8227 | +0.31% | -0.30% to +0.56% | +0.26% |
| fastfloat / decimal-parse | 1187.4111 | 1196.7769 | +0.70% | -0.67% to +1.49% | +0.79% |
| kissfft / complex-roundtrip | 893.6364 | 903.4967 | +1.36% | +0.49% to +2.27% | +1.10% |
| libtommath / modular-arithmetic | 2243.4610 | 2268.4990 | +1.05% | -0.59% to +1.83% | +1.12% |
| PCRE2 / compile-match | 9046.9766 | 9033.5482 | -0.17% | -0.47% to +0.43% | -0.15% |

Source: [final option summary](../taken-zero-branch-mitigated-compile-summary.md), [application raw JSONL](../taken-zero-branch-mitigated-app-compile.jsonl), and [core raw JSONL](../taken-zero-branch-mitigated-core-compile.jsonl).

The actual source comparison is **6 pairs × 200 ms** and includes the candidate's unconditional source cost, which an option toggle within one binary cannot isolate. The baseline is `/tmp/paired-next-20261011-base`, built from pristine `37174169`; the candidate is `/tmp/paired-taken-zero-branch-mitigated`.

| Workload | Baseline median µs | Candidate median µs | Median paired delta | Paired IQR | Pooled delta |
|---|---:|---:|---:|---|---:|
| hardware-boolean-anf | 64.7009 | 65.1384 | +1.46% | -2.73% to +2.74% | +0.68% |
| audio-adpcm | 23.1684 | 23.0567 | +0.86% | -1.09% to +4.20% | -0.48% |
| compiler-register-allocation | 65.4430 | 66.5268 | +1.43% | +0.57% to +2.27% | +1.66% |
| files-glob-match | 25.7581 | 26.8211 | +2.47% | +0.25% to +5.13% | +4.13% |

Source: [final source-comparison summary](../taken-zero-branch-mitigated-cross-summary.md), [raw JSONL](../taken-zero-branch-mitigated-cross-build.jsonl), and [matching process-image proof](../taken-zero-branch-mitigated-cross-native-proof.txt). These results support a compilation tradeoff, not a claim that compilation is uniformly flat.

The original 4-pair core compilation screen remains intact in [raw JSONL](../taken-zero-branch-core-compile.jsonl). Fastfloat ON observations include 3811.13 and 1944.04 µs against OFF observations around 1150–1207 µs; its original median paired delta was **+31.89%**, IQR **-0.33% to +105.10%**, and pooled delta **+32.37%**. The final +0.70% repeat above does not erase those outliers or identify their cause. The [initial application compile records](../taken-zero-branch-screen-compile.jsonl) and [control records](../taken-zero-branch-controls-compile.jsonl) are also preserved.

## Profile and reproduction records

The [prepared SIGNAL capture](../profile-taken-zero-branch-on/) passes the exact Boolean ANF oracle. Its embedded **1904-byte** native image matches the qualified and timed candidate byte-for-byte; see the [profile proof](../taken-zero-branch-profile-native-proof.txt), [capture log](../profile-taken-zero-branch-on.txt), and [source-aware sampled assembly](../profile-taken-zero-branch-on-assembly.txt). The assembly records leaf observations, not CPU-cycle weights or instruction-cost attribution. It does not establish that seven branches account for a measured percentage of cycles.

Recorded scripts remain at their original paths: [screen](../taken-zero-branch-screen.sh), [qualification](../taken-zero-branch-qualify.sh), [preflight qualification and compile repeat](../taken-zero-branch-mitigated.sh), [pristine-source comparison](../taken-zero-branch-cross.py), and [profile capture](../taken-zero-branch-profile.sh). They reference the isolated source checkout `/tmp/wago-taken-branch-37174169` and frozen `/tmp` binaries/inventories from the run. They are historical reproduction records; the temporary experiment environment hook is not the retained production policy. Device-intensive commands used the shared FIFO `~/benchmark-lock.py --wait` reservation.

The retention decision accepts the measured compile costs in exchange for the repeatable approximately 11% ANF execution gain, exact both-bounds qualification, tightly bounded semantic proof, and unchanged native images outside that one workload. It does not refresh the historical w2c2 ranking or claim a gain across all corpora.
