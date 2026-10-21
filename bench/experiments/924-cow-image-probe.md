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

The compiled module builds one sparse Linux memfd at first eligible
instantiation, with the ordinary 288-byte basedata prefix followed by ordered
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
instance after `Compiled.Close`, and 12 concurrent instantiations. Real yyjson and PHP modules instantiate twice
and agree with their active data bytes. PHP's 43 function imports use no-op
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

## Full Wago measurements

Linux/AMD64 Ryzen 7 8845HS. Two short runs each, same compiled modules and
results. For warm instance throughput, the image is built before timing:

```sh
GOCACHE=/tmp/wago-go-cache go test ./src/wago -run '^$' -bench '^BenchmarkCOWIntegratedInstantiate$' -benchtime=60ms -count=2 -benchmem
```

| Workload | Default warm ns/op | CoW warm ns/op | Go B/op / allocs, default → CoW | Native code B |
| --- | ---: | ---: | ---: | ---: |
| Real yyjson | 6,130 / 6,044 | 20,847 / 20,927 | 1,096 / 5 → 1,160 / 6 | 298,661 |
| PHP-derived data-only module | 1,645,788 / 1,787,969 | 913,508 / 917,223 | 1,040 / 3 → 1,104 / 4 | 0 |
| Real PHP with inert import bindings | 2,594,465 / 2,695,918 | 1,582,802 / 1,575,344 | 28,816 / 482 → 28,880 / 483 | 30,164,957 |

The real PHP instance initializes the complete real module and data, but does
not run PHP code. The data-only case isolates initialization. The small yyjson
regression is material; this path should not become universal.

`TestCOWImageFirstUseCost -count=2 -v` includes one-time image creation at
the first instance. Real PHP Compile is **645.6/656.7 ms default** and
**644.0/645.1 ms CoW** (image creation occurs later); first instance is
**7.18/7.24 ms default** versus **11.44/12.46 ms CoW**. The approximately
4–5 ms first-use penalty is recovered after several warm PHP instantiations
under this artificial no-entrypoint workload. Native code size is identical.

A later `TestCOWImageFirstUseCost -count=2 -v` recheck reproduced the
first-instance cost: real PHP default **7.50/7.75 ms** versus CoW
**11.36/12.53 ms**; the data-only PHP fixture was **2.07/2.44 ms** versus
**6.40/7.13 ms**. Native code bytes remained 30,164,957 for real PHP.
This points to first-use image construction rather than native codegen;
it does not isolate the memfd operations individually. The draft decision
is unchanged.

`TestCOWImageIntegratedPSS -count=2 -v` holds 1 or 10 **real PHP** instances
and reads the first byte of every active data segment in every instance.
Whole-process `/proc/self/smaps_rollup` PSS at 10 instances was **266,368 /
284,536 KiB default** versus **173,832 / 189,164 KiB CoW**, a directional
92–95 MiB difference in paired runs. At one instance the paired figures were
167,820/167,308 KiB and 183,108/182,748 KiB, too close relative to changing
process heap state to claim a one-instance memory win. The same process runs
modes sequentially; this is not isolated peak RSS. Pages dirtied by real PHP
execution may reduce sharing; the real-command followup below tests execution,
though it does not yet measure dirty-page PSS.

An unchanged, no-active-data instance control was 5,255/5,324 ns/op on base
and 5,370/5,379 ns/op on head, with **1,368 B/op and 8 allocs/op** on both.
The 1–2% time gap is short-run noise or a small default-path cost; it needs
attention before default enablement.

## Real PHP command and end-to-end cost

The bench suite supplies PHP's actual WASI imports, input program, and pinned
output oracle. `TestCowPHPRealCommand` compiles the **same real PHP Wasm**
with explicit bounds in default and opt-in modes, executes the entrypoint
twice per mode, and requires equal results, stdout, stderr, output files, and
the existing corpus oracle. It passed in 1.53 seconds total. This closes the
prior correctness gap from inert import stubs; it does not assert all PHP
semantics or other input programs. Run from `bench`:

```sh
GOCACHE=/tmp/wago-go-cache go test ./suite -run '^TestCowPHPRealCommand$' -count=1 -v -timeout=120s -args -wago.corpus=all
GOCACHE=/tmp/wago-go-cache go test ./suite -run '^$' -bench '^BenchmarkCowPHPRealCommand$' -benchtime=5x -count=3 -benchmem -timeout=120s -args -wago.corpus=all
```

The benchmark excludes Compile, preflights one full oracle per mode, then
measures each fresh instance through the real PHP command (including private
page faults and normal execution). Short directional samples on the same
Linux/AMD64 machine:

| Mode | ns/op (five commands each, three samples) | Go B/op | allocs/op |
| --- | ---: | ---: | ---: |
| Default | 6,456,002 / 5,361,740 / 5,231,330 | 77,641 | 961 |
| CoW opt-in | 6,948,195 / 6,720,054 / 6,518,906 | 77,705–77,718 | 962 |

A second, three-command/two-sample check gave default 4,723,727 /
4,745,842 ns and CoW 5,437,092 / 5,491,829 ns. The short-run spread and
sequential mode order limit precision, but **both checks point to an
end-to-end slowdown** for this PHP input. Faster warm instance creation does
not translate into faster command completion. Per-operation Go allocation
changes are small; page fault and kernel mapping work is not captured by
Go B/op. Production native code bytes remain identical. Whole-process PSS
was measured on instances before execution, so the earlier 10-instance memory
result cannot be attributed to completed real PHP commands.

## Decision

Keep the Linux implementation opt-in and this PR draft. It provides lower PSS for 10 PHP instances before execution and faster warm
PHP instantiation, but the real PHP command is directionally slower, first use
costs more, and small yyjson regresses severely. Before ready review, measure
dirty-page sharing and total peak RSS after real command execution across
independent processes, verify file-descriptor/mapping quotas and failure
injection, and define an admission threshold that excludes small or
short-lived workloads. No
merge or default enablement is supported by these results.
