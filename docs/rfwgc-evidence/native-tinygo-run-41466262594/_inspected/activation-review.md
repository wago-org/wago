# Darwin TinyGo activation readiness recheck

Reviewed harness `041f7ed4c376e2a33ce21b22075cd617ddca6597`, fixture implementation `4913c91e1ad5636543d0ea7eed740b16de1b9bf2`, evidence `375e47bb81ee2969615cf48ec31b94eb1edf8daf`, and TinyGo execution source `7e41fafbbcd737ed1edce11b94b43b4eda134dcf` plus the declared test overlay. This is a bounded activation review only. It does not approve the port and does not claim native Darwin coverage.

## Findings

No preactivation defect was identified in the reviewed scope.

The retained checksum manifests validate. The cross receipts contain nine distinct raw commands: three packages in ordinary, `wagodebug`, and `wago_regalloccheck`, each with exit 0 and a matching empty-log hash. Their result census requires the expected top-level tests and children, including both `TestDarwinTinyGoCancellationAndGC/cancel` and `/close`, in every mode. The missing, duplicate, skip, failure, unexpected, vacuous, and wrong-platform controls are recorded as rejected. All evidence-owned command paths are finished; no process using the recorded cross or Linux temporary roots remains.

The cross source manifest matches Git source `7e41fafbbcd737ed1edce11b94b43b4eda134dcf` except for exactly the five declared overlay effects: the canonical resume oracle, the two new supplement files, and the two architecture/build-constraint adapters. The Linux manifest's 6,151 exported files all match Git commit `4913c91e1ad5636543d0ea7eed740b16de1b9bf2`. The installed patched TinyGo compiler and its four recorded foreign-stack runtime files match the evidence hashes. The first incomplete Linux export and its internal compile failure remain preserved separately; the corrected run manifest is the credited execution.

The repaired resume test first requires `nativeResumeStackTop()` to equal the physical prepared-stack top. It then substitutes `preparedInt.stackTop ^ uintptr(16)` for the entry field, repeats the physical-top requirement, and restores the entry before deferred `Close`. The Linux TinyGo 0.42 execution passes the canonical test in all three modes. An exact wrong-field mutation that returns `e.stackTop` fails in all three modes. This Linux negative proof is sufficient for the limited discrimination claim: it demonstrates that the platform-independent controlled-sentinel oracle rejects the specified wrong-field implementation. It is not native Darwin execution evidence. The planned Darwin run needs no separately built negative variant because its positive canonical test contains that controlled wrong-field challenge itself; the Linux negative does not substitute for the planned native positive execution or any other native suite coverage.

The cancellation adapter still has required `cancel` and `close` children. Both establish import entry, perform eight collections, have bounded unwind waits, perform post-unwind and final collections, and retain deferred cancellation and instance close for early failures. Cancel requires `context.Canceled`; close requires the interrupted trap. The ARM adapter removes exactly two `dead-gc-new=false` calls that are registered only for AMD64 at the pinned source, while preserving relocation/movement, refresh-generation, result-42, trap, and configuration controls. The harness checks source, tool, and driver immutability after execution.

The Darwin TinyGo workflow job is unchanged from `880125` through `041f7ed4c`: native Darwin ARM64/Rosetta refusal, exact source and harness identities, pinned archive, three serialized modes, per-command bounds, isolation from the other qualification inputs, failed-attempt retention, and the total 333 job-minute bound remain intact. The workflow delta in this range only repins the separate native ARM qualification from `5847` to `08f511fac`; it does not change the Darwin TinyGo job.

## Verdict

Activation: **YES** — safe to launch the pinned native Darwin TinyGo experiment. Native execution is evidence the experiment is intended to obtain, not a prerequisite for launching it. The prior activation blockers (unfinished 9/9 cross validation and absent patched execution discrimination) are now closed by the retained cross and Linux receipts, and no new preactivation defect was found.

Native qualification: **NOT RUN** in this recheck; the previous native run remains **FAIL**. Only a fresh native Darwin ARM64 run can qualify the experiment. This verdict neither converts prior failures nor weakens the native suite, census, identity, cleanup, or all-three-mode requirements.
