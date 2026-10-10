# #924: initialized memory image — opt-in Wago integration

Base: `origin/main` `209e448c392510a0325d5b282a0d86a776fb379c`.
This branch now has an **opt-in Linux AMD64/ARM64 Wago memory path** selected by
`WAGO_EXPERIMENT_COW_IMAGE=1`. Default behavior is unchanged. Keep the PR draft.

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
a general break-even theorem. Tests use `WAGO_EXPERIMENT_COW_IMAGE=force` to
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
prototype plus the admission refinement described above. The original
prototype rescanned PHP's 63,770 segments and duplicated its image FD at
**165–180 µs** per warm instance (20x, three samples). The revised cached
plan and FD duplication take **1.03–1.26 µs** (same benchmark). A standalone
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

Thus savings remain after this real program executes: about **70–73 MiB
PSS** and **61–64 MiB RSS** in these short paired runs, with an **11–12 ms**
aggregate execution penalty. Host hashing faults clean image pages into the
process, increasing CoW RSS; PSS remains about **60–64 MiB** lower. `HeapAlloc`
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

## Verification and decision

The ordinary and `-tags=wago_regalloccheck` full `./src/wago
./src/core/runtime` suites pass with `WAGO_EXPERIMENT_COW_IMAGE=1`, using
pinned WABT 1.0.41 and the exact spec-v3 gitlink. The focused image tests
pass under `-race`. Real PHP command outputs and both full-memory hashes
agree across modes. These checks cover the measured cases, not arbitrary
Wasm programs. The cost gate is workload-specific and the post-execution
measurements use only one PHP input and short runs.

Keep this PR **draft** and the feature opt-in. For repeated read-heavy PHP
instances, the reduced PSS is credible and warm instance setup is faster.
Execution is directionally slower; write-heavy instances lose the memory
benefit. Before any default enablement, test more real programs and establish
an admission policy that accounts for page write behavior. No merge is
proposed.
