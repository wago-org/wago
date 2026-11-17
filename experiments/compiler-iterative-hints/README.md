# Iterative hint scanning

Baseline 74ab035a. Both byteBodyScanner and globalScoreByteScanner now use
explicit bounded control frames rather than native recursion. The allowed
nesting depth and immediate/error consumption remain unchanged. An inline
16-frame buffer serves shallow functions; deeper expressions grow a slice.
Measurements and qualification follow below.

The original recursive scanner bodies are retained test-only under !tinygo.
Differential tests compare full hint views including local-event tapes, sparse
global scores/eligibility, partial eligibility tracker state, entry-local state,
loop constant candidates, call/terminator returns, error fields and reader
positions. They cover 1,000 deterministic generated control trees, truncations,
nested loops/if/else/try_table and the 20,000/20,001 depth boundary. A separate
iterative-only deep test remains available under TinyGo, avoiding an oracle
stack overflow that would obscure production behavior.

An initial differential failure identified recursive error propagation's zero
terminator for nested invalid else, even though error offsets already matched;
the iterative path now preserves it. No test was weakened to accept this.

Go 1.27.1 linux/amd64 full backend suite passes (8.965s), GOMAXPROCS=2,
go test -p=1 ./src/core/compiler/backend/railshot/amd64 -count=1.
Native-code corpus equivalence, TinyGo default-stack causality, RSS and paired
performance were measured independently as recorded below.

## Historical 132-workload checkpoint (superseded below)

Cloud Linux AMD64, Go 1.27.1 and TinyGo 0.42.0. Corpus: 132 exact
artifact hashes recovered from the live wasm.fyi 145-contract Linux manifest;
13 rebuilt artifacts remain unavailable. This is not full-corpus acceptance.
Fresh processes, single compile worker, CPU affinity, absolute `/proc` VmHWM.

Standard Go, nine alternating paired trials: mean of per-workload median
compilation times 1.051 ms baseline versus 0.984 ms iterative (6.4% lower).
Mean per-workload median peak RSS 7.584 versus 7.602 MiB. All 2,376
compilations pass and all 132 generated native-image hashes are identical.
The 40% compilation-latency target is not achieved by this result.

TinyGo default-stack iterative: all 660 compilations pass (five trials),
mean per-workload median peak RSS 2.643 MiB and compilation time 1.616 ms.
The original scanners crash on pcre2 and yyjson with the same default-stack
build flags. Increasing only the old build's stack to 2 MiB passes all 132,
but uses 6.809 MiB mean peak RSS and 3.709 ms compilation. That oversized-stack
workaround is not a fair baseline for claiming a latency improvement.
All 132 iterative TinyGo native-image hashes match the old 2 MiB-stack build.
Execution latency and additional tagged qualification are recorded below.

These measurements distinguish the stack-safety fix from the broader compiler
performance goal. Go and TinyGo must not be compared as identical toolchains.

### Additional qualification

Full AMD64 regalloc-checker and profile suites pass (13.320 s and 21.455 s).
Public GC tests pass (8.247 s) with the clean pinned Release3 spec corpus
9d36019973201a19f9c9ebb0f10828b2fe2374aa.

All 102 exact application kernel result oracles pass seven paired trials,
128 timed calls each. Aggregate mean of workload median execution times is
204.412 us baseline versus 203.611 us iterative. Eighteen initial per-workload
medians appeared over 5% slower. Retesting these with seven paired trials,
2,048 calls and an additional identical-baseline control produced no candidate
median regression above 3.2%; the identical-binary control itself reached 8.1%
on one workload. This is reassuring, not proof that cloud timing is noiseless.
The 30 other exact fixtures have compilation/native-image verification, not
this application-export execution check.

TinyGo arithmetic mean RSS across all 660 samples is 2.643 MiB; averaging
worst-trial RSS per workload instead gives 2.646 MiB. The default Go embedding
remains above the memory target. Full 145-workload acceptance is still blocked
on 13 exact artifact bytes, and the 40% latency target remains unachieved.

### TinyGo nesting-boundary test

The default-stack TinyGo 20,000/20,001 nesting-boundary test passes (0.018 s)
with unchanged production files and only the iterative depth test selected at
source level. The normal package test build is independently blocked by
pre-existing compile_test.go constant indexing of TinyGo's zero-sized scratch
array (lines 111, 125, 130), even with a test-name filter. Therefore this is an
isolated targeted test, not a full TinyGo backend-suite pass. All temporarily
excluded test sources were restored; none were removed from this branch.

### Fairer TinyGo stack baseline

The original scanner still crashes on pcre2/yyjson with an explicit 64 KiB
stack, but 128 KiB passes the full available corpus. Seven alternating paired
trials compare old-scanner 128 KiB to iterative default-stack, with all other
build flags identical: tasks scheduler, conservative GC, opt=z, no-debug.
All 1,848 compilations pass, and all 132 native images match. Mean workload
median compile time is 1.680 versus 1.583 ms (5.8% lower); mean workload median
peak RSS is 2.841 versus 2.599 MiB. Both configurations meet the available-corpus
memory target. This supersedes the oversized 2 MiB workaround as the useful
TinyGo performance comparison. It changes source and stack configuration,
so it is not a source-only comparison. The toolchain includes the same existing
TinyGo tasks-exit runtime patch in both builds.

## Full 145-workload qualification

All 13 missing artifacts were recovered from wasm.fyi's canonical
`https://wasm.fyi/api/artifacts/{sha256}.wasm` route and independently hash
verified. No rebuilds or same-name substitutions. `contracts145.json` records
all exact workload IDs and SHA256s. The missing-corpus blocker is resolved.

Nine paired trials with Go 1.27.1: all 2,610 compilations pass; all 145 generated
native images match. Arithmetic mean of workload median compile times is
1.208 ms baseline versus 1.180 ms iterative (2.4% lower); peak VmHWM means are
7.599 versus 7.618 MiB. This full-corpus result supersedes the earlier 132-case
6.4% figure and does not meet the 40% source-redesign latency target.

Seven paired TinyGo opt=z trials: original scanner with 128 KiB stack versus
iterative scanner default stack, all 2,030 compilations pass and native images
match. Mean workload median compile time is 3.139 versus 3.003 ms (4.3% lower),
and mean peak RSS is 2.866 versus 2.619 MiB. Both pass the memory target in this
embedding; the iterative scanner additionally removes native recursion.

TinyGo opt=2 iterative also passes all 145 workloads (1,015 compilations), with
mean compilation 1.378 ms and mean peak RSS 3.242 MiB. The original opt=2 scanner
with 128 KiB stack crashes on pcre2 in every trial. Its successful-only aggregate
must not be used as a full-corpus baseline. A larger-stack control is pending.
Changing opt=z to opt=2 is host build-configuration tuning, not a source-only
speedup, and trades resident code for speed.

These compile/RSS measurements include all 145 exact artifacts. Execution
oracle/timing checks currently cover the 102 application kernels described
above; the other 43 contracts require their specified fresh-instance sampling,
host profiles, input setup and oracle checks. Native-image equality does not
replace that pending full execution-contract qualification.

### Valid performance-optimized TinyGo control

Increasing only the original opt=2 scanner's stack to 256 KiB yields a valid
full-corpus control. Seven alternating paired trials all pass (2,030 total),
with all 145 native-image hashes identical. Mean workload median compilation
is 1.376 ms old-256 KiB versus 1.252 ms iterative-default (9.0% lower), and
peak RSS is 3.675 versus 3.242 MiB. These are source-plus-stack-configuration
comparisons, not evidence of a 40% source-only improvement. Both use the same
performance-oriented TinyGo host flags. Do not compare a failed or opt=z
baseline against opt=2 as though only source changed.

### Official execution contracts

Both Go builds pass all 43 remaining exact execution contracts using the
upstream wasm-bench adapter at a017c56f, with pinned wasi v0.3.1 and
component-model v0.1.6. A documented adapter-only compatibility patch rejects
removed AOT APIs/scenarios; steady execution, inputs, fast HostCall imports,
fresh-instance resets and oracle handling are unchanged. This covers the
BLAKE3 35-vector contract, AssemblyScript initialization and memory oracles.
Together with the 102 application kernels, all 145 execution oracles pass.

Eleven paired trials, 15 measured fresh-instance samples and five warmups:
946/946 adapter runs verified. Arithmetic mean of workload median execution
times is 2.195 ms baseline versus 2.209 ms candidate (0.6% higher). Two initial
outliers required retesting: monocypher and tiny/add. Seventeen additional
paired trials with 51 samples and identical-binary controls show monocypher's
apparent 52% regression was bimodal process noise also present in the control.
Tiny/add remains borderline: pooled baseline median 180 ns versus candidate
189 ns (+5%), with median paired ratios slightly above 5%. The per-workload
execution regression gate is therefore unresolved, not declared passed.

The public compact records retain all measured samples, artifact/contract and
binary hashes without repeating machine-local artifact paths. Compilation
probe native-image equality is established separately; official default
function-worker settings differ from the probe's explicit single worker.

### Official adapter compilation RSS

Three trials across all 145 exact contracts, on both builds: 870/870 runs
verify successfully. The official memory-profile phase protocol pauses at
`compiled`, before oracle instantiation; Linux `/proc/PID/status` VmHWM is read
externally before acknowledging the barrier. This measures absolute process
peak through compilation, including adapter and memory instrumentation, not
Go allocated bytes or post-execution guest memory.

Mean workload-median peak RSS is 9.614 MiB for Go baseline versus 4.262 MiB for
iterative TinyGo opt=2/default-stack. Averaging each workload's worst of three
trials gives 4.264 MiB for TinyGo. This meets the requested arithmetic-average
5 MiB threshold in the official adapter, not merely in the smaller probe.
It is a configuration comparison, not an isolated source-change comparison.

TinyGo harness compatibility marks two unsupported GC telemetry fields
unavailable and explicitly rejects command fixtures because TinyGo lacks
os.OpenRoot. Go retains scoped filesystem access. All 145 measured contracts
are core contracts; none were substituted or dropped. No confinement fallback
was introduced. The old adapter's AOT description metadata remained stale
although its AOT methods rejected requests; no measured request used AOT.

After these runs, an ephemeral workspace reset removed temporary harness
sources and binaries. All raw results, artifact/contract hashes and measured
binary hashes survived, as did every compiler source commit. The harness is
being reconstructed from pinned upstream source and recorded compatibility
decisions, with metadata corrected and source persisted before new builds.
Do not claim the lost binaries have been reproduced bit-for-bit. A matched
TinyGo control comparison will use newly built, newly fingerprinted binaries.
