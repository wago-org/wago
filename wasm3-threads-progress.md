# Defined shared memory and zero-timeout wait checkpoint

2026-10-12, Apple M4 Max, Darwin/arm64, Go 1.27.1. The full feature and
performance-parity objective remains open.

## Implementation

Wago `c1acd6c0` adds instance-owned shared-memory backing, separate from instance
control memory. Shared-memory exports retain the producing instance until the
last importer closes. Mapping quota checks charge both mappings; failed starts
release both. wasm-bench `c351f3d` admits the `threads-defined-v1` host profile
with threads explicitly enabled and explicit bounds checks.

Wago `05545b7d` lowers statically zero-timeout shared waits directly to native
loads and comparisons on arm64 and amd64. The lowering returns mismatch (1) or
timeout (2), preserves alignment-before-bounds trap order, and leaves parking
and notification on the existing helper path. Differential tests cover both
wait widths, shared and unshared memory, offset overflow, upper address bits,
expected-value widths, and misaligned/out-of-bounds addresses.

Wago `967218d9` admits memoryless `atomic.fence`, with original and codec-loaded
execution coverage. The [threads proposal](https://github.com/WebAssembly/threads/blob/main/proposals/threads/Overview.md)
defines fences without a target memory.

The next checkpoint routes shared `memory.size` through the instance memory
directory rather than the separate control mapping. Defined and imported
memories with zero, one, and three pages execute correctly before and after
codec reload on both native architectures. The full `^TestThreads` group passes
on arm64 and amd64 after this change.

The ordinary access checkpoint routes all 23 scalar and 22 SIMD memory
instruction forms through shared backing on both architectures. Shared loads
materialize immediately; shared stores bypass ordinary store forwarding.
Differential execution against the existing unshared path checks exact results,
memory contents, and trap codes, with unaligned addresses, near-end accesses,
32-bit address truncation, and offset overflow. Defined and imported shared
memories are exercised from original and codec-loaded products. The full
`^TestThreads` group passes on native arm64 and amd64. The local Core 3
regression corpus and unsupported-proposal fail-closed tests also pass.

At this initial checkpoint, bulk segment initialization, shared memory64,
multiple memories, and broader import/segment combinations remained restricted.
Later checkpoints below add segments, shared memory64, and several import combinations.
Shared copy/fill and growth support are recorded below. JSPI and JS-string embedding profiles remain unsupported.

## Execution measurement

The wasm.fyi source-built performance contracts use 4,096 inner operations and
64 guest repetitions per invocation. Each result below is the median of three
independent launch medians, each with five oracle-verified samples, two measured
warmup batches, and adaptive batches of at least 50 ms. Units are nanoseconds
per entire guest invocation. All successful samples met the duration target.

| Contract | Defined-memory implementation | Zero-timeout lowering |
| --- | ---: | ---: |
| atomic-load-store | 249,434 | not rerun |
| atomic-rmw-add | 916,967 | not rerun |
| atomic-compare-exchange | 894,102 | not rerun |
| atomic-fence | 309,458 | not rerun |
| wait-mismatch-notify | 26,154,313 | 13,418,261 |

The wait/notify contract takes 48.7% less time (1.949 times faster). Both runs
use artifact SHA-256
`a85a706319b79d0118fd21c99aa294a1f47d3cbb789a779bc4238d1f58c910f2`.
It exercises zero-timeout mismatch plus notification; it does not measure
parked waits or cross-thread wakeup latency. The five new successful contracts
bring cumulative Wago coverage in the 90-contract set from 77 to 82. They have
no successful w2c2 pair in the initial capture, so these measurements establish
Wago progress, not parity.

Captured source identities:

- Before: `c1acd6c0b14a1050b4d0222406fc1f9c4edf50c8/source-0b08aee0cbda6a2e1cc95af3a551152240d4dbdc1e3a026e4ea6408dd525167e`.
- After: `05545b7dd9a8e223a7dd30962fd096dc7d770384/source-f57a02941f34153632ed019f8caa4603e21c77812d363ed91b169529315c542b`.

## Compile cost and validation

Five sequential samples per fixture compare `c1acd6c0` with `967218d9`. They
measure the zero-timeout optimization and memoryless-fence changes, not the
earlier defined-memory implementation. Timings are medians; no statistical
speedup claim is made from these sequential runs.

| Fixture | Before | After | Bytes/op before / after | Allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Small scalar, default config | 6,300 ns | 6,079 ns | 19,202 / 19,203 | 74 unchanged |
| Small scalar, explicit Core 3 | 6,268 ns | 5,995 ns | 19,058 / 19,058 | 73 unchanged |
| Imported workers, one compiler worker | 22,104,623 ns | 22,154,409 ns | 3,420,954 / 3,420,944 | 94 unchanged |

The larger fixture's generated code remains 1,587,220 bytes. These fixtures
show no meaningful allocation increase or compile-time regression; they do
not establish peak RSS or compile cost for the entire feature corpus.

`go test -tags=wago_regalloccheck ./src/wago -run '^TestThreads' -count=1`
passes locally and on native Linux/amd64 (Ryzen 7 7800X3D, hub@hub). Defined
shared-memory tests also pass with the race detector, including concurrent
instances, producer-first close, import ownership, queued wakeup, quota
rejection, and failed-start cleanup. Backend and optimization tests pass with
the allocation checker enabled. Unsupported-proposal fail-closed tests pass.

Raw evidence remains local and ignored by Git:

- `.validation/wasm3-parity/threads-defined-arm64.json`, SHA-256 `0784a0eeab499389e8929bfe907c83c14083f5ef0d142b988cc3a61e0696eae7`.
- `.validation/wasm3-parity/threads-zero-wait-arm64.json`, SHA-256 `e5caeb37d45f1277e9c8205b662e141d5a999c52b7e9fbde76bf8312e5e2f786`.
- `threads-defined-plan.json` and `threads-zero-wait-plan.json` in the same
  directory retain commands, artifacts, oracles, and batching controls.
- `threads-compile-before.txt` and `threads-compile-after.txt` retain compile
  samples from the baseline worktree and current checkout respectively.

## Ordinary access measurement and restored w2c2 thread comparison

wasm.fyi `97beeee` adds shared unaligned scalar and SIMD load/store contracts.
Their source-built oracles pass in Node/V8 at every declared size, twice per
instance. The expanded source-built performance set has 92 contracts; cumulative
Wago coverage is 84/92, with the same eight JSPI/JS-string profile rejections.
This coverage combines the initial capture and subsequent successful thread
captures, not a new full-suite rerun.

Wago source identity for all seven current thread measurements is
`0a24f14a364689fdf2f0734b2e7806b16f6105e5/source-69e9065f3a78775c218699417e2e46103b7ab88c03e190a94ac36e1676607b59`.
The w2c2 SIMD fork and translator remain at the initial comparison's identities.
The isolated AOT adapter initially rejected `threads-defined-v1` before running
the translator. After admitting that no-host-import profile, the C compiler
required `-DWASM_THREADS_PTHREADS -pthread`; wait/notify additionally needed the
fork's `futex/futex.c`, `futex/list.c`, and `futex/map.c` linked into the module.
The [adapter admission patch](docs/benchmarks/wasm3-2026-10-12/aot-defined-threads.patch)
is retained. Apply it to the isolated native adapter with `patch -p1`, then
rebuild using `cargo build --release --locked --no-default-features --features aot`.
These changes resolve harness/configuration gaps; the earlier unsupported and
failed captures remain separate.

Apple Clang 21 uses `-O3 -march=native -fno-strict-aliasing`, the explicit Xcode
SDK, and the pthread flags above. Each table row is the median of three launch
medians, five verified samples per launch. Every pair uses identical artifact
SHA-256 and meets the 50 ms duration target. Units remain ns per whole invocation.

| Thread contract | Wago | w2c2 | Wago / w2c2 |
| --- | ---: | ---: | ---: |
| ordinary-load-store | 209,161 | 94,085 | 2.223 |
| simd-load-store | 233,839 | 103,353 | 2.263 |
| atomic-load-store | 242,021 | 155,667 | 1.555 |
| atomic-rmw-add | 905,594 | 204,531 | 4.428 |
| atomic-compare-exchange | 889,516 | 254,805 | 3.491 |
| wait-mismatch-notify | 13,600,083 | 1,916,274 | 7.097 |
| atomic-fence | 307,669 | 257 | 1,199.241 |

The fence gap is now explained by generated-code inspection: Clang's exported
performance function has one `dmb ish` followed by closed-form integer arithmetic
and `ret`, with no guest loop remaining. The ignored `fence-inspect.c` and
`fence-inspect.s` retain the translated source and assembly. This optimization
remains part of the parity objective rather than being removed from comparison.
The RMW/CAS measurements above precede the CPU-gated LSE checkpoint below.

Five additional sequential compile samples at `0a24f14a` retain 74/73/94
allocations per operation for default scalar/Core 3 scalar/imported-workers
fixtures. Medians are 6,088 ns, 6,059 ns, and 22,146,119 ns; the larger fixture
retains 1,587,220 code bytes. Compared with the recorded `c1acd6c0` baseline,
these fixtures show no material compile regression. Peak RSS remains unmeasured
for this checkpoint.

Additional ignored evidence under `.validation/wasm3-parity/`:

- `threads-current-arm64.json`, SHA-256 `d85a45bd97e5b54226260bc22c33488277c70470dbf1ec07ca17d8a365c16db9`.
- `threads-w2c2-pthreads.json`, SHA-256 `cec83b7bc8badeaff39d763700352f1a9dec3185de7ee9090a5a9daca9149ef3` (six successful kernels; wait/notify linker failures excluded).
- `threads-w2c2-futex.json`, SHA-256 `e67101ef3bcaf0cad294b3da26ff350cc73c5b4c857c53bf84c2edfd1c2f7686` (successful wait/notify).
- `shared-access-compile.txt`, `threads-current-plan.json`,
  `threads-w2c2-pthreads-plan.json`, and `threads-w2c2-futex-plan.json` retain
  samples, exact commands, artifacts, and batching controls.
- Rebuilt AOT adapter executable SHA-256 `9d18504522f3b22c8e2e161010f5a2a0870931f338e3d465224d20b59e949c84`.


## CPU-gated LSE and proven atomic bounds

Wago `76f7bdac` emits acquire/release LSE RMW and compare-exchange on capable
arm64 CPUs. Darwin uses the kernel LSE capability query; other arm64 hosts use
CPU discovery. `atomic-lse=false` retains exclusive loops. Compiled artifacts
record the LSE requirement and reject loading on an incompatible host. Parallel
compilation joins the requirement from every emitted function; unreachable
atomic instructions do not introduce a requirement.

Wago `a0da41af` proves aligned constant atomic accesses against the declared
memory minimum on both architectures. `atomic-static-bounds` controls this
proof independently. Address addition remains full-width, with overflow checked;
shared memory cannot shrink below its declared minimum. Nonconstant, unaligned,
and out-of-bounds addresses retain runtime checks. Tests include accesses above
2 GiB and at the final eight bytes of a 4 GiB memory. Wago `9d06b863` also borrows
read-only arm64 store operands and removes zero-offset address additions.

The controlled comparison alternates `a0da41af` and `9d06b863` across three
independent launches per revision. All rows have five verified samples, two
warmup batches, and meet the 50 ms target. The current revision's median launch
values below match the prior Wago and w2c2 artifact hashes exactly. Units are ns
per entire 262,144-operation invocation.

| Contract | Prior Wago (`0a24f14a`) | Current Wago (`9d06b863`) | w2c2 | Current / w2c2 |
| --- | ---: | ---: | ---: | ---: |
| atomic-rmw-add | 905,594 | 247,316 | 204,531 | 1.209 |
| atomic-compare-exchange | 889,516 | 290,398 | 254,805 | 1.140 |

These workloads take 72.7% and 67.4% less time respectively than the prior Wago
capture. They remain slower than w2c2. The operand cleanup itself shows
essentially unchanged RMW time and a 1.6% lower CAS median in the alternating
comparison; that small difference is not a statistical speedup claim. An earlier
capture that overlapped race-test compilation is excluded. Other thread kernels
were not rerun for this checkpoint.

Current adapter source identity:
`9d06b863ab05f4e1e29dcf7a63fdcab2b0f5e11a/source-add69b1748ff0a9dbc862b5622cbbf607d0af3c5347426da8f4ac25dfad87822`.

The full native `^TestThreads` group passes on arm64 after the cleanup and on
amd64 after the bounds proof. LSE differential tests cover all RMW operations,
CAS, all widths, extreme values, codec reload, and exclusive-loop fallback.
Race-enabled LSE and defined-shared-memory tests pass locally. Encoder words
match independently assembled Clang output. Backend/shared/optimization tests
pass with the allocation checker on both architectures; the amd64 run includes
the previously missing fixture files copied to the isolated workspace.

Five sequential compile samples after the bounds proof retain 74/73/94
allocations and 19,202/19,058/3,420,950 median bytes per operation for default
scalar/Core 3 scalar/imported workers. Median times are 6,009 ns, 5,961 ns, and
22,257,458 ns. Imported-worker code remains 1,587,220 bytes. Compared with the
preceding checkpoint, the larger fixture takes 0.5% more time; these few fixtures
show no material compile regression and do not establish peak RSS.

Additional ignored evidence under `.validation/wasm3-parity/`:

- `threads-lse-arm64.json`, SHA-256 `dc6f50ef3826dd4d34b9a21a3ae0acc7b45dd1783201b4d089fe3833bf305a0a`.
- `threads-static-bounds-arm64.json`, SHA-256 `ea31c9e61dadf4ba7e3b13858e2da1f8c785d3a540d64a93553f8121bc06d578`.
- `threads-store-borrow-ab-arm64.json`, SHA-256 `a6f6ea15baf054ba084c41836ffc12a70ef9ee2cf935bcd7fd1e71ac35b03af8`.
- `threads-store-borrow-ab-plan.json` retains alternating commands and launch
  order; `threads-lse-compile.txt` retains all compile samples.


## Shared copy and fill

Wago `8d969e51` admits shared `memory.copy` and `memory.fill` and routes their
bounds and addresses through the shared backing directory. Both ranges are
checked before copy writes begin. Constant-length operations initially used the
dynamic bulk path, preserving the ordinary memory mapping's existing unrolling.
Wago `29979007` makes the arm64 unrolled path directory-aware and reuses one
backing base for same-memory copies. Wago `5012eb80` does the same for amd64,
including its scalar and SIMD copy tiers. Dynamic and large operations retain
the existing overlap-safe bulk loops.

Execution tests use an independent Go memmove/fill oracle and check exact memory
contents on success and bounds traps without partial writes. They cover defined
and imported shared memories, original and codec-loaded artifacts, zero-page
memories, zero-length operations at and beyond the boundary, both overlap
directions, low-byte fill values, constant lengths through all scalar/vector
tiers, dynamic lengths, and upper address/count-bit truncation. The allocation
checker thread group and backend tests pass on native arm64 and amd64. Local
race-enabled bulk tests pass. The Wasmtime Core 3 regression and unsupported
proposal gates also pass on both platforms after restoring the missing remote
corpus files; this is still not the full official conformance suite.

wasm.fyi `d21d580` adds `bulk-fill` and `bulk-copy-overlap`. Node/V8 verifies each
fixture's independent arithmetic oracle at every declared size twice on the
same instance. The expanded performance inventory has 94 contracts; cumulative
Wago execution coverage is 86/94, combining captures rather than a full rerun.
The performance fixtures exercise 33-byte unaligned fill and overlapping copy,
with 262,144 inner operations per entire invocation. Each table value is the
median of three launch medians, five verified samples per launch, two warmup
batches, and at least 50 ms per sample. Artifact hashes match across every pair.

| Contract | Initial shared bulk (`8d969e51`) | ARM64 unrolled (`29979007`) | w2c2 | Unrolled / w2c2 |
| --- | ---: | ---: | ---: | ---: |
| bulk-fill | 451,070 ns | 351,481 ns | 183,185 ns | 1.919 |
| bulk-copy-overlap | 745,291 ns | 679,526 ns | 608,568 ns | 1.117 |

The unrolled capture takes 22.1% less time for fill and 8.8% less for overlapping
copy than the initial implementation. Both remain slower than w2c2. These
sequential captures do not establish a statistical speedup or general memory
bandwidth parity. No amd64 performance claim is made from the arm64 captures.

Captured Wago source identities:

- Initial: `8d969e514bf24f6fb33b8ff2d9dfcfbc3d179ed4/source-05900e4560652c3ed26aa866b3f429f9da89e9e52e87133db762d30a601b21d0`.
- Unrolled: `29979007954e7b542f4cc4092c09a7c15b080fce/source-f726f165795268ca8ae1ea16fa5b8129c9eb3a9f15a956f788158dd0bc2fc6ff`.

The w2c2 translator, Clang, SDK, pthread flags, and AOT adapter retain the earlier
comparison's identities and settings. Exact artifact SHA-256 values are
`ac330e6ee7d95873b2eb56d4be994c2cf88eb9a5fc8fbb8c211b25df575871fe`
for fill and `b4d356e5233268ad20244f0bc2d6bad982015e317f42e11c73b86d1d6a4ff084`
for overlapping copy.

Additional ignored evidence under `.validation/wasm3-parity/`:

- `threads-bulk-arm64.json`, SHA-256 `4b723da5e384a264d975b783faa9d875433ae9d4b9a16d8c1627f42d6fad566c`.
- `threads-bulk-unroll-arm64.json`, SHA-256 `2d846152ce99e5e1e43e1e3a7530a011ee40fe2293a1b76178e4f607369f71b6`.
- `threads-bulk-plan.json` and `threads-bulk-unroll-plan.json` retain commands,
  artifacts, oracles, and measurement controls.


Five sequential ordinary compile samples at `5012eb80` retain 74/73/94
allocations and 19,202/19,058/3,420,934 median bytes per operation for the default
scalar/Core 3 scalar/imported-worker fixtures. Median times are 6,036 ns,
6,155 ns, and 23,106,982 ns; larger code size remains 1,587,220 bytes. These
sequential timings are about 0.4%, 3.3%, and 3.8% higher than the preceding
recorded compile capture. An alternating `a0da41af`/`5012eb80` comparison then
runs baseline/current/current/baseline/baseline/current, with 300 ms benchmark
windows and three process launches per revision. Default scalar medians are
6,147/6,187 ns (+0.7%), Core 3 scalar 6,071/6,149 ns (+1.3%), and imported workers
23,649,753/23,360,518 ns (-1.2%). Allocation counts remain unchanged. These
controlled samples show no clear material compile regression for these
fixtures; they do not establish corpus-wide latency or peak RSS.
`threads-bulk-compile.txt` and `threads-bulk-compile-ab.json` retain the complete
samples and alternating order.


## Live shared-memory growth

Wago `e4ea49f7` admits shared `memory.grow` on arm64 and amd64. Nonzero growth
uses the internal synchronous memory helper, which serializes growers using the
backing owner's mutex, enforces the instance's page quota, and commits guarded
pages before publishing the new extent. One aligned atomic byte-size word is
the authoritative extent. Shared scalar/SIMD, atomic, bulk, and size accesses
read that word rather than an instance-local directory snapshot. Constant-zero
growth stays native and uses the same sequentially consistent size query.
Go page queries derive from the authoritative bytes, including exactly 4 GiB.

Call-frame planning now recognizes shared growth as a helper call. The minimal
single-function `memory.grow 0` regression reproduced a return-frame crash before
that classification was added. The broader original growth tests now pass.
Directory initialization reads the atomic extent without extending a host view;
this fixes a guarded-view race found while importing a memory concurrently with
growth. Shared host byte views serialize view extension under the owner's lock.
The existing codec helper requirement admits the growth bridge after reload.

Native tests cover original and codec-loaded products, defined and imported
backing, zero-page memories, zero initialization, success/failure boundaries,
32-bit delta truncation, supplied quotas, the 4 GiB memory32 limit, and
fill/copy/load in the activation that grows memory. Eight competing instances
produce 64 distinct successful previous sizes and 64 failures at the ceiling.
A bounded active observer sees another instance's growth without returning to
Go. Full thread checks pass on both architectures; growth checks pass three
repetitions with both the allocation checker and guarded race detector on
Darwin/arm64 and Linux/amd64. Runtime/backend checks, resource-quota checks, the
local Wasmtime Core 3 corpus, and unsupported-proposal gates pass. This does not
establish full thread or official Core 3 conformance.

wasm.fyi `6bde0f4` adds `memory-size-zero-growth` and a fresh-instance
`memory-grow` lifecycle contract. Node/V8 verifies all sizes on two fresh
instances. Cumulative Wago coverage is now 87/95 long execution contracts,
combining earlier captures with this checkpoint; the new growth lifecycle probe
is separate. All ten thread execution contracts are rerun here with three
independent launches, five verified samples per launch, two warmup batches, and
a 50 ms minimum per sample. The older w2c2 successful captures retain identical
artifact hashes for the nine earlier contracts; zero growth has a new w2c2 pair.
Units below are ns per entire 262,144-operation invocation.

| Contract | Wago (`e4ea49f7`) | w2c2 | Wago / w2c2 |
| --- | ---: | ---: | ---: |
| atomic-compare-exchange | 294,087 | 254,805 | 1.154 |
| atomic-fence | 314,288 | 257 | 1225.044 |
| atomic-load-store | 287,092 | 155,667 | 1.844 |
| atomic-rmw-add | 251,837 | 204,531 | 1.231 |
| bulk-copy-overlap | 735,723 | 608,568 | 1.209 |
| bulk-fill | 386,563 | 183,185 | 2.110 |
| memory-size-zero-growth | 191,639 | 2,033,699 | 0.094 |
| ordinary-load-store | 224,551 | 94,085 | 2.387 |
| simd-load-store | 260,164 | 103,353 | 2.517 |
| wait-mismatch-notify | 12,666,104 | 1,916,274 | 6.610 |

The zero-growth/size contract takes about 10.6 times less time in Wago than
w2c2. It measures size queries, not allocation. Live extent reads increase the
observed cost of several shared-access workloads relative to earlier captures;
for example atomic load/store is now 287,092 ns versus 242,021 ns before growth
support. These sequential captures do not isolate the change statistically.
The gaps remain in the parity objective. Reusing stable shared backing addresses
while retaining live extent reads is a subsequent optimization opportunity.

The fresh-instance 16-page growth probe verifies all samples on both engines:
launch-median medians are 500 ns for Wago and 333 ns for w2c2. Both adapters create
the fresh instance before the call timer. Each sample contains one invocation;
all are below the 50 ms duration target and therefore marked unqualified. These
short lifecycle samples establish execution coverage, not sustained parity.
Nonzero growth's synchronous-helper performance still needs work.

Five sequential compile samples retain 74/73/94 allocations and median allocated
bytes of 19,202/19,058/3,420,947 for default scalar/Core 3 scalar/imported workers.
Median times are 6,126 ns, 6,046 ns, and 22,604,353 ns. Imported-worker code remains
1,587,220 bytes. These fixtures do not show a material compile allocation or
latency regression; broader corpus cost and peak RSS remain unmeasured.

Captured Wago source identity:
`e4ea49f7f02dd5a012306564f3cd5eb9fe3bf830/source-d4759406ea0402984410fbdf796e1a9835ec2a9f137c6b612217b198e2eacc8b`.
Additional ignored evidence under `.validation/wasm3-parity/`:

- `threads-live-size-arm64.json`, SHA-256 `95f66438f4868fd61cc8d7a5a79b9a84357bc2d29d20238c9e9d4c86f9b41bdf`.
- `threads-zero-growth-w2c2-arm64.json`, SHA-256 `ee3634e3e7806434575c4fb35636f2bfdb86f68b0e303c21925f62f78b2e025d`.
- `threads-growth-lifecycle-arm64.json`, SHA-256 `88b0f9e1e83c10fa0cf043f9605a82a7eaf9c8a57a90fcd88a74ed0f149082b7`.
- Corresponding `threads-live-size-plan.json`, `threads-zero-growth-w2c2-plan.json`,
  and `threads-growth-lifecycle-plan.json` retain commands, oracles, and controls.
- `threads-growth-compile.txt` retains compile samples;
  `growth-minimal-frame-before.txt` retains the minimized crash evidence.


## Stable shared backing cache

Wago `454f79c7` reuses stable shared-memory backing addresses on both native
architectures. `shared-memory-base` controls the lowering independently. The
module-wide memory register holds a backing pointer for shared memory and a
size for ordinary memory. Shared bounds checks and size queries continue to
read the live atomic extent. Wrapper entries establish the backing pointer;
helper and external-context continuations reestablish it from the restored
private directory. Small bulk operations can borrow that stable pointer.
Shared registers cannot be leased to ordinary size or adjusted-limit paths.
This adds no field to the compiler function state and preserves the ordinary
memory paths' emitted instructions for the checked scalar/SIMD probes.

Native execution tests compare cache on/off and register/wrapper ABIs with both
defined and imported backing. An eight-live-value caller enters a callee that
grows memory, fills/copies the new range, calls notify, and performs an RMW.
Original and codec-loaded products preserve the exact result and backing bytes.
Unshared scalar/SIMD probes produce byte-identical native code with the option
on/off. Full thread tests and backend/shared/optimization tests pass on arm64
and amd64. Guarded race-enabled cache/growth checks pass on both architectures;
Core 3 regression and unsupported-proposal gates pass locally. These checks
still do not establish full thread or official Core 3 conformance.

The initial standalone capture showed higher atomic-add/CAS medians despite
lower memory-kernel times, so it is not used to infer an atomic regression.
The controlled capture alternates before/current/current/before/before/current,
with three independent process launches per revision and contract. Every row
has five verified samples, two warmup batches, and meets the 50 ms target.
Units are ns per complete 262,144-operation invocation. All w2c2 pairs retain
identical artifact hashes; their earlier qualified launch medians supply the
last column, rather than a newly interleaved peer capture.

| Contract | Before (`e4ea49f7`) | Cached (`454f79c7`) | Observed change | Cached / w2c2 |
| --- | ---: | ---: | ---: | ---: |
| atomic-compare-exchange | 308,103 | 280,839 | -8.8% | 1.102 |
| atomic-fence | 330,545 | 316,152 | -4.4% | 1232.307 |
| atomic-load-store | 293,290 | 281,707 | -3.9% | 1.810 |
| atomic-rmw-add | 261,429 | 253,966 | -2.9% | 1.242 |
| bulk-copy-overlap | 765,695 | 599,782 | -21.7% | 0.986 |
| bulk-fill | 394,309 | 323,466 | -18.0% | 1.766 |
| memory-size-zero-growth | 203,926 | 147,559 | -27.6% | 0.073 |
| ordinary-load-store | 227,805 | 206,989 | -9.1% | 2.200 |
| simd-load-store | 258,115 | 233,856 | -9.4% | 2.263 |
| wait-mismatch-notify | 13,322,531 | 13,332,114 | +0.1% | 6.957 |

The substantial memory-kernel reductions persist in the alternating comparison.
Smaller atomic differences are descriptive medians rather than statistical
speedup claims. Wait/notify is effectively unchanged. Overlapping copy is close
to the recorded w2c2 time on this 33-byte workload; broader bulk parity remains
unproven. The large fence gap and the remaining scalar/SIMD/atomic/notify gaps
remain part of the full objective.

Five sequential compile samples retain 74/73/94 allocations and median bytes of
19,202/19,058/3,420,938 for default scalar/Core 3 scalar/imported workers. Median
times are 6,241 ns, 6,083 ns, and 23,402,958 ns. An alternating 300 ms-window
comparison with three process launches per revision then yields 6,129/6,171 ns
(+0.7%), 6,044/6,054 ns (+0.2%), and 22,717,021/22,796,571 ns (+0.4%) respectively.
The imported-worker fixture retains 1,587,220 code bytes. These measured fixtures
show no material compile regression; corpus-wide compile cost and peak RSS
remain open.

Current adapter source identity:
`454f79c77ac09f1e6493947657654304a79441f3/source-c48f09a43bdb08411a7d535a533f75118e7626e4ca780afa2ba63dd4f654c8ab`.
Additional ignored evidence under `.validation/wasm3-parity/`:

- `threads-cached-base-ab-arm64.json`, SHA-256 `897a242b385d5334517ec4edc674731b9feafe5185f9854965ecd0564d22eb49`.
- `threads-cached-base-arm64.json`, SHA-256 `a1bb83e8bdbe509fb61422b1a6ef81cfd4290dbd041f5eb93ed99789c9329917` (initial standalone capture).
- `threads-cached-base-ab-plan.json` retains alternating commands, artifacts,
  oracles, and measurement controls.
- `threads-cached-base-compile.txt` and `threads-cached-base-compile-ab.json`
  retain compile samples and alternating order.

## Shared data segments (2026-10-12)

Active data initialization now selects shared backing rather than the private
instance control mapping. The memory owner lock serializes guarded host-view
extension with growth. Segments apply in definition order; a final atomic RMW
by zero provides a publication barrier without changing the live extent.
The [threads proposal](https://github.com/WebAssembly/threads/blob/main/proposals/threads/Overview.md#instantiation)
permits non-atomic, coalesced initialization followed by synchronization.

Thread admission now permits data segments and `memory.init`. Existing native
initialization paths already check the live destination extent and private
passive descriptor before copying. `data.drop` changes only that instance's
descriptor, including when instances share the same backing.

`threads_data_test.go` verifies defined/imported memory and original/codec-loaded
products: overlapping active segment order, start visibility, source/destination
bounds with no partial writes, i32 operand truncation, initialization after
growth, zero-length initialization after drop, and drop isolation. Failed active
initialization preserves earlier segments, leaves the failing segment unchanged,
and releases its memory attachment. Eight concurrent instantiation workers and
a separate growth instance retain initialized bytes and the final memory size.

The full `^TestThreads` suite passes with `wago_regalloccheck` on Darwin arm64
and native Linux amd64. Data tests pass three repetitions with `-race` and
`wago_guardpage,wago_regalloccheck` on both hosts. This adds execution coverage;
passive initialization performance and broader thread conformance remain open.

### Passive initialization performance checkpoint

wasm.fyi commit `815077d` adds `threads/passive-data-init`: each operation copies
33 bytes from an immutable 64-byte passive segment, selects the source offset
from the iteration, and reads the final destination byte. The arithmetic oracle
is independent of measured output; V8 verifies the complete performance export
result `39452672`. Pinned wasm-tools 1.260.0 reproduces the source artifact.
Twelve long thread contracts now exist, including the scalar baseline; cumulative
Wago long coverage is 88/96 across checkpoints, not a full rerun of all contracts
on this revision.

Both native backends now unroll constant shared-memory initialization lengths
up to 64 after checking both ranges. Exact ascending chunks avoid extra tail
writes. Zero length retains the checks. Execution/codec tests cover lengths
0,1,2,3,4,7,8,9,15,16,17,31,32,33,63,64,65, source/destination ends, and failures.
ARM64 uses direct NEON accesses and scalar tails. The first scalar indexed
prototype measured slower (543,534 ns launch median versus the original initial
459,682 ns); the final direct-access implementation supersedes it.

The controlled sequence before/current/peer/current/before/peer/before/current/peer
has three independent launches per product, five verified samples and two warmup
batches per launch, all qualified to 50 ms. On M4 Max Darwin arm64, medians per
262,144 operations are 467,268 ns before, 426,593 ns current (-8.7%), and 181,232 ns
w2c2 (current/peer 2.354). The identical measured artifact has SHA-256
`326f066d54be5d3be3c8c9c9bbde22910744975760daeb254c4dac9e3e34798b`.
The peer retains the earlier pthreads/Clang -O3 native profile.

Before adapter:
`ac2e44deee3c5484760b11b0eaee3d1810c8e288/source-583cd68e1f15a8e82e9ef4dcd6c04afec6518a2953d96e945dca4e5c352030e3`.
Current adapter:
`409ed3baa238d398b118dbd79f69e8de7a2a6ff2/source-7120a91fdb73bfb8f05bbca16b668ea5035cfd41722c034c246658550bc8b7c1`.
Ignored raw evidence `threads-shared-data-neon-ab-arm64.json` has SHA-256
`ae0d2ff6dc8fa103fd69038abc5b73f79b1f755312cd4a8be1f726c5a0769149`;
its adjacent plan preserves ordering, commands, artifact and oracle. Initial
and indexed-prototype captures remain in `threads-shared-data-arm64.json` and
`threads-shared-data-unrolled-arm64.json`.

Three sequential 300 ms compile samples give median default/Core 3 scalar times
6,197/6,119 ns, allocation bytes 19,203/19,058 and counts 74/73. Imported workers
p1 give 22,678,711 ns, 3,420,973 bytes and 94 allocations, retaining 1,587,220 native
code bytes. These fixtures remain near earlier measurements; this is not an
alternating compile comparison or peak-RSS assessment. Evidence is
`threads-shared-data-compile.txt`.

Final full thread suites and targeted bulk lowering tests pass on both native
hosts, and three guarded race repetitions of data tests pass on both. The local
Wasmtime Core 3 regression corpus (215 modules, 690 assertions) and unsupported
proposal gates pass. A broad remote bulk filter also selected unrelated staged
GC tests, which fail because `tests/conformance/spec-v3/test/core` is absent on
that host; the targeted AMD64 thread and backend bulk tests pass. Full official
Core 3 conformance and passive-copy parity remain open.

### Register-preserving passive initialization (2026-10-12)

`15daed25` replaces the constant shared-memory initialization path's whole-stack
flush with allocator-owned operands and scratch. Memory32 operands are narrowed
before the live destination and private passive-source checks. Both checks
precede writes. Source bytes cannot alias the destination, so exact ascending
NEON/XMM chunks and scalar tails retain initialization semantics. Dynamic and
ordinary-memory paths retain their prior lowering. No function-state fields or
runtime metadata are added.

The new pressure module retains sixteen computed scalar locals, twenty-four
SIMD locals, and eight prior memory loads across a 33-byte initialization that
overwrites some of those loads' addresses. It verifies prior-load ordering,
exact initialized bytes, and all retained values, with cache on/off and both
register/wrapper calling conventions. Original and codec-loaded products pass
on Darwin arm64 and Linux amd64. Full thread suites and backend bulk tests pass
on both architectures. Three guarded race repetitions of data tests pass on
both. Local Core 3 regression (215 modules, 690 assertions) and unsupported
proposal gates pass. These remain targeted gates rather than full conformance.

An alternating before/current/peer/current/before/peer/before/current/peer run
retains the same `326f066d...34798b` artifact and arithmetic oracle. Each of the
three launches per product has five verified samples, two warmup batches, and
passes the 50 ms duration target. M4 Max medians per 262,144 operations are
429,827 ns before, 298,301 ns current (-30.6%), and 190,241 ns w2c2. Current/peer
is 1.568; passive-copy parity remains open. Peer variation relative to the prior
checkpoint is why this comparison uses its newly interleaved peer, not an older
peer timing. AMD64 execution is validated, but this capture measures ARM64.

Before adapter:
`409ed3baa238d398b118dbd79f69e8de7a2a6ff2/source-7120a91fdb73bfb8f05bbca16b668ea5035cfd41722c034c246658550bc8b7c1`.
Current adapter:
`15daed2519f9cae6826937d776a4badb08502dfd/source-cad45205e180313b7c8a00f1bc361e3949bb01a5ef8dcf3ad567b69b765a7b2f`.
Ignored evidence `threads-shared-data-register-ab-arm64.json` has SHA-256
`9ed3470ffcfa52917322486e3e0e31a1aebd408e07013f0b45f9f306166e3b6d`;
the adjacent plan retains adapter commands and ordering.

Three sequential 300 ms compile samples yield median default/Core 3 scalar
6,019/5,926 ns with 19,202/19,058 bytes and 74/73 allocations. Imported workers
p1 yield 21,901,818 ns, 3,420,965 bytes and 94 allocations, with unchanged
1,587,220 native code bytes. This is near the prior allocation footprint; it
is not a controlled compile speedup or a corpus-wide peak-RSS result. Evidence
is `threads-shared-data-register-compile.txt`.

## Host and cross-instance function imports

`9f2782d7` admits slot, typed numeric, HostCall, Caller, and owned HostFuncRef
imports with shared memory. Host byte views use the live shared backing. Tests
cover defined/imported memory, artifact reload, register/wrapper ABIs, callback
growth, independent concurrent roots, and owned binding lifetime.

Native function imports use the global execution lease when their callee control
can be shared. Private linear-memory roots retain their local lease. Publishing
a callable control from a callback migrates its continuation to the global
lease. Foreign dispatch preserves the producer's saved registers and carries
the public root's identity and callback scope explicitly, without an ambient
binding at the producer's shared control address.

Blocking waits release the global lease so another root can notify them. They
inherit the public caller's cancellation context. GC waits publish complete
native frames and exact liveness masks before releasing collector leases.
Concurrent roots can resume out of suspension order; parked activation slots
reuse holes instead of requiring stack order. Tests retain GC structs across
collection, wakeups, and activation-slot reuse, including a preceding inlined
zero-timeout wait.

A follow-up deterministic GC-result test exposed a separate ownership problem:
two callbacks sharing one producer control address attached the first result
to the second parked activation. Result translation now carries the exact
activation token through dispatch. The test checks that the first completed
callback leaves no result root on the still-parked second callback, collects,
and then verifies the second result.

Three repetitions of guarded race validation pass on Darwin/ARM64 and native
Linux/AMD64 for `^TestThreads|^TestPluginGCHost|^TestHostReferenceTranslation|`
`^TestCrossInstance|^TestGCFrame` (15.706 s and 18.608 s). The preceding
host-import commit also passes the local Core 3 regression corpus; this does
not establish full official conformance or host-import peer performance.

An alternating before/current/current/before/before/current ARM64 capture at
`9f2782d7` compares against `15daed25`. Every launch has five verified samples,
two warmup batches, and a qualified 50 ms target. These are median launch times
for 262,144 work units:

| Existing thread kernel | Before ns | Current ns | Change |
| --- | ---: | ---: | ---: |
| Ordinary load/store | 208,076 | 205,237 | -1.36% |
| SIMD load/store | 233,434 | 228,025 | -2.32% |
| Bulk fill | 321,922 | 316,952 | -1.54% |
| Overlapping bulk copy | 590,386 | 584,979 | -0.92% |
| Passive data initialization | 294,131 | 294,125 | -0.00% |
| Memory size / zero growth | 137,950 | 138,148 | +0.14% |
| Atomic load/store | 271,723 | 272,969 | +0.46% |
| Atomic RMW add | 243,623 | 243,141 | -0.20% |
| Atomic compare exchange | 277,631 | 276,270 | -0.49% |
| Wait mismatch / notify | 13,321,333 | 13,234,542 | -0.65% |
| Atomic fence | 310,928 | 310,267 | -0.21% |

No peer was retimed in this capture. Current adapter identity is
`9f2782d7eff967e98378a54951186b491424038a/source-425fd6a2583c9ba507c6d8da2831e6915de198b69bb402bcadbd498bef7768a4`.
Ignored evidence `threads-host-import-regression-ab-arm64.json` has SHA-256
`bf847375d5544b4db5cd422c77be8c08ea6b83ffcee9ecf780731e7a1e692d3d`.
The adjacent plan preserves ordering and exact artifact/adapter commands.

Alternating 300 ms compile captures at the same two revisions yield scalar
default 6,227/6,276 ns (+0.8%), Core 3 6,141/6,144 ns (+0.05%), and imported
workers p1 23,654,821/23,490,631 ns (-0.7%). Scalar allocations remain
19,202/19,058 bytes and 74/73 counts; worker allocations remain approximately
3,420,931 bytes and 94 counts, with 1,587,220 native code bytes. A preceding
sequential scalar capture was slower, motivating this alternating comparison.
These fixtures show no material compile regression; peak RSS and corpus-wide
compile costs remain unmeasured. Evidence is `threads-host-import-compile-ab.json`
and `threads-host-import-scalar-compile-ab.json`. The exact-activation GC fix
is validated separately and is not included in these timing captures.

## Shared-memory host callback performance contract

wasm.fyi `be6999b` adds `threads/host-identity` at three correctness sizes and
one sustained performance size. It stores the loop index into shared memory,
loads it, calls the declared `wasmbench.identity(i32)->i32` import, and adds the
returned value. The independent wrapping oracle is 536,739,840 for 262,144
work units. `threads-identity-v1` declares its shared-memory and callback setup;
wasm-bench `3525ee7` binds the existing owned typed Go HostFuncRef and enables
threads with explicit bounds. V8 binds its JS identity callback. The isolated
w2c2 adapter admits the profile and retains its existing noinline/noipa typed
C callback with an opaque compiler memory barrier.

The pinned correctness artifact rebuilds byte-for-byte; the sustained artifact
SHA-256 is `c9310c605dd85f4832d474a2c4e14aee30e2864c048b4c40327bbcfe8321eb53`.
All seven launches verify the oracle and qualify for five 50 ms samples with
two warmup batches. The three alternating Wago/peer launches yield medians of
63,726,416 ns and 202,795 ns, a 314.2 ratio. One V8 oracle launch yields
742,097 ns. These compare Go, C, and JS callback bindings, respectively; they
expose a host-transition gap and do not isolate shared-memory instruction cost.
Callback parity remains open. The faster bounded numeric callback route does
not currently admit this owned HostFuncRef namespace.

Wago adapter identity is
`6e55e4a0ff6f7c8300adfcc5ba490f0b36dacb36/source-077e70001fa4f70e54bcf72c54ec9562ed15245f2c8f10a555aa31bf32c56a89`.
Raw ignored evidence `threads-host-identity-arm64.json` has SHA-256
`69817170c7aa1cc60d7f59d46e93d21064149d405eb6ddd5b57774ed723df700`;
the adjacent plan preserves exact ordering and commands. Rebuilt isolated peer
adapter SHA-256 is `3fdbd71e164f6325d0344923c9962ac3977ff52f9db309c4b544ec0aa869475e`.
The incremental peer profile patch is retained at
`docs/benchmarks/wasm3-2026-10-12/aot-thread-host-identity.patch`.

The performance harness tests pass (three checks, one unrelated allocation
fixture skip). The broader curation test has four failures from absent curated
core fixtures (`br-table-state-machine` and insufficient shared-module/diversity
inventory); its source/artifact/oracle consistency checks pass. That suite is
not reported clean. AMD64 host-import execution remains validated, but these
callback timing captures are ARM64 only.

## Tables, element segments, and exception tags

`4579ec6a` admits defined/imported tables and active/passive element segments
with shared memory. Its test matrix covers cache settings, both call ABIs,
artifact reload, indirect calls, passive initialization/drop, growth/copy,
traps, and producer/caller memory-context isolation. ARM64 passes; native AMD64
initially returned 15 instead of 17 for a foreign register-ABI table call,
reading the consumer's shared memory instead of the producer's. This reproduced
with cache enabled and disabled. `392128c5` makes exported-table escape detection
architecture-independent: producer descriptors publish wrappers that switch
instance context instead of private register entries that inherit the caller's.
The same matrix now passes on AMD64.

`f3308664` admits local/imported exception tags. Tests exercise shared-memory
growth before throwing, local and foreign catches, uncaught exception recovery,
payload preservation, caller memory-context restoration, artifact reload,
cache settings, and both call ABIs. Shared memory64, multiple memories, and
mutable global imports remain restricted; broader reference/table concurrency
and full official threads conformance remain open.

Final three-repeat guarded race checks pass on ARM64 (13.912 s, threads,
cross-instance, GC frames) and AMD64 (19.711 s, those checks plus funcref,
dynamic reference, and layout tests). Initially the broader AMD64 selection
also lacked two dynamic-funcref fixtures; synchronizing the pinned local
fixtures resolved that blocker. It exposed a 792-byte Compiled layout against
the existing 784-byte budget. Both architectures had the same layout: the ARM
LSE requirement flag added padding at the tail. Moving it beside the other
flags restores 784 bytes without changing field-based serialization. The
existing AMD64 footprint test now passes. Guarded race Core 3 regression checks
also pass at the repaired table/exception checkpoint on both architectures:
215 modules and 690 assertions (1.559 s ARM64, 1.617 s AMD64 with race exit delay
disabled). These are regression fixtures, not full official conformance.

wasm.fyi `3b19519` adds three correctness sizes and one sustained
`threads/table-indirect-call` contract. It writes/reads shared memory, selects
between two table functions, and reads memory size after returning. The pinned
artifact rebuilds byte-for-byte. The alternating ARM64 capture at `4579ec6a`
has three launches per runtime, five verified 50 ms samples and two warmup
batches each. Median Wago/peer times are 467,492/1,081,869 ns for 262,144 work
units, a 0.432 ratio (Wago 2.31 times faster). A single V8 oracle launch yields
179,368 ns. This capture predates the AMD64-only behavior correction and
exception admission; it measures the ARM64 table workload only.

Adapter identity is
`4579ec6a0056888cb55e734a66025a8d539663a1/source-fc62d886f6e41fb406058c3d2966efd69403dd5f1ecba976a4449ae3ba6a775d`.
Performance artifact SHA-256 is
`27363fe83509918397526b8291e49943f9cdf9f466409e84244de345f6d2b279`.
Ignored evidence `threads-table-indirect-arm64.json` has SHA-256
`475a1e345737a71125ea3cb2ec8f125ee70192bd594cc997d6d4655c42123482`;
the adjacent plan retains exact commands and ordering. The threads performance
family now contains 14 long contracts (including its scalar baseline) and one
separate fresh-instance growth probe. No full-family retiming is claimed here.

## Mutable-global imports

Mutable-global imports are admitted with shared memory. Their native entry uses
the shared execution lease also used by the global's host accessors; separate
memory backing does not imply private imported-global storage. Immutable
imports retain their existing policy. Tests combine imported shared memory and
atomic access with a mutable numeric global, and run eight concurrent instances
performing 10,000 updates each against one shared global. They verify the exact
counter, host mutation handoff, re-export identity, and rejection of closing a
global while importers remain live, with both ABIs, cache settings, and artifact
reload. Three-repeat guarded race checks pass on ARM64 (14.083 s) and native
AMD64 (17.078 s), including all threads, cross-instance calls, GC frames, and
the AMD64 layout checks. This adds numeric-global execution coverage; no
mutable-global performance contract or reference-global composition claim is
made here. Shared memory64 and multiple-memory thread combinations remain open.

## Preserve owned typed callbacks

`c6fb630b` stores the original immutable callback binding in HostFuncRef instead
of wrapping it immediately in a slot callback. HostFuncRef remains 128 bytes.
Attached pure typed numeric imports use the ordinary per-import scalar portal;
their owner/store/signature validation and importer lifetime attachments remain
in place. Scoped callbacks, gates, references, GC bridges, and materialized
funcrefs retain their ownership-aware paths. Owned reference dispatch preserves
the exact indexed signature and callback scope. An existing indexed-scalar
signature test caught over-broad routing during development and now passes.

The focused thread test uses a host-owned shared memory, grows it through another
instance inside an owned typed callback, publishes the caller's function control
inside that callback, and verifies native continuation after the private-to-global
lease migration. It repeats with both ABIs, both cache settings, and artifact
reload, and checks that closing the owner while its importer lives fails.
Three-repeat guarded race checks pass on ARM64 (15.573 s) and native AMD64
(19.056 s) for threads, plugin GC host imports, cross-instance calls, funcrefs,
GC frames, exact signatures, reference translation, and the AMD64 layout check.

The first controlled ARM64 identity capture against `6e55e4a0` yields
58,522,125/567,072/182,133 ns for before/current/peer (three interleaved launches,
five qualified 50 ms samples, two warmup batches). This is a 103.2-times semantic
kernel improvement and a 3.114 peer ratio. It exposes the existing ARM64
specialization for verified pure Go i32 leaf arithmetic: instructions are
validated and copied into a native trampoline, with no Go transition remaining
for the identity operation. It does not establish a generic Go callback speedup.
Raw ignored evidence `threads-owned-typed-ab-arm64.json` has SHA-256
`799fe05519995c844c3e231b4fddfcd94c2b7c34f9220207f2c08b740820c466`.

wasm.fyi `a83302d` and wasm-bench `14994c6` add an explicit opaque callback
profile beside that pure identity contract. The Wasm bytes and arithmetic oracle
are identical, but the Go callback calls a noinline helper, so the native leaf
proof rejects it and the call enters Go. The peer retains its existing noinline
C identity callback with an opaque compiler memory barrier. The adapter's
description states the distinction. The new artifact rebuilds byte-for-byte;
performance-harness tests pass (three checks and one unrelated allocation-fixture
skip). V8's pure identity capture verifies the same artifact and independent
oracle; no opaque V8 execution-mode claim is made.

A second alternating before/current/peer capture compares `859351a8` with
`c6fb630b`, using the updated adapter source for both. Each kernel has three
launches per revision/runtime, five verified qualified 50 ms samples and two
warmup batches:

| Callback mode, 262,144 work units | Before ns | Current ns | Peer ns | Change | Current / peer |
| --- | ---: | ---: | ---: | ---: | ---: |
| Pure identity, native arithmetic eligible | 57,013,792 | 574,743 | 178,689 | -98.99% | 3.216 |
| Opaque identity, Go transition retained | 57,974,583 | 13,810,073 | 180,695 | -76.18% | 76.427 |

Opaque callbacks improve 4.20 times; Go/C callback transition parity remains
open. The additional noinline Go helper is part of the opaque binding's measured
cost. These captures are ARM64; native AMD64 execution is validated separately.
Both sustained artifacts have SHA-256
`c9310c605dd85f4832d474a2c4e14aee30e2864c048b4c40327bbcfe8321eb53`.
Current adapter identity is
`c6fb630b4e8945c616fb61b357cc128714b3d979/source-746256a88ca127347ed3eb94ccd60ee70bb41c386f7c4042a4e1a595f6413416`.
Before identity is
`859351a82a13389d6315de7c3247e9ccb81df307/source-7080eb339a04b1a3d7848c301e79113deeded6a52782f1dc6151645ec6a8400b`.
Ignored evidence `threads-owned-opaque-ab-arm64.json` has SHA-256
`1f664ce5f76b2667f5a4646d37fea3ec073152330d833e934ac5e088e1bcd9b0`;
the adjacent plan retains commands and ordering. Updated peer adapter SHA-256
is `47e87191034e974e1b67763b4e6fac406946807d04b9e5141eca3d6c2a6b3488`,
with incremental profile patch at
`docs/benchmarks/wasm3-2026-10-12/aot-opaque-thread-host.patch`.

An alternating six-launch 300 ms compile capture against `859351a8` yields
default scalar 6,529/6,175 ns and Core 3 scalar 6,023/5,964 ns. Allocations are
unchanged at 19,202/19,058 bytes and 74/73 counts. Imported workers p1 yield
22,965,283/23,550,324 ns (+2.55%), median 3,421,317/3,421,321 bytes, 95 allocations
in both revisions, and identical 1,587,220 native code bytes. The preceding
sequential capture also had 95 allocations, so comparison with the older
94-allocation checkpoint does not attribute that difference to this change.
Compiled remains 784 bytes. These samples protect this fixture's allocation
footprint; they do not establish corpus-wide compile latency or peak RSS.
Evidence is `threads-owned-typed-compile-ab.json` and the two preceding
`threads-owned-typed-{scalar,worker}-compile.txt` captures.


## Shared memory64 composition

Wago `f204d89a` admits one shared memory64 with explicit bounds checks on native
ARM64 and AMD64. Atomic memargs retain their full u64 offsets; helper calls carry
i64 addresses, and overflow cannot wrap wait/notify into the backing. Shared
size/grow return i64 values, reject oversized deltas before narrowing, and return
all 64 failure bits. Literal high-bit grow deltas do not take the zero-growth
shortcut. This follows the address-type rules in the
[memory64 proposal](https://github.com/WebAssembly/memory64/blob/main/proposals/memory64/Overview.md).
The existing finite 65,535-page implementation reservation remains in force;
this checkpoint does not provide allocations above 4 GiB.

Guarded race tests pass on Apple M4 Max Darwin/ARM64 and Ryzen 7800X3D
Linux/AMD64. Coverage includes imported/defined memory, both ABIs, stable-base
cache modes, codec reloads, atomic load/store/RMW/CAS, full-width bounds and
memarg arithmetic, growth visibility, blocking wait32/wait64, notification and
cancellation. All 23 scalar and 22 SIMD access forms match the unshared
memory64 path for results, memory bytes and trap codes. Bulk copy/fill cover
full-width bounds, overlapping copies, zero-length boundary cases and no
mutation on a bounds trap. Existing thread tests and the local Core 3 regression
corpus also pass; AMD64's staged memory64 tests pass. These are targeted and
local regression checks, not full official conformance.

wasm.fyi adds two source-built shared-memory64 contracts with both `threads`
and `memory64` recorded in their feature lists. Both artifacts rebuild byte for
byte using wasm-tools 1.260.0. Wago and V8 independently verify the arithmetic
oracles. Three Wago launches each retain five qualified 50 ms samples and two
warmup batches, yielding medians of 281,707 ns for dynamic atomic load/store and
13,513,156 ns for wait mismatch plus notify (262,144 work units per invocation).
The pinned w2c2 translator rejects both artifacts with `invalid limit kind`;
there is no same-feature peer timing or parity claim for shared memory64.
The raw harness records that translation failure, rather than a measured ratio.
Adapter identity is
`f204d89a40be91132968e091c1caf0b0576b5b39/source-b581ad24e9432f93a392d7b754041644eba3ec2cdbfca59a6a8f7a0fde256fd0`.
Ignored evidence `threads-memory64-arm64.json` has SHA-256
`4f7ff3b98dd9b013a560e9e8c8d14268e59df88ab51fd991b57c66c7cc4357b6`.

An alternating six-launch 300 ms compile comparison against `199209f8` yields
scalar 6,351/6,429 ns (+1.23%), Core 3 scalar 6,381/6,330 ns (-0.80%), and
imported workers p1 23,732,112/23,928,978 ns (+0.83%). Scalar allocation bytes and
counts remain 19,202/74 and 19,058/73; worker code remains 1,587,220 bytes.
Worker allocations fluctuate between 94 and 95 in both revisions. These fixtures
show no material compile cost increase; corpus-wide latency and peak RSS remain
unmeasured. Evidence `threads-memory64-compile-ab.json` has SHA-256
`d25f0618c4c07670e609ed154307c55d6b8febebc391c0b6ac9b9715c64744e2`.

Unshared memory64 with atomic instructions, multiple memories with threads,
shared-memory64 segment composition, broader conformance and overall
performance parity remain open. The missing detached remote session was
recreated and verified; SSH uses ControlPersist=24h.


## Memory64 segments and unshared atomics

Wago `9a4c2d00` repairs constant-length shared-memory64 `memory.init`: its
optimized path previously narrowed the destination to i32, accepting a high
address and writing to low backing. Both backends now retain the i64 destination.
Tests cover active segment ordering and start visibility, passive segment
initialization, high destinations and overflow, i32 source/count carriers,
growth, data.drop, imported/defined memories, both ABIs and codec reloads.
Guarded race thread/Core 3 regression checks pass on both native architectures.

wasm.fyi `06d47f1` adds the shared-memory64 passive-data contract; its artifact
rebuilds byte for byte. Three Wago launches with five qualified 50 ms samples
and two warmup batches yield 329,035 ns per 262,144 operations. V8 independently
verifies the oracle. The current w2c2 translator again rejects the limits; no
peer ratio is established. Artifact SHA-256 is
`d7aef9ac0dc4ca95c5769b6642e1bee3c01416d966cf155cadc095775358a637`.
Adapter identity is
`9a4c2d0015fe5c3cde7a3833b4145d101aebd82a/source-8d85b07a549ba402da7b735d0f1e82a4a57169b2094461c5107bca7a45996e71`.
Ignored evidence `threads-memory64-data-arm64.json` has SHA-256
`1fcc993adc088e2582bf32fe7c3b5716fe563aec236b30655bf24e1729c37602`.
Performance/protocol harness checks pass (five checks; one unrelated allocation
fixture remains skipped). The threads family now contains 18 sustained
contracts plus one separate lifecycle probe; this is cumulative coverage,
not a full same-revision retiming.

The following unshared-memory64 checkpoint uses the existing canonical backing
and full-width bounds lowering for atomic accesses. Simply removing admission
previously crashed: an unshared instance has no shared directory by default.
Tests cover imported/defined memories, quota/no-quota policies, both ABIs,
codec reloads, load/store/RMW/CAS, high addresses, growth, notify and the required
unshared-memory wait trap. Guarded race thread/Core 3 regression checks pass on
ARM64 and AMD64, including staged memory64 on AMD64. This checkpoint does not
claim new peer timings. Multiple memories with threads, wider proposal
combinations, full official conformance and overall parity remain open.


## Unshared memory32 atomic backing and wider compile costs

Wago `2fd39f1a` repairs dynamic unshared-memory32 atomic addressing. The shared
directory is absent on this path; canonical backing and its memory32 bounds
calculation are now used instead. The first wasm.fyi RMW capture then exposed
the same assumption in the constant-address shortcut. Wago `d793333f` retains
its minimum-size/alignment proof but selects canonical backing for unshared
memory. A focused constant-address test reproduced the native fault before the
fix. The original failed capture remains at `threads-unshared-arm64.json`.

Both ABIs and original/codec products pass store, add, subtract, bitwise
and/or/xor, exchange and compare-exchange checks for i32/i64 values, with dynamic
and constant addresses. Dynamic checks cover upper-i32 truncation, alignment,
offset/bounds failures and no memory mutation on traps. Guarded race thread and
Core 3 regression checks pass on both native architectures; the final dynamic
trap cases also pass three repeated guarded race runs on both.

wasm.fyi `67613c0` adds unshared memory32 load/store and RMW contracts. Both
artifacts reproduce byte for byte, and V8 independently verifies the arithmetic
oracles. Corrected ARM64 captures use three Wago and three w2c2 launches, five
verified qualified 50 ms samples per launch and two warmup batches:

| Unshared contract, 262,144 work units | Wago ns | w2c2 ns | Wago / peer |
| --- | ---: | ---: | ---: |
| Atomic load/store | 183,163 | 165,533 | 1.107 |
| Atomic RMW add | 260,352 | 221,488 | 1.175 |

There are no before-speedup claims for the broken path. Adapter identity is
`d793333fde9cfb12c7077dc76bc6ef696f1cc788/source-b2e8ef80d9dc94ecbaaa64320ddea66fde964827e4fd3e7743738f948328517a`.
Artifacts have SHA-256
`08a0477d1e993b8ab4715487727a5075bc4df11a76254a3d7875f42304915b5b`
and `d06bc621ce43a7caa472003a016a1614cce82e88a95df3a5347f2fdd0e9e3fec`.
Evidence `threads-unshared-static-arm64.json` has SHA-256
`883d3e770f7271bc9452536a4d8a6b1b241f2cb47ab42a96747852a6bb522015`.
The thread family now has 20 sustained contracts and one separate growth probe;
this is cumulative coverage, not a full same-revision retiming.

A wider compile capture compares `199209f8` with `d793333f` on Apple M4 Max,
Darwin/ARM64, Go 1.27.1, GOMAXPROCS=1. The input is all 218 module and
module_definition files in the local Wasmtime Core 3 regression JSON commands
(excluding invalid-module assertions). Each fresh process warms one complete
pass, runs Go GC, then measures 100 complete passes: 21,800 compiles, including
close and GC costs, with decode, validation and native code generation enabled.
Input reading and warmup are outside the timed/allocation interval. There is no
instantiation or guest execution in this capture. Six launches alternate
before/current/current/before/before/current; no builds or other tests overlap.

| Median per process | Before | Current |
| --- | ---: | ---: |
| Compile interval | 398,313,084 ns | 392,916,792 ns |
| Go allocated bytes | 777,964,064 | 777,964,208 |
| Go allocation count | 2,649,935 | 2,649,935 |
| Generated code bytes over 100 passes | 17,900,800 | 17,900,800 |
| Maximum resident set size | 14,123,008 bytes | 14,106,624 bytes |
| macOS peak memory footprint | 9,404,968 bytes | 9,388,560 bytes |

Latency differs by -1.35%, with effectively identical allocation and process
memory footprints. Resource peaks come from `/usr/bin/time -l` over the complete
process, including input buffers, warmup and measurement. They are not an
isolated live compiler-heap peak or memory-instantiation cost. These modules use
CoreFeaturesV3; the capture does not measure every new thread combination.
The retained runner is
`docs/benchmarks/wasm3-2026-10-12/compile-corpus.go`.
Ignored input file/hash inventory `core3-compile-inputs.json` has SHA-256
`e3e38004a705b1c7fdb4aed594458dac7756734356ce0c2996078638719b6091`.
Raw `threads-memory64-core3-compile-ab.json` has SHA-256
`b9c8e2e3f1059ef21e3c3edd960ef082c74b0ad5c129fa3b8d9dc3c5dd5b03e0`.

The SSH control master and detached `wago-wasm3-parity` session were verified
live after these captures. Multiple memories with threads, broader composition,
full official conformance and overall performance parity remain open.


## Full pinned official Core 3 suite restored and validated

The pinned `tests/conformance/spec-v3` submodule is now initialized locally and
in the dedicated AMD64 workspace at
`9d36019973201a19f9c9ebb0f10828b2fe2374aa`. Both native runs use the checksummed
WABT 1.0.41 release asset from `scripts/bootstrap-wabt.sh` and the official
reference interpreter built by `scripts/bootstrap-spec-interpreter.sh`, with
version/revision and build provenance verified. ARM64's interpreter uses
OCaml 5.5.1, Dune 3.24.2 and Menhir 20260209; AMD64's uses OCaml 4.14.1,
Dune 3.14.0 and Menhir 20231231. AMD64 tools and standard libraries were extracted
under the dedicated workspace cache, requiring no system installation.

At Wago `c88d2457` (production source identical to `d793333f`),
`TestSpecSuiteExec` with `wago_regalloccheck` passes on Apple M4 Max
Darwin/ARM64 Go 1.27.1 and Ryzen 7800X3D Linux/AMD64 Go 1.22. Both runs cover all
258 pinned files with 2,226 modules and 58,238 assertions passed, zero failures,
skips or reported execution gaps. Exactly 28 files use the official interpreter
fallback when WABT cannot parse the text. The current count is 200 assertions
above the historical committed baseline; the current captures retain per-file
counts and are not substituted into that historical record. The checksummed
release assets and both independently built interpreters agree on the current
counts. This result does not establish threads proposal conformance or overall
performance parity.

Ignored logs and their SHA-256 values:

- `official-core3-pinned-arm64.txt`:
  `3f215111d2096a47142cc80e822d7c7aaf63eede1e64b285ba75e59133134f33`
- `official-core3-pinned-amd64.txt`:
  `89b0ccb0cb81eb1fa79377b124b9da86361af3b17419d182f6d4aad8d293d11d`

The AMD64 suite ran in the detached `core3` window of
`wago-wasm3-parity`; its status file records exit 0. Both source checkouts and
native tools remain available for subsequent validation. Full feature and
performance parity work remains open.


## Pinned threads proposal conformance with actual concurrent workers

Wago `2f7d37ea` adds `TestThreadsOfficialAtomicSuite`, pinned to
WebAssembly/threads `cc535ada1aa21cfaa3cabf3ac73b89acef78a0a0`. Its original
atomic script passes all 5 modules and 274 assertions on native ARM64 and AMD64.
The first run found a harness vocabulary gap: 49 correct atomic-alignment traps
were rejected because `unaligned atomic` was not mapped. The runner now maps
that exact text to `TrapAtomicUnaligned`, with a negative bounds-code check.
No production trap classification was relaxed.

Wago `72fe8dd3` extends the binary-script converter and spectest runner for
shared-module references and actual parallel workers. Module filenames retain
one counter across nested scripts. Worker Runtimes keep their own module
namespaces; only explicitly selected parent module instances are shared.
Workers inherit bounded invocation contexts, and parents join workers before
closing provider instances. The existing serialized Wasmtime fixture path is
preserved for its separate independent-store scripts.

All 12 concurrent files in the pinned proposal's `test/core/threads` directory
pass: simple, thread, nested, deeply_nested, unlinkable, wait_notify, LB,
LB_atomic, MP, MP_atomic, SB and SB_atomic. They include ordinary/shared atomic
accesses, imported shared backing, nested workers, namespace isolation, blocking
wait/notify and allowed memory-ordering outcomes. The pinned threads reference
interpreter (`wasm 2.0.1`) converts text to binary scripts; WABT 1.0.41 cannot
parse the concurrent WAST commands. The atomic-only file uses WABT 1.0.41.

The concurrent files account for 46 modules and 38 assertions; together with
atomic.wast, all 13 files pass 51 modules and 312 assertions, with zero failures,
skips or reported gaps. Both native architectures pass guarded race execution.
Three repeated guarded race runs of the official suite plus the local Core 3
regression corpus also pass on each architecture. A complete pinned Core 3
rerun after the harness change still passes 2,226 modules and 58,238 assertions
on ARM64. These are correctness runs; the finite litmus scripts do not prove
all possible schedules or establish worker performance parity.

Retained ignored evidence SHA-256:

- `official-threads-atomic-arm64.txt`:
  `1cce92166a3d23b99df3f6b1bea71e9ef8c660f621c3cbd96b112b502a88d963`
- `official-threads-atomic-amd64.txt`:
  `878cae5e9c843aae1e63cb9c0c26d9dc7c1f22abeb31170195a15139581e2f78`
- `official-threads-repeated-arm64.txt`:
  `bfdf3e34ed5a29a78a5361438a6250d2dc35ca6b8d5dc8edd66dac31f6b2bc48`
- `official-threads-repeated-amd64.txt`:
  `4a2930d243020623c3cf501ff0a181fe2259cc76930abe72840b75741f14baf6`
- `official-core3-thread-harness-arm64.txt`:
  `60b5e3b462cb24d74f7a5e2575300c55d1a90291a1f2087cf180cad2cad61770`

Current remaining work includes multiple memories with threads, broader
proposal combinations, phase 5 embedding behavior and feature-by-feature
performance parity. Passing the pinned suites does not remove those limitations.


## Defined and imported unshared secondary memories

`acecc24d` permits defined unshared secondary memories beside memory zero.
`141e4622` permits imported unshared secondary backing and retains the common
execution lease needed for ordinary accesses that alias another instance or
host accessor. Eight importers performing 1,000 non-atomic read/modify/write
updates each must produce exactly 8,000. Mixed address-width growth tests verify
that provider and consumer observe the same secondary extent and contents,
while primary size remains independent. Both call ABIs and codec products pass.
A native Linux mapping-registry test repeats a trapping start eight times and
checks that no backing/control mappings remain registered after any failure.
Targeted guarded race tests pass on both native architectures.

Earlier defined-secondary tests cover independently growing both memories,
explicit memory-zero memargs, ordinary secondary loads/stores, cross-memory
copies, high memory64 addresses, bounds-before-write, ownership, and quota/start
rollback. Their matrix includes imported shared primary backing, cache settings,
both call ABIs and codec products.

wasm.fyi `926655b` adds two sustained contracts; the median across three fresh
Wago process launches on Apple M4 Max / Darwin ARM64 / Go 1.27.1 is:

| Contract, 262,144 work units | Wago ns |
| --- | ---: |
| Private secondary ordinary load/store with primary atomic access | 233,335 |
| Shared-primary to private-secondary copy | 251,445 |

This capture uses Wago `acecc24d`, source fingerprint
`cb831c7ac718b50de27215075b1d59ffcb3b2100ca061dce439570b6d38e68b4`.
Each launch has two warmup batches and five independently verified timed batches
of at least 50 ms. A separate V8 launch verifies each arithmetic oracle.
Peer `f303ed27` traps on the ordinary indexed memarg and rejects memory.copy
with a nonzero memory index. Its ordinary memarg reader consumes the encoded
memory index as an offset, leaving an unreachable opcode in generated C.
No matching peer timing exists. Raw evidence `threads-private-secondary-arm64.json`
has SHA-256 `67b807c76a81a4f6913d02178687fc12db25b1fa007e86f83ea347c833b2a614`.

## Atomic instructions on an unshared secondary memory

`ed977b84` resolves the atomic memory index before selecting its memarg offset
width. Both native backends route numeric atomics through the indexed backing;
the existing memory-zero static proof and shared-base fast paths are retained.
Wait/notify pass the selected index to the synchronous helper, which uses that
memory's address width and backing. Blocking-wait classification accepts the
additional index argument. Waiting on unshared secondary backing still produces
the required expected-shared-memory trap; notifying it returns zero.

All 63 numeric opcodes execute across 32 combinations of primary/secondary
address width, ownership, constant/dynamic addresses and call ABI, with original
and codec-loaded artifacts. Tests check old RMW/CAS values, narrow writes,
unaligned traps, full-width high addresses/offsets, bounds-before-write and an
unchanged primary sentinel. Restoring the old decoder reproduces rejection of
memory index 1. Full thread tests plus the local Core 3 regression corpus pass
with guarded race checking on both architectures; staged memory64 passes on
AMD64. The expanded selected-memory matrix also passes on both architectures.

wasm.fyi `8d65088` adds two more sustained contracts. Three fresh Wago launches
with the same verified 50-ms/five-sample protocol give these medians:

| Contract, 262,144 work units | Wago ns |
| --- | ---: |
| Private secondary atomic load/store | 240,122 |
| Private secondary atomic RMW add | 265,913 |

The Wago build is `ed977b848d91a2920a91a31ff5f659416cab779d`, source fingerprint
`d0b5d6170afb458ba89ddf0f20e57ecce123482b06543bdc7debf42ec8e9a4d5`.
V8 verifies both independent arithmetic oracles. All three peer launches reject
these indexed atomic memargs: expected alignment 2, got 66. No peer ratio is
reported. Raw `threads-secondary-atomic-arm64.json` has SHA-256
`a06049612e97665178798103708078d7d83ee5efa0e67421d3b1be7eb76a8a44`.
The current threads family contains 24 sustained contracts plus one growth
lifecycle probe; the full family has not been retimed at one common revision.
All 121 feature artifacts reproduce byte-for-byte with wasm-tools 1.260.0.
Performance/protocol Node tests have five passes and one unrelated allocation
artifact skip.

Additional shared memories, broader proposal combinations and execution parity
remain open. The native Go JSPI/CSP counterparts were cancelled before changes.

### Compile cost of indexed atomic admission

Six alternating native ARM64 processes compare `141e4622` with `ed977b84`.
Production builds omit register-allocation checking; test validation uses it
separately. Compilation benchmarks use 300-ms calibration windows. Timed
captures run without concurrent builds or tests. Median times are:

| Complete compilation | Before ns | Current ns | Change |
| --- | ---: | ---: | ---: |
| Small scalar | 6,116 | 6,233 | +1.91% |
| Small scalar with Core 3 enabled | 6,064 | 6,043 | -0.35% |
| Imported workers, one compiler worker | 22,780,756 | 22,922,839 | +0.62% |

Scalar allocations remain 19,202 / 19,058 bytes and 74 / 73 allocations per
compile. Worker code remains 1,587,220 bytes; allocation totals differ by only
single-digit bytes at the median, with 94 allocations at both medians. The last
current launch is slower across all three cases, so small latency differences
should not be treated as an established regression or speedup.

The wider corpus repeats 218 valid pinned Core 3 modules 100 times per process,
using the tracked `compile-corpus.go` helper and the existing checksummed input
inventory. Six alternating processes with GOMAXPROCS=1 give:

| 21,800 compilations | Before | Current |
| --- | ---: | ---: |
| Median elapsed ns | 404,269,291 | 405,209,417 |
| Go allocated bytes | 777,964,272 | 777,964,008 |
| Go allocation count | 2,649,938 | 2,649,933 |
| Generated code bytes | 17,900,800 | 17,900,800 |
| Process maximum RSS bytes | 14,172,160 | 14,139,392 |
| Process peak memory footprint bytes | 9,519,632 | 9,355,792 |

Corpus latency differs by +0.23%; allocation/code/peak measurements show no
material increase in these probes. RSS and footprint include input buffers and
warmup, and do not isolate compiler-only live memory or instantiation cost.
The input inventory is the one recorded in the prior Core 3 compile checkpoint.

Ignored evidence SHA-256:

- `threads-secondary-atomic-native-compile-ab.json`:
  `71019ec0762806836e3abbd15040cee24566289f1a3d2f857a0b2adaa2d12b3e`
- `threads-secondary-atomic-core3-compile-ab.json`:
  `2bf29799cbe2fa73fadf05dcb1eb51c08ec1533134f23334494256e121cd4c81`

An initial guarded compilation capture is retained separately at
`threads-secondary-atomic-compile-ab.json`; its checker overhead is not the
production compilation baseline above.

The pinned official threads suite was rerun after indexed lowering: all 13
scripts, 51 modules and 312 assertions pass on both native architectures under
guarded race checking, with no skips or reported execution gaps. These scripts
exercise the standard primary-memory proposal cases; selected-secondary coverage
comes from the separate matrix above. Retained logs:

- `official-threads-indexed-arm64.txt` SHA-256:
  `8006def98b59f9e9986252cfa2de9b0d6041ac57570486e087bc00b5392fea78`
- `official-threads-indexed-amd64.txt` SHA-256:
  `6d840cc11e93251321d3f25cb6df1ba72ea31c1f7134bfe623a202561293a2a4`


## Shared memories at nonzero indices

Wago `2aa65ce0` admits shared memories at every memory index under explicit
bounds checks. Each owned shared secondary backing establishes its shared type
before execution. Shared growth uses the selected synchronized host helper;
size queries, ordinary/atomic/SIMD accesses and constant/dynamic bulk bounds read
the selected backing's authoritative published byte extent. ARM64 acquires that
extent with LDAR. Primary-memory cached paths are preserved.

Instances with a private primary memory and a shared secondary also receive
threaded invocation and native-state leases. Imported unshared backing, foreign
callables, shared collectors, tables and mutable globals retain the conservative
common execution lease. Shared backing directory refresh never rewrites its
fault owner from another instance's private control.

Active-data initialization previously held the primary shared backing lock while
resolving a secondary backing. Two imported declarations aliasing one Memory
then deadlocked in jobMemory. The new alias test reproduced this on both native
architectures. Initialization now locks only the current segment's backing,
publishes its initialized bytes, and releases the lock before the next segment.
This also synchronizes host-view extension with growth for shared secondary data.

Validation includes:

- All 63 numeric atomic opcodes against shared memory 1, with mixed primary and
  secondary address widths, constant/dynamic addresses, defined/imported backing,
  both call ABIs and codec products; narrow writes, alignment, high offsets and
  bounds-before-write remain checked.
- A separate 32-case matrix, each with original/codec execution, combines a
  private or shared primary, both address widths, ownership and call ABI with
  independent growth, zero/high-delta growth, ordinary and SIMD access,
  cross-memory copy, constant/dynamic fill/init, active/passive data and drop.
- Blocking waits/cancellation on memory 1; notifications against a distinct
  memory 0 cannot wake its waiters. Aliased declarations share one queue and one
  growth extent, and closing one importer preserves the other importer.
- A guest starts with a one-page directory, signals its native entry, then spins
  on memory.size 1 until a separate grower publishes page two. It reads that new
  page without leaving/re-entering native code. This tests live publication,
  including a private primary, without a directory refresh masking stale bounds.

Restoring the old admission gate reproduces rejection of shared memory 1. Full
thread tests and the local Core 3 regression corpus pass under guarded race
checking on ARM64 and AMD64; staged memory64 passes on AMD64. Broader ARM64
memory/data/instance/resource-quota regression tests also pass under guarded race
checking. The pinned official threads suite passes all 13 scripts, 51 modules
and 312 assertions on both architectures with no skips or reported gaps. Its
standard primary-memory cases complement the selected-secondary tests above.

Official threads logs SHA-256:

- `official-threads-shared-secondary-arm64.txt`:
  `591a26a6c9c65264a3bb302ba4eeebf1e980e130ff4d0ebb56609681e15b06f8`
- `official-threads-shared-secondary-amd64.txt`:
  `5187dca12dc856885aba8508be8ae97ff1d3aa812360063e063bf8ce88db4e0b`

### Sustained shared-secondary contracts

wasm.fyi `1ce2a5c` adds three source-built contracts. On Apple M4 Max, Darwin
ARM64, Go 1.27.1, the median of three independently launched Wago processes is:

| Contract, 262,144 work units | Wago ns |
| --- | ---: |
| Shared secondary atomic load/store | 302,497 |
| Shared secondary zero-timeout wait and zero-delta growth | 438,012 |
| Shared-secondary to shared-primary copy | 418,867 |

Each launch has two warmup batches and five verified timed batches of at least
50 ms. A separate V8 launch verifies the independent arithmetic oracle for each
contract. Wago identity is `2aa65ce046172e263968f8c2cd37b3b01f580e35`, source
fingerprint `84281aae26a0eefff72874b85e72ea8d4aef05f70702bcad514f6667b56c0656`.
All three peer launches reject indexed atomic memargs: expected alignment 2,
got 66. No peer timing or same-feature performance ratio is available.
Raw `threads-shared-secondary-arm64.json` has SHA-256
`c8c0b5809a140c2aa42b6291b4e0a1bd47461fc90cdb18404f0e12e3b3a9a89a`.

The threads family now has 27 sustained contracts and one growth lifecycle probe.
These accumulated captures are not a full common-revision retiming. All 124
feature artifacts reproduce byte-for-byte with wasm-tools 1.260.0. The Node
performance/protocol checks have five passes and one unrelated allocation
artifact skip.

### Compile costs

Six alternating production ARM64 processes compare `c223867e` and `2aa65ce0`
without overlapping timed captures with builds/tests. Register-allocation checks
are reserved for validation, not production compilation measurements.

| Complete compilation | Before ns | Current ns |
| --- | ---: | ---: |
| Small scalar | 6,289 | 6,088 |
| Small scalar with Core 3 | 6,448 | 6,115 |
| Imported workers, one compiler worker | 23,550,536 | 23,382,920 |

Scalar allocation totals remain 19,202 / 19,058 bytes and 74 / 73 allocations;
worker generated code remains 1,587,220 bytes with 94 allocations at both medians.
These small latency differences are not evidence of a general speedup.

The 218-module Core 3 corpus helper makes 21,800 compiles per process. Six
alternating processes with GOMAXPROCS=1 give these medians:

| Corpus metric | Before | Current |
| --- | ---: | ---: |
| Elapsed ns | 419,936,042 | 415,502,959 |
| Go allocated bytes | 777,964,464 | 777,964,584 |
| Go allocations | 2,649,939 | 2,649,937 |
| Generated code bytes | 17,900,800 | 17,900,800 |
| Process maximum RSS bytes | 14,057,472 | 14,073,856 |
| Process peak footprint bytes | 9,339,408 | 9,519,656 |

Latency is -1.06%, allocation/code totals are essentially unchanged, and peak
process memory varies slightly (+0.12% RSS, +1.93% footprint). These probes show
no material compile-cost increase. Process peaks include inputs and warmup and
are not isolated compiler-only live memory or instantiation measurements.

Ignored compile evidence SHA-256:

- `threads-shared-secondary-native-compile-ab.json`:
  `74f9274ca4c1a1696f29f077812c186e95551b3f633da36b8bb5a3bbe4cd6433`
- `threads-shared-secondary-core3-compile-ab.json`:
  `cdb30e8e80e2587e6bec45461e64152d2a5ef9a7e4b84b38c504456a295d800a`

Broader proposal combinations and feature-by-feature execution parity remain
open. The cancelled native Go JSPI/CSP counterparts remain absent.

A fresh full pinned Core 3 execution run after this stage passes all 258 source
files, 2,226 modules and 58,238 assertions on both native architectures, with no
failures, skips or reported execution gaps. The same 28 files use the pinned
reference interpreter text conversion fallback. WABT remains 1.0.41 and the
spec/interpreter revision remains 9d36019973201a19f9c9ebb0f10828b2fe2374aa.
These full Core 3 runs use the allocation checker, without the Go race detector;
thread and targeted regression validation above uses both.

- `official-core3-shared-secondary-arm64.txt` SHA-256:
  `e4680565ecd8123994208f346cae750b4f4f0db95a5d4fc6fc2f8e0a8d08ffd9`
- `official-core3-shared-secondary-amd64.txt` SHA-256:
  `d6a2debe616546ec85519c8532113968862434bfe5a335a0096c0cc7c751cbcc`
