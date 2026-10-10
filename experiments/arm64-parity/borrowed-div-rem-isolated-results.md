# Borrowed division/remainder: retained concrete-source cover

Retained default on in the main compiler after final verification. The following sections preserve the isolated experiment and mitigation history. See the final retained verification below. Source: `/tmp/wago-borrowed-div-rem-isolated-20261010`. Uses current default-on common-exit and dominated-indexed-base compiler; leaf-scoped constant experiment is explicitly disabled. Production division lowering remains unchanged while its separate diagnostics/oracle/impact/timing pipeline waits for the shared reservation.

Option `borrowed-div-rem` / `WAGO_ARM64_EXPERIMENT_BORROWED_DIV_REM=1`. Integer division/remainder reads pinned local/global operands in place, pins operands during allocation, restores inherited pins and releases only owned registers. Existing zero/overflow traps, signed remainder treatment, constant divisor lowering and output allocation rules are preserved. Native fixtures exercise sixteen live/reused parameter values to check source preservation, same-source aliases, every operator and width, division-zero and signed-overflow boundaries, both bounds modes and both switch states. Strict diagnostics check actual borrowing admission.

Queued pipeline stops on diagnostics failure; otherwise it checks all148 signal exact oracles, inventories changed native images against the retained baseline, then times up to six actually changed application images plus a register-allocation control and a core modular-arithmetic case. The application subset is selected from the native-impact inventory, with gcd first if it changes; the selected names are saved alongside results. Unchanged images are controls. No benefit or correctness claim until the jobs complete. Full explicit-bounds corpus qualification remains required if the candidate earns retention.

Static inspection of the retained gcd native image confirms two division-operand copies at0xe8 and0xec before UDIV0xf8; the earlier snapshot at0xe0 is required because a local.tee overwrites the old dividend's local. Added a separate operand-local.tee fixture that uses both updated locals after division and verifies the old dividend survives. This is source/assembly evidence only, not a new sampled timing claim.

Setup completed with local `codegen`/`profile` sources and a read-only corpus-fixture link. The first substantive diagnostic run exposed a result-destination alias bug in the worker-reset test: returning64/x with borrowedx in the mandated result register must still write that exact destination. The candidate now copies an operand into owned storage when it aliases the explicit destination, preserving the caller contract. Added direct-return fixtures for variable/constant numerators, both widths/operators/bounds/switch states. Diagnostic retry is queued; production division lowering remains unchanged.

## Initial qualification and focused timing passed

Full backend/catalog diagnostics pass after the explicit destination alias fix, including fresh/shared worker reuse and dedicated source/alias/trap/pressure fixtures. All46core+102application signal exact oracles pass. Twenty application and four core native images change; affected-image inventory and focused selection are saved in sibling files. Main production division remains unchanged.

| Workload | Execution change | Compilation change |
|---|---:|---:|
| Euclidean gcd | -3.64% | -0.16% |
| ADPCM | +0.38% | -0.54% |
| Dead-code kernel | +0.05% | -0.53% |
| B-tree | +0.80% | +0.23% |
| Cellular automata | -0.90% | -0.14% |
| Collision impulse | +0.93% | -0.24% |
| Register allocation (unchanged image) | -0.09% | -0.20% |
| Core modular arithmetic | +0.35% | not measured |

Eight rounds of200ms, paired on the same OS thread. Compiler costs are roughly flat; small execution changes require confirmation. Queued:12rounds300ms confirming gcd/B-tree/collision, six additional actually affected applications including K-means (native2936→2808bytes), full explicit-bounds oracle checks and ordinary backend tests. Candidate is not retained yet.

## Confirmation and expanded affected subset

Initial candidate passes all148explicit-bound exact oracles and ordinary backend tests, completing both-bounds qualification. Longer12round300ms execution confirmation: gcd -12.68%, B-tree +0.93%, collision impulse -0.11%. Gcd gains repeat but their magnitude varies (initial -3.64%); no physical-core pinning claim.

Six further changed applications (8round200ms): K-means -0.84%, register VM +1.98%, optical flow -0.50%, wave equation -0.18%, ray-box -0.42%, bootstrap +0.94% execution. Their compilation changes range -1.29% to+0.59%, roughly flat. K-means' substantial code shrink does not establish a substantial execution gain.

Before retention, test general concrete-source admission: both operands must be concrete values, and at least one must be a pinned local/global read. Deferred evaluation keeps the established destination-directed lowering and allocation order; owned-only reuse is excluded from this cover. This is an operation/storage rule, not workload recognition. Mitigated diagnostics,148signaloracles/native-impact inventory, five-case execution/compilation timing and core compilation are queued under `borrowed-div-rem-concrete-screen.sh`. Main production division remains unchanged. Preserve the original qualified candidate and frozen paired binary for comparison.

## Concrete-source mitigation passed

Diagnostics (including deferred-operand rejection), all148signal exact oracles and a3second gcd Samply capture pass. Scope narrows from20 to11applications; four core images remain affected. B-tree and bootstrap return to baseline native code. Gcd remains byte-identical to the first candidate. Focused medians: gcd -17.39%, register VM +1.72%, K-means +0.61%; unchanged B-tree -0.41% and bootstrap +0.12% are timing controls, not feature gains. Selected application compilation -0.82–+0.09%; modular arithmetic compilation -0.91% (small/noisy). Saved source-aware profile shows UDIV0xf0 and the necessary snapshot/local update, with the two old operand copies absent. Leaf observations are not CPU-time weights.

## Owned-dividend result mitigation passed initial checks

A second general variant reuses an owned dividend as result when no explicit destination is supplied; borrowed locals/globals remain read-only. SDIV/UDIV/MSUB permit this alias, and signed remainder's -1 path writes the reused result only on that path. Existing source/alias/trap/pressure/worker tests and all148signal oracles pass. A short exact-contract profile also completes.

Focused medians: gcd -3.41%, register VM +0.86%, K-means -0.24%, unchanged B-tree +0.08%, unchanged bootstrap +0.02%. Compilation -0.31–+0.25% for applications; core modular arithmetic +0.01%, approximately flat. Gcd gain is smaller than the concrete variant's last run, so do not select based on unrelated sample windows. Queued same-reservation ordering: concrete, owned, owned, concrete;12rounds300ms each on gcd and VM. Frozen paired binaries preserve each implementation. Main production division remains unchanged pending this comparison and final selected-variant qualification.

## Controlled variant selection and production integration

Same-reservation concrete/owned/owned/concrete comparison (12round300ms each): concrete gcd -17.50% and -5.68%, VM +0.62% and +0.72%; owned-result gcd -14.93% and -8.61%, VM +1.46% and +1.75%. Magnitudes vary, but all four gcd pairs improve; controlled VM evidence favors concrete. Reject owned-result mitigation and choose concrete-source borrowing with the established fresh result allocation.

Both selected variants pass all148 explicit-bound exact oracles and ordinary backend tests. The concrete variant is integrated into the main worktree with default-on `borrowed-div-rem`, disabled by its per-compilation override or `WAGO_ARM64_NO_BORROWED_DIV_REM=1` / `WAGO_ARM64_EXPERIMENT_BORROWED_DIV_REM=0`. Eligibility remains general operation/storage rules, with no workload recognition. Main final default-on diagnostics, both-bounds oracles and all148 native-image identity to the measured concrete candidate are queued. Retention verification is pending.

Tradeoff is explicit: substantial repeated gcd gain, roughly flat compilation, and a small measured VM execution loss. This does not establish universal parity; VM remains a target for further general optimization.

## Retained; final verification passed

Default-on main compiler diagnostics, catalog, runtime and encoder checks pass; all46core+102application exact oracles pass in BOTH signal and explicit bounds modes. All148default-on signal native images match the measured concrete candidate byte-for-byte. Ordinary backend tests pass. Frozen retained binaries: `/tmp/parity-borrowed-div-rem-retained.test` and `/tmp/parity-borrowed-div-rem-retained-explicit.test`; native baseline: `/tmp/borrowed-div-rem-retained-core` and `/tmp/borrowed-div-rem-retained-app`. These are the baseline for future experiments.

Selected concrete cover is retained; broader deferred/owned-only admission and owned-dividend output reuse are not integrated. Large repeated gcd benefit versus modest VM loss and flat compilation supports this tradeoff. Full parity remains incomplete.
