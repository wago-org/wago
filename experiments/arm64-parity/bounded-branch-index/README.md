# Retained bounded arm64 branch-vector index

Apple M4 Max, arm64; isolated source baseline `37174169`. Retain `bounded-branch-index` default ON, with `WAGO_ARM64_NO_BOUNDED_BRANCH_INDEX=1` and per-compilation `Optimizations["bounded-branch-index"] = false` rollback. No workload identity or manifest arguments enter admission.

The direct source proof accepts an i32 constant or a deferred i32 AND with a concrete i32 constant operand. For unsigned 32-bit values, `x & mask <= mask`. Capture that upper bound before ordinary local reconciliation can materialize the expression; omit the executable branch vector's CMP/unsigned-default branch only when the bound is strictly below the label count. Negative/high masks are interpreted as uint32 and decline when not proved in range. Both operand orders are accepted. No range fact is stored or propagated through a local assignment or control join.

Normal index materialization and stack flushing still run, so trapping operands and older trapping values execute before dispatch. The existing small unique zero-argument branch-vector cohort remains the admission boundary. Compact code, duplicate/ineligible tables, calls, EH, custom instructions, tracked GC frames, and argument-carrying targets retain established lowering. The nearby default stub is still emitted, ADR and branch patches use the actual new offsets, and opaque four-byte vector entries retain their fixed stride and existing target ledger. UXTW consumes exactly the low i32 index, including dirty upper host carriers.

## Execution and compilation

Negative deltas mean faster. Paired deltas are medians of pairwise ratios; pooled deltas compare independent medians and are reported separately. Same-binary option runs alternate ON/OFF order on one locked OS thread. The source comparison reverses separate-process order against `/tmp/paired-next-20261011-base`, built from actual pristine `37174169`, selecting the common `loop-memory-base` ON state in both binaries. Thread locking and QoS do not establish physical-core affinity.

| Register-VM run | Pairs × budget per state | OFF/baseline median µs | ON/candidate median µs | Median paired delta | Paired IQR | Pooled delta |
|---|---|---:|---:|---:|---|---:|
| Initial execution screen | 8 × 150 ms | 11.2831 | 10.4995 | -5.48% | -6.21% to -5.14% | -6.95% |
| Execution confirmation | 10 × 300 ms | 11.0167 | 10.3190 | -6.74% | -7.56% to -5.73% | -6.33% |
| Actual pristine-source execution | 4 × 150 ms | 11.1263 | 10.6103 | -4.19% | -5.68% to -2.90% | -4.64% |
| Initial option compilation | 8 × 150 ms | 42.0460 | 42.1013 | -0.14% | -4.83% to +0.15% | +0.13% |
| Actual pristine-source compilation | 4 × 150 ms | 41.9364 | 42.4769 | +1.60% | +1.34% to +1.98% | +1.29% |

Retain the repeatable execution gain with the measured approximately 0.54 µs source compilation cost. The same-binary option toggle does not isolate unconditional source costs; no uniformly flat compilation claim is made. [Screen summary](../bounded-branch-index-screen-summary.md), [confirmation summary](../bounded-branch-index-confirm-summary.md), and [actual-source summary](../bounded-branch-index-cross-summary.md) retain the full comparisons.

| Pristine-source compilation control | Median paired delta | Paired IQR | Pooled delta |
|---|---:|---|---:|
| compiler-register-allocation | -0.43% | -0.54% to +0.13% | +0.25% |
| files-glob-match | -0.86% | -3.36% to +2.05% | -0.54% |
| hardware-boolean-anf | -0.99% | -2.56% to +0.25% | -0.20% |

All three control images are byte-identical between feature states. No control execution gain is claimed. [Source process-image proof](../bounded-branch-index-cross-native-proof.txt) checks candidate and pristine process images against the original screened inventories. Raw observations remain in the [screen execution](../bounded-branch-index-screen-exec.jsonl), [screen compilation](../bounded-branch-index-screen-compile.jsonl), [confirmation execution](../bounded-branch-index-confirm-exec.jsonl), and [source comparison](../bounded-branch-index-cross-build.jsonl) JSONL files. No full-corpus timing sweep was run.

## Qualification and profile

All **46 core + 102 application exact contracts pass in both SIGNAL and explicit bounds modes**. Only `language-register-vm.bin` changes, **1232 → 1224 bytes**; the other **147 images match `37174169` byte-for-byte**. The VM image matches the frozen timed candidate exactly. The application `1x` records are correctness/image capture, not steady timings. [Native proof](../bounded-branch-index-native-proof.txt), [SIGNAL core](../bounded-branch-index-on-signal-core.txt) and [applications](../bounded-branch-index-on-signal-app.txt), [explicit core](../bounded-branch-index-on-explicit-core.txt) and [applications](../bounded-branch-index-on-explicit-app.txt).

Full backend/optimization/encoder/runtime diagnostics and plain focused tests pass. Strict fixtures cover unsigned high masks and values, constant-on-left/right, trapping owned operands, mask-zero traps, older discarded traps, arithmetic owned expressions, direct constants, variable/stored masks, duplicate tables, vector OFF, compact policy, dirty host carriers, and feature ON/OFF under both bounds policies. [Diagnostics](../bounded-branch-index-diagnostics.txt), [plain fixtures](../bounded-branch-index-plain.txt), and the disabled source/test copies here preserve the qualification.

After removing the temporary experiment hook, default ON and `WAGO_ARM64_NO_BOUNDED_BRANCH_INDEX=1` OFF each reproduce the qualified/timed VM inventory and pass its exact oracle. Policy diagnostics include the per-compilation rollback. [Default policy/image proof](../bounded-branch-index-default-native-proof.txt), [default diagnostics](../bounded-branch-index-default-diagnostics.txt), and [source-only retained patch](candidate.patch).

A fresh 3 s prepared SIGNAL Samply capture passes the exact VM oracle and embeds the **1224-byte** qualified/timed image. [Profile identity proof](../bounded-branch-index-profile-native-proof.txt), [capture](../profile-bounded-branch-index-on/), [capture log](../profile-bounded-branch-index-on.txt), and [source-aware assembly](../profile-bounded-branch-index-on-assembly.txt). The removed CMP/B.HS pair is absent. The remaining `MOV X9,X19; AND W9,W9,#7` is a prominent retirement location; sampled leaf observations are not CPU-cycle weights or branch-frequency measurements.

The broader borrowed-logical-immediate candidate was already rejected, including a VM regression; this result does not reopen that candidate. A future dispatch-specific late mask computation could eliminate the owned-index copy/lifetime, but needs a separate source-lifetime and trap-order proof and its own measurements. No such follow-up is implemented here.

Recorded scripts remain at their original paths: [initial screen](../bounded-branch-index-screen.sh), [confirmation](../bounded-branch-index-confirm.sh), [actual-source comparison](../bounded-branch-index-cross.py), [qualification](../bounded-branch-index-qualify.sh), [profile](../bounded-branch-index-profile.sh), and [default policy check](../bounded-branch-index-default-verify.sh). Historical scripts use the temporary experiment hook with frozen binaries; production uses the NO-prefixed rollback. Heavy runs used the shared FIFO `~/benchmark-lock.py --wait --owner counted-next` reservation. Main production import belongs to the root agent; this directory records the source-only retained candidate and evidence.
