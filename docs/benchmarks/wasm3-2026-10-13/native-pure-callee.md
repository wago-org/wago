# ARM64 pure single-callee native callbacks

Source `9ed2645085982cb74fab5b629dc260d8cc31385f`, compared with retained
`7a92edde120d321859222d53f761336ac7e19525`. Apple M4 Max, Go 1.27.1,
`GOMAXPROCS=1`, shared device reservation. [Audit summary](native-pure-callee.json)
and [complete sample/receipt data](native-pure-callee.json.gz) (gzip JSON).

The existing native scalar proof now recognizes one specific Go 1.27 i32
call-and-return wrapper. It checks the complete frame/stack-growth sequence,
function boundaries and relative branch destinations, then independently proves
the callee using the existing bounded register-only decoder. The native callback
contains that pure callee body. No benchmark name, module hash or identity-result
substitution is used. The runtime test compares its complete emitted callback
with independent lowering of the callee, including nonidentity arithmetic.

Every imported callback call and original guest shared-memory store/load remains.
The nested Go helper frames are fused; a physical Go/helper call is not retained.
This is pure callback lowering, not faster arbitrary Go transitions. Stateful,
indirect, multi-call and unsupported wrappers keep their Go fallback. Existing
ownership, artifact and profiling policies apply. `native-leaf-host=false`
disables the optimization; `WAGO_ARM64_NATIVE_SCALAR_CALL=0` disables the new
single-callee proof at binding. Other compiler versions fall back conservatively.

## Matched results

| Comparison with retained source | Execution delta / ratio |
| --- | --- |
| Shared-memory pure-helper loop, focused reversed orders, 15 samples/phase | **-87.47% / -87.19%** |
| Same loop, independent nine-launch rechecks, 45 samples/phase/order | **-86.11% / -87.04%** |
| Focused loop against pinned w2c2 Clang `f303ed27` | **1.639x / 1.677x**; still slower |
| Short `WasmFunc.Invoke`, two owned typed helper aliases/invocation, same-binary policy toggle | **-16.77% / -17.14%** |

The long contract uses artifact `c9310c605dd85f4832d474a2c4e14aee30e2864c048b4c40327bbcfe8321eb53`,
input 4096, 64 repeats, 262144 callbacks/invocation and independent oracle
536739840. Its guest image remains 944 bytes. The short probe uses the existing
`nativeLeafAliasModule` shape, identity helper, input/oracle 17, five paired
samples per construction order and one million invocations/sample. These are
distinct entry/amortization measurements.

The peer preserves one noinline/noipa C callback with an opaque compiler memory
barrier. The Wago adapter discloses pure helper fusion. The new result must not
be presented as an arbitrary effectful Go callback improvement.

## Complete corpus and contrary evidence

Both revisions completed all **266 workloads**: **532 captures**, no functional
outcome changes or failed resource measurements. Numeric pair counts are 255
for compile/steady and 258 for instantiate/first-call.

| Full paired geometric mean | Current / retained |
| --- | ---: |
| Compile | 1.01118 |
| Instantiate | 1.03915 |
| First call | 0.99856 |
| Steady execution | 0.99381 |
| Compile process RSS | 0.99517 |

The feature steady cohort is 0.98255 (107 pairs); the main steady cohort is
1.00203 (148 pairs). These are incremental deltas, not default-main comparisons.

Every steady outlier outside [0.90,1.05], compile outlier outside [0.80,1.20],
and four controls was independently rechecked in both orders: **58 workloads,
232 captures, nine launches and 45 samples/phase**. No execution regression
above 5% repeats in both orders. Startup costs remain:

| Phase / workload | Forward ratio | Reverse ratio |
| --- | ---: | ---: |
| Compile: bio reverse complement | 1.262 | 1.480 |
| Compile: i64 carry overflow | 1.154 | 1.074 |
| Compile: private secondary copy | 1.072 | 1.099 |
| Instantiate: table fill/copy | 1.186 | 1.972 |
| Instantiate: Floyd-Warshall | 1.655 | 1.091 |
| Instantiate: ADI | 1.193 | 1.136 |
| Instantiate: BK-tree search | 1.167 | 1.050 |
| Instantiate: i64 carry overflow | 1.134 | 1.059 |
| Instantiate: unaligned load/store | 1.124 | 1.061 |
| Instantiate: pair results | 1.117 | 1.059 |

These measurements are retained without attributing them to an unproven cause.
Identical guest images do not exclude host-side costs. The repeated large
execution gain and the short-API gain justify retaining this bounded proof,
with startup regressions visible and the PR still draft.

## Correctness and source qualification

All **255 inspectable guest images** are byte-identical to the baseline;
disabling the new proof also restores every baseline image. Host callback
trampolines are runtime mappings, covered by lifetime tests and process RSS,
rather than the guest-image metric.

The exact source passes broad ordinary and regalloc-checker runtime, ARM64
backend and public packages, plus focused profiling-policy tests. Tests cover
source/artifact admission, owned aliases closed before invocation, integer
boundaries, effectful callbacks, cancellation and GC progress with one Go
processor. All **373 ARM64 compatibility contracts** match the baseline:
**346 passed / 27 unsupported / 0 failed**. The frozen manifest and artifact
hashes were verified before collection. Browser-specific unsupported behavior
was not implemented or relabeled as passing.

The common legacy custom-slot host-call control remains slower than pinned
current main: **+6.1% / +5.0%** versus `069a26ea`, and **+5.1% / +2.4%** versus
merge base `8097344a`. It is a different binding/entry workload. Historical
control reports had an incorrect hardcoded baseline label; see the
[source-attribution correction](main-control-source-attribution.json) and
[fresh pinned controls](pr-main-host-control.md). No branch-wide regression-free
or default-main speedup claim is made. Publication is local only.
