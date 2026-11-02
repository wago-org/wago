# Final port measurement preparation

**PREPARATION ONLY — READY FOR LEASE, not authorized to measure.** No lease,
benchmark invocation or intentional timing is part of this assignment. This
harness does not enter shipped builds and cannot affect production performance.
The implementation still needs an independent measurement campaign. This report
neither closes review findings nor grants PR readiness.

Source `5345bd365c13963f1bf60e26bb80789ab9ead4cc`; correctness evidence
`2c9f72744dd6f414fc8f236c2d45aa39ce6ced69`; pre-region baseline
`a3c08365f531eb3c9003e3acf3e0a822c12c31f4`. These are immutable commits, never
moving branch HEAD. `plan.json` is the concrete logical matrix; `inputs.json`
pins all READY TO MEASURE reports (including historical negative/incomplete
reports, which confer no qualification). Source exports receive only declared
identical measurement test overlays. Never copy dirty production or corpus
scripts from the shared worktree. A review repair changing production requires
a new source pin, correctness reconciliation and a newly sealed campaign.

## Attribution and workload contract

| Lane | A / B | Operation and required evidence |
| --- | --- | --- |
| Full port/default | baseline Off / final Off, worker 1 | Six native allocation cycles, actual guest graph, scalar/noGC, compile controls; detects default-path overhead |
| Stage 3–5 policy | final region Off / On | Native scalar, 64-node chain, 1/256 and 256/256 survivors, immediate promotion, 64-KiB LOS churn; region shift 8 in original fixtures, nursery/survivor 4096 each |
| Stage 6 graph | baseline/final Off; final shift 0/12; final worker 1 / 2,4,8 | 8192 actual nodes, 2/32 cyclic/shared edges per node, root array, exact 8193 live census, Verify, root drop to zero; 16-MiB nursery and 64-MiB shared old/LOS capacity |
| Stage 7 evacuation | final evacuation Off / On at each worker 1,2,4,8, shift 12 | 8448 promoted objects with explicit 32-KiB sparse gaps; force pressure on both sides; On must move 8448*32 bytes; exact live count, Verify and final zero; inclusive create/promote/collect/close cost |
| H2 | final shift 0 / 6 | Real public/imported host global/table replacement, aliases, bulk fill/copy/init/grow and final converted-cycle release; existing exact receipt, publisher retirement and physical-release assertions |
| H6 | final shift 0 / 6 plus shape revision pair | Active/exit boundaries, nested failed initialization and callback quiescence; retain ordered ownership and drain assertions |
| Host shape | `8dcaa7095169a36eff93652a2b74b10a51900655` / `6e706159a0eb71b939bc0e9c9e0e32d5e03d5f4f` | Same owner/receipt fixtures; collector 200→192, mark 192→168 bytes on 64-bit host; independent source-size receipts, lifecycle costs and Go B/op where available |
| TinyGo idle scans | 1 / 1 or 8 engines at each 1/8-MiB mapping size | Eight explicit runtime GC calls over live idle registered mappings, Close and post-Close GC; includes lifecycle and GC costs, no pure scanner throughput claim |
| TinyGo active scans | final / final repeat control | Existing deterministic foreign-SP handshake, host reentry, prepared entry, Close-thunk failure; eight GC calls where fixture specifies; no unsafe pre-registration baseline |
| Compiler | baseline / final, identical module bytes and options | Stage3 public Compile+Close controls; canonical corpus BenchmarkCompile measures codegen+Close with decoding/validation excluded; report these separately |
| Historical corpus | baseline / final canonical settings | All 128 IDs retained, 121 binary hashes and 47 input references; compile and runtime/command adapters plus existing semantic/output oracles; NOT GC graphs |

Off/On means region policy unless explicitly labeled evacuation. Region On now
includes mature descriptors; it is not a stage-3-only causal estimate. Worker
counts include the coordinator. Do not claim additive per-stage gains from the
full-port pair. Synthetic graph data are separate from real application census;
these graphs really allocate managed objects but are not application workloads.

Original guest graph: 32-node Wasm chain escaping via mutable global, result 7,
64 calls produce at least 2048 allocation charges and 32 live nodes after GC.
Scalar/noGC controls return 7; noGC has zero GC charges and runs Off/Off because
it lacks collecting-frame admission. Compile options are CoreV3, inline=false,
lifetime analysis=true only for those known-safe generated modules. Runtime
region settings are not compiler proof settings. Compile comparisons use no
runtime policy ablation; compile same-head repeats are controls only.

Native graph and evacuation benchmark cycles include setup, checks and cleanup.
Guest invocation benchmarks exclude compilation/instantiation. Ownership and
TinyGo `test-cycle` cells measure one complete fresh-process correctness cycle,
including fixture construction, compiler work, assertions, output and teardown;
never label them store latency or isolated GC pause. Fresh same-binary repeated
controls quantify this process cost. Separate instrumented correctness receipts
record counts/bytes without adding heavy diagnostics to one side only.

Before acquiring timings, extend census receipts with actual charges, allocation
bytes, live handles, young/old/LOS occupancy, copied/promoted/evacuated bytes,
region used extents, descriptor/run/reserved credit, backing length/capacity and
joined phase participants. `ParallelActivity` must witness work; requested 8 is
not proof of 8 active workers. A below-threshold serial fallback is a separately
labeled result. Do not derive peak RSS or fragmentation from end-of-cycle backing.
Capture peak process RSS separately if requested, with identical sampling.

Additional correctness coverage before timing: source-sized 256-MiB chain,
zero-credit survivors, fragmented old/LOS high cards, partial cancel/refill,
required/optional refusal and retry, callback serial fallback, traps/EH/tails,
borrowed arrays, and failed admission. Use the pinned stage4–8 and combined
receipts as the exact test census, rerun changed overlays in all three modes.
These broad controls are not all multiplied into timing cells: `plan.json`
explicitly defines the bounded measured subset. Rejection/fault tests are
correctness gates, not successful-operation performance samples.

## Applicable targets and prohibited comparisons

Primary timing target is native Linux amd64 standard Go 1.27.1. Freeze executable
hash, GOROOT, compiler settings, kernel, CPU topology/governor/power policy,
GOGC/GOMEMLIMIT/GODEBUG/GOEXPERIMENT and environment. Remove inherited diagnostic
and profiling flags. Use ordinary binaries for timings; ordinary, wagodebug and
wago_regalloccheck must pass correctness first. Keep diagnostic modes separate
if explicitly authorized for measurement. The candidate has no automatic safety
exemption simply because combined tests passed.

Use one fixed physical core for serial revision/policy cells. For worker cells,
use the same fixed eight-physical-core allowed CPU set on BOTH sides with
GOMAXPROCS=8; Workers selects participation. Do not pin worker8 to one CPU.
Record SMT siblings and hybrid core class; an insufficient homogeneous eight-core
host must label that lane unsupported or get a separately approved topology
plan. Never silently reduce requested workers. One-processor fallback is a
correctness/control lane, not parallel scaling evidence.

ARM64/RV64 QEMU receipts qualify supported execution, never native speed. ARM64
has supported host opaque receipts; RV64 has compact extern and managed-function
controls and preserves its 200/208-byte shapes. RV64-excluded host receipt tests
are NOT_APPLICABLE. Darwin remains NOT_RUN without a native runner; cross builds
are not execution. Separate native target campaigns require host grants and
pinned toolchains. Do not use emulation to fill those speed cells.

Actual patched TinyGo 0.42 Linux amd64 uses `-scheduler=threads -gc=conservative`.
Wago's collector is serial: Workers 0/1 only. Stage8 supersedes historical stage7
TinyGo evacuation rejection for collecting Throughput + regions; ProfileTiny is
a distinct nonmoving control, not region mode. TinyGo foreign-stack mapping
registration must remain enabled; never disable it or run the unsafe pre-fix
foreign-SP scanner to manufacture a pair. Opaque host receipt tests excluded
from TinyGo builds remain unsupported, not passing. TinyGo evacuation Off/On
must also run the final evacuation fixture (serial only) after actual compiler
qualification; the native Go result does not stand in for that result.

**Never execute the known unsafe baseline no-inline/lifetime-analysis clang-cpp
configuration.** Baseline canonical checked compile preflight precedes canonical
execution. A checked rejection is retained as failure/unsupported and bars that
execution configuration in every mode. Explicit RFW catalog compilation can
record the known rejection but cannot yield a matched successful compile-speed
ratio for that row. Canonical compilation/execution is a separate configuration;
its passes do not waive the prohibition. Do not rerun the old stage2 run.py:
it invokes 1x benchmarks during purported validation and then starts timing.

## Exact lease and drain discipline for a later continuation

1. Obtain explicit owner authorization for **measurement**, naming this protocol
   commit, source/evidence pins, independent measurer, host, bounded budget and
   supported target lanes. This preparation request is not that authorization.
2. Parent confirms final review and all test/build/fuzz/race/QEMU/TinyGo workers
   have drained, with owner acknowledgments, process trees and empty active jobs.
   `final-combined-5345/process-drain.json` attests only its own qualification.
   Inspect live host/container namespaces and any remote runner owners; absence
   from a local process list is not global readiness. Schedule no competing
   CI/build/benchmark job; block new worker starts until release.
3. Reconcile **both** `/home/dev/Projects/wago/.performance-measurement.lock` and
   `/tmp/gc-trim-regate-laptop-timing.lock` with the global owner. Read every live
   grant, including trim and wakejoin. Wakejoin currently says released and
   samples_must_not_resume=true, superseding stage3's old active observation.
   Never resume it, delete/recreate a lock inode, or rewrite another owner's
   record. A released JSON record alone is not a lease transfer.
4. Owner records canonical+legacy inode/device, status and holder reconciliation,
   confirms no namespace/remote competing lease, and transfers the single global
   grant. Acquire nonblocking advisory exclusive flocks in canonical-then-legacy
   order; if either fails, release the first and stop. Hold both descriptors for
   the whole campaign. This transitional dual-lock discipline protects legacy
   users; later canonical-only adoption requires explicit global coordination.
5. Build/export and validate remaining adapters before sample acquisition; no
   benchmarks for calibration before lease. Freeze executable/tool/input hashes,
   source/overlay hashes, all expanded argv/env/cwd, affinity, oracles, expected
   leaf test census, sensor names and plan SHA in a new seal. Owner drain is
   rechecked after builds. The seal must be committed before first sample.
6. Grant records authorization reference, independent measurer, hostname,
   reviewer-drain receipts, both lock identities and reconciliation, correctness
   receipts and exact seal SHA. Any source/tool/fixture change invalidates seal.
7. Record acquisition/release and raw output including rejected samples. On
   interrupt, finish or terminate and reap only owned processes, wait for all
   child/helper lifetime joins, preserve partial evidence, release both flocks
   without unlinking, close the grant and acknowledge drain to the parent.
   No silent resume or leftover background sampler. Regrant for continuation.

## Admission, pairs and acceptance

Use six blocks per cell, alternating ABBA/BAAB, two adjacent opposite-side pairs
per block. Deterministically shuffle cell order with seed 20261102, then freeze
it. Use 500ms Go bench duration, one process per sample; test-cycle cells use
count=1. No adaptive reduction of a slow side, optional early success stopping,
or per-side outlier trimming. Record all benchmark leaves; corpus adapters can
emit multiple export/semantic leaves and must join by leaf, never select the
fastest leaf. Timing executor refuses a zero-leaf or changed-leaf receipt.

Read every applicable CPU package/core sensor immediately before **each** sample;
maximum must be <=60 C. Missing/unreadable sensors fail closed. Sample thermal,
/proc/stat, CPU pressure and process census at 250ms without pausing the child.
>=85 C, throttling evidence, sensor loss, foreign workload, oracle failure or
process error invalidates the entire four-sample block. Wait for <=60 C before
retry; retain both slow and fast invalid observations. Maximum three attempts
per block, 300-second admission wait, then stop and report incompleteness.
Record sampled peak limits; polling cannot exclude shorter thermal spikes.

Both A/A and B/B repeat ratios must lie in [0.90,1.10] **for each leaf**. Failure
rejects the whole block. Control failures on scalar/noGC also invalidate the
surrounding acquisition segment for review. Inspect telemetry for systematic
load, frequency or order drift even if controls pass. Do not tighten or loosen
these thresholds after seeing results.

Analyze log(B/A), average the two pair logs within each block, and bootstrap
whole blocks (10000 resamples, seed 20261102); report geometric mean, median,
95% interval, all six block values and repeat controls. Six blocks are six
independent units, not twelve. These are descriptive per-cell intervals;
for a family-wide claim apply Holm correction to predeclared two-sided paired
block sign-flip tests at family alpha .05, and disclose low power with six
blocks. No significant result is promised. A practical favorable cell requires
upper 95% ratio <0.98, adverse lower >1.02; neutral only when the entire interval
is inside [0.98,1.02], otherwise inconclusive. Any adverse cell prevents an
unqualified overall GAIN. Report compiler, runtime, memory and unsupported rows
separately; no arithmetic/geometric average may erase a regression or missing
required gate. For causal shape comparison use only the explicit shape pins.

## Census and campaign size

`census.json` retains every historical ID including failures and missing inputs.
The inherited combined evidence contains 768 ID/revision/mode rows and 384 joins;
retain its provenance instead of pretending this preparation reran them. Before
acquisition, hash all 47 input references too and join fresh outcomes by
ID/revision/mode/config/adapter/export. Keep missing, compile failure, unsupported,
skipped, runtime failure and reference-only states distinct. Captured V8/wasmtime
references are not fresh execution; wazero and Wago exact contracts remain
separate. Never count a successful load as a runtime oracle pass.

The logical matrix has 316 cells (256 historical compile/runtime rows and 60
focused cells), 1896 accepted blocks, **7584 accepted process samples**, at most
22752 attempts under the three-attempt cap. Unsupported/zero-leaf rows remain in
the denominator but do not execute. Four TinyGo serial evacuation Off/On/repeat cells are included in this count.
Plan two four-hour acquisition windows plus up to two hours for untimed export,
build/adapter qualification. This is a scheduling allowance, **not a prediction
from timings**. Cooling and large compiler cells may exhaust it; at eight hours
of acquisition stop between samples, retain incomplete cells and request a new
bounded continuation. Never extend a budget silently or drop hard corpus rows.
Prioritize focused cells, then all compile census rows, then runtime corpus rows;
freeze randomized order within each stratum. No partial pass is a full verdict.

## Reproduction and handoff

- `python3 make-plan.py` regenerates logical cells and read-only receipts.
- `python3 prepare.py <this-directory>/.prepared --build-and-check` exports pins,
  overlays identical graph/old stage3 harnesses and runs ONLY Tests in three modes.
- `python3 validate-extra.py` validates candidate evacuation and graph worker
  settings 1/2/4/8, Off/On, in all three modes. No Benchmark selector is invoked.
- `python3 validate-tinygo.py` checks the new idle mapping fixture at 8 engines /
  8 MiB and serial evacuation Off/On on actual patched TinyGo in three modes.
- `python3 expand.py --bindings <qualified-binaries.json> --out <new-seal.json>
  --serial-cpu <cpu> --parallel-cpus <eight-cpus>` expands frozen selectors to
  commands without executing them. Missing binaries stay explicit UNBOUND rows;
  `seal-requirements.json` enumerates the separate owner grant requirements.
- `python3 validate-plan.py` checks logical identities/unsafe-baseline exclusions,
  source test selectors and evidence hashes without executing workloads.

Run from the script directory or supply its full path. Fresh output directories
are required. `.prepared` is disposable owned data, never an implementation
worktree. Copy logs/identities into validation evidence before deleting it.

Remaining acquisition setup is explicit: build the same archived bench adapters
on both pins, the shape pair and actual TinyGo binaries; run correctness-only
nonempty census checks in three modes; complete the remaining TinyGo dimension/selector census and add its serial
evacuation executable bindings;
freeze argv/env/binary hashes and leaf census. These are prerequisites to the
first timing, not evidence acquired here. The supplied fixtures and selectors
make that continuation concrete; no claim of measured or universally built
adapters is made. Production source remains untouched. No PR/push/subagent.

## Preparation result

All **45 correctness commands passed**: 12 baseline/final native/guest commands,
24 candidate worker/policy graph+evacuation commands, and nine actual TinyGo
commands, each family covering ordinary, wagodebug and wago_regalloccheck.
`validation-summary.json`, identities, fixture hashes and compressed raw logs
retain the exact scope. TinyGo checks cover idle 8×8-MiB mappings and serial
shift-12 evacuation Off/On; smaller idle points and shift 6 still need the
per-selector acquisition census. Standard Go extended checks cover workers
1/2/4/8, graph shift 12, evacuation Off/On. No performance direction follows.

Both original source archives were byte-checked after execution with zero drift;
only declared test overlays were added/replaced. Disposable exports and private
TinyGo cache were removed after preserving receipts. Original shared dirt/index
were not included. All own test processes drained; reviewers may still be active.
No new engine finding was established or triaged in this preparation.
