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

This is partial thread support. Bulk segment initialization, shared memory64,
multiple memories, and broader import/segment combinations remain restricted.
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
