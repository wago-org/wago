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

This is partial thread support. Shared-memory growth, bulk segment
initialization, multiple memories, and broader import/segment combinations
remain restricted. Shared copy/fill support is recorded below. JSPI and JS-string embedding profiles remain unsupported.

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
