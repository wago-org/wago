# Rejected dead initial integer-local zeros

Rejected after bulk-memory coverage, one-pass analysis, worker-scratch storage and immediate cold-helper mitigations. The modest execution benefit did not justify the final measured compiler cost. No production import occurred; isolated compiler source was restored to exact37174169. The qualified candidate survives as `source.patch`, separate `.go.txt` files, frozen binaries and evidence. Reproduction requires applying that patch to an isolated37174169 checkout before running the archived scripts; the scripts retain their original temporary paths.

The experiment was default OFF throughout. Baseline 37174169; Apple M4 Max arm64; signal-mode timing. Admission uses function structure and instructions only, with no corpus or manifest identity in compiler decisions.

The current LibTomMath profile showed eager STP XZR pairs at function 19 entry. The hypothesis was that the initial values were overwritten before any reachable read. A bounded one-pass structured proof now records values read before definition. Forward exits and if joins intersect the written masks of reachable predecessors. Assignments cannot make a local unwritten, so the first loop-entry mask conservatively covers backedges without a fixed point. Calls preserve private caller locals; their explicit argument reads are still recorded. Classified bulk memory operations have no private-local side effects. EH, GC/reference instructions, unknown/custom operations and malformed or oversized bodies fail closed.

Eligibility is call-making, integer signature and locals, at most 64 total locals, at least eight declared locals, at most 4096 body bytes, no planned inlining, no lazy-zero path, no EH/GC/custom scope. Candidates already eliminated by the independent entry-prefix proof are excluded. Only initial zeroing changes; branch lowering, homes, later assignments and register ownership retain their existing behavior.

The final cost mitigation computes the proof and emits zeros immediately inside a cold entry helper. Its mask is local and consumed immediately: there is no retained function/worker field, hint mutation, or reset cost. `compileFuncAttempt` and both metadata layouts are restored exactly to the baseline source. The only `compile.go` change is a three-line option/helper branch at `zeroDeclaredLocals` entry; the OFF/unaffected eager-zero loop remains the original implementation. `traceFuncIdx` is initialized unconditionally, including ordinary non-profile builds, to imported function count plus local function index. The helper subtracts that import count to access the same stripped `Code[idx].BodyBytes` previously scanned at compile-attempt setup. It runs at the real internal or wrapper-ABI function entry, before body/inline tracing can change the index; separately generated register-ABI adapter stubs do not zero declared locals.

## Compiler-cost mitigations

| Prototype | LibTomMath execution | Compiler latency | Evidence |
|---|---:|---:|---|
| Compact CFG, bulk memory rejected | +0.79% | +4.32% | 8 paired samples; profiled f19 unchanged |
| Compact CFG, classified bulk memory admitted | -1.87% | +3.95% | 8 paired samples; 169 homes eliminated in 16 functions |
| One-pass masks, retained fn mask | -1.58% | +1.33% | 10 exec / 8 compile pairs; native image unchanged from bulk prototype |
| Worker scratch mask and cold emission | -2.17% | +0.77% | 8 exec / 6 compile pairs; all qualified native images unchanged |
| Immediate cold proof and emission, no retained mask | -0.87% | +2.21% | 10 exec / 8 compile pairs; all qualified native images unchanged |

The bulk-memory extension materially increased useful coverage, rather than changing workload thresholds. Function 19 eliminates all 13 declared initial homes: 3184 to 3152 function bytes. The complete LibTomMath native image falls from 93588 to 93060 bytes. One-pass analysis removes graph allocation, per-byte nodes/edges and fixed-point iteration. Worker scratch and cold emission restore the original hot function layout and unaffected zero loop.

## Correctness and native scope

All 102 application and 46 cached-core exact contracts passed in signal and explicit bounds modes before the storage-only refactors. The final ON source emits all 148 images byte-identically to that qualified source in both modes. All 148 final OFF signal images are byte-identical to exact37174169. See `cold-native-proof.txt`, `scratch-native-proof.txt` and `native-changes.json`.

Fixtures cover nested branches and branch tables to outer loops/blocks, unreachable then paths and reachable else paths, terminal function-label branches/returns, first-iteration initial reads, old operand-stack snapshots, calls and bulk memory with initial-valued arguments, typed call/tail-call decoding, malformed/unsupported control and prefix-policy independence. The intermediate scratch-reset fixture passed; the final helper has no retained mask. A final three-unused-import fixture with different local bodies passes both register and wrapper calling conventions, proving unconditional function-index/body mapping and preserving an initial-valued local in the second body. The one-pass result also agrees with an independent compact CFG fixed-point reference for every admitted bounded function in LibTomMath (57 functions). Full backend/catalog diagnostics with register lifetime checking passed before the storage refactors; focused diagnostic fixtures passed after each refactor.

Only stats-gini changes among the 102 applications. Fifteen cached cores change: BLAKE3, CoreMark, drwav, fannkuch, LibTomMath, Monocypher, NanoSVG, PCRE2, Floyd-Warshall, GEMM, Nussinov, SHA256, utf8proc, yyjson and zstd. This is an image inventory, not a full timing sweep.

## Focused timing

The paired subset uses a locked OS thread with requested interactive QoS and reverses option order. It does not prove physical-core affinity. Current same-process medians are in `scratch-subset-summary.md`; raw samples are in `scratch-subset.jsonl` and the `.raw` files. Beyond LibTomMath, drwav is -0.63% execution/-0.73% compile, SHA256 -0.77%/+2.62%, PCRE2 +0.71%/+0.99%, and stats-gini +0.12%/-2.91%.

CoreMark's memory oracle, zstd's input/initialization contract and Nussinov's full workload identity are preserved with the cached-contract harness. Four reversed separate-process 150ms pairs yield execution/compiler changes of -1.30%/+4.02%, -0.65%/+3.02% and -1.22%/+2.94%, respectively. These short process comparisons cannot establish small sub-percent effects. The initial attempted simple CoreMark harness correctly rejected its memory oracle before timing; that raw admission failure is preserved.

Exact-source controls separately compare pristine37174169, source OFF and source ON using the common default-ON loop-memory-base option. The prior fn-field version gave short, noisy source results (-0.29% target execution/+4.83% compile), motivating the worker-scratch/cold-emission mitigation. That scratch version's six-permutation source controls gave target execution -0.74%/compile +4.38%, with target OFF compile -1.63%. Several byte-identical execution controls moved +1.4% to +2.7%; compile controls also varied materially (kissfft OFF -5.87% but ON +3.36%). These separate-process results are preserved in `scratch-source-summary.md` and are not treated as a clean estimate of analysis cost. They motivated the final immediate-helper mitigation, which removes every compile-attempt and metadata change.

The final source comparison rotates all six base/OFF/ON permutations with 300ms compiler and 250ms execution budgets. It confirms target execution -1.85%, but total compiler latency is +5.11%; final OFF source is -0.46% execution/+2.18% compiler for that target. OFF compiler controls remain mixed, so the experiment does not establish a uniform disabled-source tax. Enabled unchanged-guest execution controls stay within roughly 0.8%. The final measured execution/compiler balance is insufficient for default retention.

| Source ON versus pristine37174169 | Execution | Compiler latency |
|---|---:|---:|
| ADPCM (unchanged native) | +0.61% | +1.51% |
| glob (unchanged native) | +0.65% | +2.19% |
| register VM (unchanged native) | +0.31% | -2.45% |
| fastfloat (unchanged native) | +0.81% | +0.64% |
| kissfft (unchanged native) | +0.26% | +5.93% |
| PCRE2 | -0.40% | +3.30% |
| LibTomMath | -1.85% | +5.11% |
| stats-gini | -1.89% | +4.37% |

Stats-gini OFF execution also moved -1.90% versus pristine despite identical native bytes, so its source execution improvement cannot be attributed to zero elision. The cached-contract timings remain short separate-process screens, not precise estimates of sub-percent effects. See `cold-source-summary.md`, `cold-target-summary.md` and all preserved raw samples.

Frozen final builds: `/tmp/paired-entry-dead-zeros-cold`, `/tmp/parity-entry-dead-zeros-cold.test`, `/tmp/inventory-entry-dead-zeros-cold`. The scratch and qualified predecessor builds also remain frozen under their original names. Pristine source control: `/tmp/paired-next-20261011-base`. All reservations ended successfully; no live benchmark or profiler handles remain from this experiment.
