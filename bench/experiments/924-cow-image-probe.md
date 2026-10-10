# #924: initialized memory image — opt-in Wago integration

Base: `origin/main` `209e448c392510a0325d5b282a0d86a776fb379c`.
This branch now has an **opt-in Linux AMD64/ARM64 Wago memory path** selected by
`WAGO_EXPERIMENT_COW_IMAGE=1` (deferred image creation) or `eager` (first
eligible instance). Default behavior is unchanged. Keep the PR draft.

## Phase-0 OS oracle

`GOCACHE=/tmp/wago-go-cache go test ./src/wago -run '^(TestCOWImageCoverage|TestCOWImagePrivateMapping|TestCOWImageMultiInstancePSS)$' -count=1 -v` surveys six real modules and builds temporary file-backed images for SQLite and PHP. Every surveyed module has one owned, unshared memory32 and constant-offset active data. None of their active spans overlap in this sample; write order still needs a general overlap oracle for a production path.

| Module | Active segments | Source payload bytes | Active 4 KiB pages | Highest initialized byte |
| --- | ---: | ---: | ---: | ---: |
| SQLite | 412 | 123,227 | 32 | 1,178,403 |
| QuickJS | 214 | 126,461 | 32 | 1,177,539 |
| jq | 407 | 389,688 | 97 | 1,443,883 |
| PHP | 63,770 | 3,203,245 | 2,418 | 9,907,883 |
| Lua | 3 | 35,844 | 10 | 36,880 |
| yyjson | 59 | 15,655 | 5 | 82,423 |

The test-only oracle creates one ordered image file, maps it with `MAP_PRIVATE` twice, checks the original bytes at every segment, writes one mapping, and verifies the sibling and backing file remain unchanged. It passes for SQLite and PHP. Mapping-specific `/proc/self/smaps` PSS after reading every active page is directional: SQLite 1/10/100 live instances **128/120/100 KiB** (32 active pages), PHP 1/10 instances **9,672/9,670 KiB** (2,418 pages). PSS can fluctuate with kernel page accounting and concurrent process activity; the near-constant result is consistent with shared clean image pages. Virtual mapping lengths still scale: PHP 9.9 MiB per instance. The compiled module's input bytes, image file/page cache, other Wago mappings, and total process PSS are outside this mapping-only figure.

`GOCACHE=/tmp/wago-go-cache go test ./src/wago -run '^$' -bench '^BenchmarkCOWImagePrototype$' -benchtime=50ms -count=2 -benchmem` compares isolated image operations. SQLite: private map only **4.3/4.5 µs**, map plus read all 32 active pages **11.1/11.0 µs**, map plus one first write **10.0/10.5 µs**, fresh buffer and segment copies **55.6/52.0 µs**. PHP: map only **7.9/7.9 µs**, map plus read all 2,418 active pages **501/487 µs**, map plus one first write **9.7/10.1 µs**, fresh buffer and 63,770 segment copies **1,318/1,460 µs**. Map cases report 0 Go B/op and 0 Go allocs/op because the mapped pages are off heap; copy cases allocate approximately 1.18 MiB (SQLite) or 9.91 MiB (PHP) per operation. Image file creation, compilation, source retention, actual Wago instance setup, memory growth, guard layout, and cleanup are excluded. The copy loop is a proxy for active initialization, not a measured current Wago instantiation baseline.

The [Wasmtime article](https://bytecodealliance.org/articles/wasmtime-10-performance) describes its own CoW instance allocator; these standalone results cannot be attributed to Wago. These phase-0 numbers exclude Wago instance construction and are retained only as the OS control.

## Integrated path

For the normal opt-in, only modules with at least 1,024 active data segments
and at least 1 MiB initial linear memory are considered. Their first instance
uses the ordinary path, the second builds a sparse Linux memfd, and later
instances reuse it. This is an empirical cost gate for this experiment, not
a general break-even theorem. `eager` applies the same size/segment gate but
builds the first instance's image when the caller prioritizes retained memory
over first-use latency. Tests use `WAGO_EXPERIMENT_COW_IMAGE=force` to
exercise the mapping mechanism on small fixtures. The image has Wago's
ordinary 288-byte basedata prefix followed by ordered
active data. The file is sized to Wago's **full growable reservation** (up to
4 GiB memory32), but only written pages consume physical backing. Instances
map it `MAP_PRIVATE` and retain Wago's stable native memory base and growth
size caches. Image mappings close rather than entering the anonymous
zero-on-reuse cache. Closing/replacing the Compiled module closes the file;
live private mappings remain valid. Eligibility is one locally owned, unshared
memory32, explicit bounds, constant active offsets, and all segments within
the initial logical size. Imported/shared/multi-memory, memory64, guard mode,
dynamic offsets, empty out-of-bounds segments, and other cases use the existing
path. No artifact format or native code changes.

If memfd setup, descriptor duplication, or a private image mapping fails,
instantiation retries the ordinary owned-memory path. An injected bad image
descriptor test confirms the fallback's initialized bytes and isolation while
an already-mapped sibling remains alive. A logically closed compiled module
still fails closed; it is not treated as a resource fallback.

The synthetic oracle covers **ordered overlapping segments**, host and guest
private writes, guest `memory.grow`, a fresh instance after close, a live
instance after `Compiled.Close`, and 12 concurrent instantiations. Configuration
and quota tests cover changing the memory reservation and a constrained grow.
Real yyjson uses forced mode for mapping correctness; normal opt-in keeps it
on the ordinary path. PHP instantiates twice and agrees with its active data
bytes. PHP's 43 function imports use no-op
signature-matching stubs solely for instantiation; PHP's entrypoint is **not
executed in this initializer test**. The separate real-command followup below
executes it with genuine WASI imports. Both real modules' mappings are verified in `/proc/self/maps`.

```sh
WAGO_EXPERIMENT_COW_IMAGE=1 GOCACHE=/tmp/wago-go-cache go test ./src/wago ./src/core/runtime -run '^(TestCOWImage|TestActiveOffsetsUseLocalImmutableGlobals|TestCompactExecutionData|TestActiveDataSegment|TestImportedMemory|TestMemoryGrowExported|TestMemoryCloseRacingInstantiationFailsClosed|TestAcquireJobMemoryGrowable|TestSerialCompiledCloseBeforeInstantiateReleasesCodeImage)' -count=1
GOCACHE=/tmp/wago-go-cache go test -race ./src/wago -run '^TestCOWImage(ParallelInstantiation|IntegratedInstances|RealPHPInitialization)$' -count=2
GOOS=linux GOARCH=arm64 GOCACHE=/tmp/wago-go-cache go test -c -o /tmp/wago-924-linux-arm64.test ./src/wago
GOOS=darwin GOARCH=arm64 GOCACHE=/tmp/wago-go-cache go test -c -o /tmp/wago-924-darwin-arm64.test ./src/wago
```

The focused tests, race check, and cross-builds pass. An initial full opt-in
run lacked the pinned spec-v3 fixture. After `git submodule update --init
--depth=1 tests/conformance/spec-v3` restored revision
`9d36019973201a19f9c9ebb0f10828b2fe2374aa` and the repository-pinned
WABT 1.0.41 was put on PATH, `WAGO_EXPERIMENT_COW_IMAGE=1 go test ./src/wago
./src/core/runtime -count=1 -timeout=180s` passed both packages (20.833s and
0.444s). `go vet` and `git diff --check` pass. The code-cache hot header remains 64 bytes, as
required by its footprint test; the optional descriptor lives in a cold
sidecar.

## Repeated instantiation and image costs

Linux/AMD64 Ryzen 7 8845HS. All comparisons use the same module and runtime
configuration; command runs check the same pinned output oracle. The base is
`209e448c392510a0325d5b282a0d86a776fb379c` and the branch includes the
prototype plus the admission refinement described above. The baseline in
current head comparisons is that same binary with
`WAGO_EXPERIMENT_COW_IMAGE=0`, not a separate `origin/main` binary. The
original prototype rescanned PHP's 63,770 segments and duplicated its image
FD at **165–180 µs** per warm instance (20x, three samples). The revised cached
plan and FD duplication take **1.06–1.55 µs** (same benchmark). A standalone
eligibility traversal is **497–516 µs**; a fresh map/unmap **18.9–21.7 µs**;
map plus touching 2,480 pages **644–677 µs**. These isolated operations
exclude imports, PHP execution, and image creation.

```sh
GOCACHE=/tmp/wago-go-cache GOPROXY=off go test ./src/wago -run '^$' -bench '^BenchmarkCOW(ImageWarmParts|IntegratedInstantiate)$' -benchtime=20x -count=3 -benchmem -cpu=1
```

After two warmup instances, real PHP with 43 inert import bindings takes
**3.14/3.20/3.35 ms** per ordinary instance versus **1.76/1.97/1.82 ms**
for image instances; Go allocations are **28,816 B / 482** versus
**28,880 B / 483** per instance. Its native code is **30,164,957 B** in both
modes. PHP-derived data-only instances take **2.29–2.34 ms** ordinary versus
**0.82–0.88 ms** CoW. Real yyjson stays ordinary under normal opt-in:
**6.49–7.02 µs** baseline versus **6.43–8.43 µs** with flag enabled, short-run
noise; both use **1,096 B / 5** Go allocations and **298,661 B** native code.
The original always-admit prototype had made yyjson **20.8–20.9 µs** versus
**6.0–6.1 µs**, motivating the small-module cost gate.

`TestCOWImageFirstUseCost -v` makes three instances: real PHP was
**7.86 / 2.82 / 2.84 ms** baseline and **8.08 / 7.91 / 1.60 ms** opt-in.
The second opt-in instance pays image creation; the third reuses it. This
single run isolates the timing pattern, not a stable break-even count. Compile
was **777 / 775 ms**, native code unchanged. The earlier always-admit
prototype had charged its first PHP instance an extra **4–5 ms**.

## Actual PHP command and retained memory after execution

The benchmark supplies PHP's actual WASI imports, input, and pinned output
oracle. Each measurement process compiles the same real PHP Wasm, retains
eight instances, executes `_start` in every instance, validates each output,
then hashes every byte of each 14,352,384-byte linear memory. Baseline and
opt-in produce the same concatenated memory SHA-256
`eabe80f5196ca887f0ca9b505a67099bd3a7e47f168f41e9020ce648e78e227d`
in all four runs. Each compilation emits **30,164,957 B** native code. The
CoW process has one ordinary instance and seven image instances by design.

```sh
WAGO_924_POSTEXEC_MODE=baseline WAGO_924_POSTEXEC_INSTANCES=8 GOCACHE=/tmp/wago-go-cache GOPROXY=off go test ./bench/suite -run '^TestCowPHPPostExecutionMemory$' -count=1 -v -args -wago.corpus=php-buckets
# repeat with WAGO_924_POSTEXEC_MODE=cow; run each arm twice in its own process
```

| Eight real PHP instances | Baseline (two samples) | CoW opt-in (two samples) |
| --- | ---: | ---: |
| PSS after initialization | 244,966 / 245,087 KiB | 175,050 / 177,194 KiB |
| PSS after `_start` | 294,270 / 294,391 KiB | 221,486 / 223,914 KiB |
| RSS after `_start` | 295,864 / 295,948 KiB | 231,124 / 234,228 KiB |
| PSS after host reads all memory | 294,270 / 294,395 KiB | 230,482 / 232,630 KiB |
| Aggregate instantiate time | 28.86 / 27.73 ms | 27.26 / 25.82 ms |
| Aggregate command execution | 25.53 / 25.38 ms | 36.48 / 37.53 ms |
| Compile time | 820 / 774 ms | 780 / 757 ms |

Thus savings remain after this real program executes: about **69–71 MiB
PSS** and **60–63 MiB RSS** in these short paired runs, with an **11–12 ms**
aggregate execution penalty. Host hashing faults clean image pages into the
process, increasing CoW RSS; PSS remains about **60–63 MiB** lower. `HeapAlloc`
is around 72 MiB in both arms, as expected for off-heap mapping. These are
whole-process snapshots, not peak memory, and the samples are too short for
precise latency claims. The prior always-admit implementation saved around
87–90 MiB PSS after execution at eight instances, but also paid the image
cost on the first instance. The admission gate trades some sharing for a
cheaper single-instance path.

A separate benchmark warms twice in each mode, then creates a fresh PHP
instance and executes the same command per iteration:

```sh
GOCACHE=/tmp/wago-go-cache GOPROXY=off go test ./bench/suite -run '^$' -bench '^BenchmarkCowPHPRealCommand$' -benchtime=10x -count=3 -benchmem -cpu=1 -args -wago.corpus=php-buckets
```

Baseline samples are **6.23/6.29/7.18 ms** and opt-in samples
**7.30/7.17/6.96 ms** per command. Go allocation is **77,641–77,652 B / 961**
versus **77,705 B / 962**. Sample overlap and sequential mode order mean
this command benchmark does not establish a precise latency difference; the
retained-instance experiment above isolates a credible execution penalty for
that input. Neither benchmark includes recurring compilation in its timing.

## Read and write boundary

A controlled Wasm fixture keeps PHP's actual ordered data segments and adds a
function that visits one byte of each of its **2,480** initial pages. Read mode
only reads; write mode increments that byte. Eight instances run in separate
processes per mode. Output checksum and full memory SHA agree across baseline
and CoW for each workload. The fixture's generated function has **197 B**
native code; it does not execute the PHP interpreter.

```sh
WAGO_924_POSTEXEC_MODE=baseline WAGO_924_POSTEXEC_INSTANCES=8 WAGO_924_PAGE_EXERCISE=read GOCACHE=/tmp/wago-go-cache GOPROXY=off go test ./src/wago -run '^TestCOWImagePageReadWriteMemory$' -count=1 -v
# repeat baseline/cow for read/write in independent processes
```

For the read fixture, executed PSS is **129,572 KiB** baseline versus
**67,932 KiB** CoW, while RSS is approximately **129,580 / 127,436 KiB**:
clean file-backed pages count fully toward process RSS but proportionally
toward PSS. Aggregate execution is **1.21 / 3.65 ms**. For write-every-page,
executed PSS is **131,808 / 127,672 KiB**, within process baseline drift;
aggregate execution is **1.28 / 35.76 ms**. Once every page is dirtied,
sharing benefit effectively disappears and private-page fault cost dominates.
The earlier all-eight-CoW version reproduced the same boundary in two
independent runs per arm; the admission gate makes one instance ordinary.

The same isolated page control was run at **1, 10, and 100** live instances
in ordinary versus eager mode (separate process per arm). Both arms have
identical full-memory SHA at each count and each read/write mode. The
100-instance read result was repeated independently.

| Instances / action | Ordinary executed PSS KiB | Eager CoW executed PSS KiB | Ordinary / CoW aggregate execute ms |
| --- | ---: | ---: | ---: |
| 1 / read | 43,224 | 44,908 | 0.13 / 0.95 |
| 10 / read | 151,736 | 62,228 | 1.44 / 6.25 |
| 100 / read | 1,231,716 / 1,231,640 | 247,796 / 248,788 | 14.94 / 46.70 (first pair) |
| 1 / write every page | 43,948 | 44,616 | 0.15 / 6.13 |
| 10 / write every page | 153,536 | 151,056 | 1.35 / 55.64 |
| 100 / write every page | 1,231,996 | 1,229,780 | 14.96 / 533.63 |

At 100 read-mostly instances, about **959–961 MiB** of process PSS is saved.
As an illustrative allowance, subtracting the **14 MiB** backing residency
observed in the separate eight-instance PHP workload would still leave a
saving above **945 MiB**. Backing residency was not directly measured in the
100-instance process, so this is not a system-wide physical-memory bound. RSS is about
1.23 GiB in both arms because it counts shared mapped pages in every VMA.
At 100 write-all instances, PSS is effectively equal and execution is far
slower. The 100-instance control is synthetic execution over real PHP data,
not 100 independent PHP interpreters. Short timings include only the added
Wasm function, not compilation or host hashing.

```sh
WAGO_924_POSTEXEC_MODE=baseline WAGO_924_POSTEXEC_INSTANCES=100 WAGO_924_PAGE_EXERCISE=read GOCACHE=/tmp/wago-go-cache GOPROXY=off go test ./src/wago -run '^TestCOWImagePageReadWriteMemory$' -count=1 -v
# repeat in its own process with WAGO_924_POSTEXEC_MODE=cow WAGO_924_EAGER=1;
# repeat both modes with instance counts 1 and 10 and exercise=write
```

## Follow-up: physical residency and first-use choice

The post-execution harness now reads `/proc/self/smaps` for the actual
`memfd:wago-cow-image` mappings as well as process rollup. With eight PHP
instances in deferred mode, seven image mappings have only **28 KiB** PSS
immediately after initialization: pages have not been faulted into those
private mappings. After `_start`, their PSS is about **26 MiB**, including
about **24 MiB private dirty**. Reading all linear-memory bytes from the host
raises image-mapping PSS to about **35 MiB** and RSS to about **98 MiB**.
These are mapping subtotals; the compiled module, first ordinary instance,
Go heap and other mappings appear elsewhere in the process total. A separate
`mincore` check on the owning memfd finds **9,668 KiB resident backing pages
after initialization**, **13,108 KiB after execution**, and **14,020 KiB
after the host reads all memory**. Some backing pages can remain in page
cache without a resident process mapping and are then absent from process
PSS. Neither process PSS nor this file-cache count is a complete system-wide
physical-memory accounting, and the two figures cannot simply be added
because some backing pages are mapped. The guest's dirty pages must stay
private to preserve memory semantics.
The harness also records `getrusage` maximum RSS. That is a process-lifetime
high-water mark including compilation, not a phase-local peak.

```sh
WAGO_924_POSTEXEC_MODE=cow WAGO_924_POSTEXEC_INSTANCES=8 WAGO_924_EAGER=0 GOCACHE=/tmp/wago-go-cache GOPROXY=off go test ./bench/suite -run '^TestCowPHPPostExecutionMemory$' -count=1 -v -args -wago.corpus=php-buckets
# repeat twice with WAGO_924_EAGER=1, separately for one and eight instances
```

The two eight-instance paired runs use identical PHP input/output oracles,
full-memory SHA and **30,164,957 B** native code. Deferred mode has
post-execution PSS **222,639/222,059 KiB**; eager mode has
**212,739/213,595 KiB**, roughly **8–10 MiB** less observed process PSS.
The incremental PSS above each run's compiled snapshot is roughly **11 MiB**
less in eager mode. Aggregate execution is **29.75/27.40 ms** deferred versus
**31.94/27.92 ms** eager; this short comparison shows no reliable speed gain.
For a single instance, eager increases setup **6.68/6.93 → 12.52/11.30 ms**
and execution **2.48/2.46 → 4.66/4.44 ms**. Reading all memory from the
host largely erases its one-instance PSS benefit. This is why `1` still
defers image creation; `eager` is an explicit memory preference.

The page control also matches full-memory hashes with eager admission. For
eight read-only instances, PSS is **67,164 KiB** deferred versus
**56,232 KiB** eager. For eight instances writing every page, PSS is
**127,364 / 129,992 KiB**; the extra image does not save memory in that
workload. Execution remains expensive at **31.76 / 34.41 ms**, compared with
about **1.28 ms** for the ordinary write control above.

## First-use plus command qualification of eager versus deferred admission

To account for first use alongside actual command execution, twelve
fresh processes ran **1 or 8** PHP instances with baseline, deferred, and
eager admission, twice per arm. Each process compiled the same Wasm, retained
the instances, executed the same pinned PHP command, validated its output,
and hashed all linear-memory bytes. Within each instance count, all arms have
identical full-memory SHA-256 (**`561eb0a008a9…`** for one instance,
**`eabe80f5196c…`** for eight); native code is **30,164,957 B** throughout.
The sum below is separately timed aggregate `Instantiate` plus `Invoke`; compilation,
host scratch/import construction, output validation, hashing, and memory
scans are excluded. Compile samples range **667.9–758.9 ms** across the
eight-instance arms, without a directional compilation claim.

| Live PHP instances / path | Aggregate instantiate ms | Aggregate command ms | Setup + command ms | Executed PSS KiB |
| --- | ---: | ---: | ---: | ---: |
| 1 / ordinary | 7.732 / 7.285 | 2.564 / 2.567 | 10.296 / 9.852 | 176,373 / 177,305 |
| 1 / deferred | 7.293 / 7.469 | 2.726 / 2.677 | 10.019 / 10.146 | 176,375 / 176,298 |
| 1 / eager | 12.656 / 11.741 | 4.658 / 4.631 | 17.314 / 16.372 | 166,797 / 163,270 |
| 8 / ordinary | 29.350 / 27.303 | 19.550 / 23.587 | 48.900 / 50.890 | 296,086 / 298,562 |
| 8 / deferred | 24.220 / 25.016 | 34.524 / 33.435 | 58.744 / 58.451 | 221,457 / 219,777 |
| 8 / eager | 24.531 / 23.570 | 37.616 / 36.663 | 62.147 / 60.233 | 208,777 / 210,886 |

Adding separately timed compilation to setup plus command yields **716.798 /
798.448 ms** ordinary, **777.647 / 817.349 ms** deferred, and **785.686 /
805.960 ms** eager for eight instances. For the first single instance, these
compile-inclusive sums are **694.902 / 693.703**, **704.773 / 733.964**, and
**715.830 / 727.455 ms** respectively. Compilation noise is larger than the
mode difference in these short samples; this is not a compilation-lifecycle
speed claim. Exact one-instance memory SHA-256 is
`561eb0a008a94bb1dd22c01d1b264ef4cd5b7ea6a8d009144dc6a0c8960f3e7b`.
At the executed checkpoint, process-lifetime maximum RSS (`getrusage`,
including compilation and setup) is **297,624 / 300,120 KiB** ordinary,
**231,036 / 229,760 KiB** deferred, and **220,776 / 222,856 KiB** eager
for eight instances. At one instance it is **177,880 / 178,900**,
**177,932 / 177,916**, and **169,116 / 165,680 KiB** respectively. These
are high-water marks through execution, not phase-local peaks.

```sh
# Run each arm twice in its own process; use INSTANCES=1 and 8.
PATH=/home/jtenner/.local/share/mise/installs/github-web-assembly-wabt/1.0.41/bin:$PATH \
WAGO_924_POSTEXEC_MODE=baseline WAGO_924_POSTEXEC_INSTANCES=8 \
GOCACHE=/tmp/wago-go-cache GOPROXY=off go test ./bench/suite \
  -run '^TestCowPHPPostExecutionMemory$' -count=1 -v -args -wago.corpus=php-buckets
# For deferred: MODE=cow, WAGO_924_EAGER=0; for eager: MODE=cow, WAGO_924_EAGER=1.
```

The eight-instance deferred process saves **74,629 / 78,785 KiB PSS** against
the paired ordinary process; eager saves **87,309 / 87,676 KiB PSS**. Its
owning image has **13,108 KiB** resident backing after execution in every
CoW arm, measured separately with `mincore`. As a deliberately conservative
illustration, charging *all* of that backing as additional to process PSS
still leaves about **60–64 MiB** deferred or **72–73 MiB** eager difference.
Some backing pages are already represented in mapped PSS, so adding the two
double-counts them. This adjustment supports the direction of physical
savings for this paired workload; it is not a precise system-wide total.
The eight eager mappings have **27,800 KiB private dirty** after execution,
compared with **24,276 / 24,360 KiB** for seven deferred mappings. This is
an observed write footprint, not a page-level first-write fraction for every
operation. The single-instance eager path has a smaller PSS difference and
adds **6–7 ms** to first setup plus command; deferred behaves like ordinary
at one instance. Its roughly **9–14 MiB** process PSS reduction may be
smaller than uncharged backing residency, so these data do not establish a
single-instance system-RAM saving.

A defensible opt-in contract is therefore a **retained, multi-instance,
memory-constrained, mostly read-heavy module** with large constant active
data and tolerance for roughly **8–13 ms** extra setup-plus-command time per
eight PHP commands on this host. The 100-instance read-only control exposes
scaling potential, but it does not establish that 100 real PHP interpreters
will behave similarly. Fully dirtying the pages erases the benefit. The
contract needs application-specific measurement; the 1,024-segment/1-MiB
gate alone cannot predict guest write behavior.

Two further cost leads were measured and rejected:

* Linux `MADV_POPULATE_READ` over each initial mapping before execution costs
  **2.88–3.16 ms** for eight PHP instances. Execution then takes
  **28.99–31.67 ms** versus **31.61–32.42 ms** without prefaulting; combined
  time has no reliable gain. It raises post-execution RSS from roughly
  **229–231 MiB** to **288–290 MiB** and PSS by several MiB. The opt-in test
  harness exposes `WAGO_924_PREFETCH_READ=1` to reproduce this result; the
  runtime does not prefetch.
* A short first-image build benchmark (`BenchmarkCOWImageBuildParts`, `5x`,
  three samples) finds descriptor/truncate/map/unmap setup at **8.3–9.6 µs**,
  ordered scatter into fresh file pages at **4.11–4.55 ms**, and the full
  create/copy/unmap/close at **5.20–5.33 ms**. The first-use penalty is
  dominated by the writes/page faults, not the FD duplication or eligibility
  scan. Retaining another fully materialized payload to avoid that work would
  undermine the low-memory goal and is not part of this branch.

These are the two measured overhead leads for a small follow-up: warming
pages merely shifts fault work and raises resident memory, while fresh image
scatter dominates first-use creation. Neither yielded a bounded, low-risk
improvement on this input, so there is no further implementation change in
this experiment.

## Verification and decision

The ordinary and `-tags=wago_regalloccheck` full `./src/wago
./src/core/runtime` suites pass with both `WAGO_EXPERIMENT_COW_IMAGE=1` and
`eager`, using
pinned WABT 1.0.41 and the exact spec-v3 gitlink. The focused image tests
pass under `-race`. Real PHP command outputs and both full-memory hashes
agree across modes. These checks cover the measured cases, not arbitrary
Wasm programs. The cost gate is workload-specific and the post-execution
measurements use only one PHP input and short runs.

An independent internal review reproduced the eight-instance PHP oracle and
memory comparison, plus the 100-instance read/write boundary with identical
full-memory hashes. It found that a plain
descriptor `Dup` cleared close-on-exec; image FD requests now use
`F_DUPFD_CLOEXEC`, with a direct regression check. The reviewer also checked
that execution time measures `Invoke` only, while output validation and
full-memory hashing happen after that timer. The fresh-command benchmark
includes instantiation and command execution after verified warmups.
The review further found that only mapping resource failures should trigger
ordinary-memory fallback. The runtime now identifies that specific failure;
size validation, an invalid descriptor and an undersized image remain errors.

Keep the feature opt-in. For repeated read-heavy PHP instances, the reduced
PSS is credible and warm instance setup is faster. `eager` gains additional
memory at a first-use cost. Execution is directionally slower; write-heavy
instances lose the memory benefit. The gate is empirical and based only on
real module size and active segment count, not predicted runtime writes.
Promotion to ready for review would need Josh to accept this explicit
memory-versus-latency contract for a target application, or another real
workload showing material memory savings at an acceptable full-command and
first-write cost. The current PHP result is a credible opt-in memory win,
but its measured execution penalty and unknown application tolerance do not
qualify an unscoped ready-for-review claim. Default enablement would need
broader real-program evidence. No merge is proposed.
