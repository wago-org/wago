# #925: lazy validated-function compilation admission

Base: `origin/main` at `209e448c392510a0325d5b282a0d86a776fb379c`.
This branch adds a repeatable real-module accounting control, **not** a lazy
compiler. Production compilation is unchanged. Keep the PR draft.

## First evidence

Run on Linux/AMD64 Ryzen 7 8845HS, one local machine:

```sh
GOCACHE=/tmp/wago-go-cache go test ./src/wago -run '^TestLazyCompileFeasibility$' -count=2 -v
```

The test separately calls eager `frontend.DecodeValidate` and the ordinary
`RuntimeConfig.Compile` on identical Wasm bytes. Compile includes its own
validation; timings **must not be added**. The two calls both succeeded. Code
bytes are `Compiled.CodeSize()`, and serialized bytes are from `MarshalBinary`.
Element reference entries and exports are static counts, **not executed-function
coverage**. Gross allocation deltas come from `runtime.MemStats` around Compile;
they are not retained heap or peak RSS and may include background process noise.

| Module | Source B | Local funcs | Element refs | Native B | Artifact B | Validate ms (2) | Compile ms (2) | Compile allocated B (2) | Mallocs (2) |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| SQLite | 1,318,577 | 1,547 | 554 | 4,130,246 | 4,296,986 | 32.59 / 31.61 | 94.56 / 91.85 | 13,479,192 / 13,467,752 | 6,935 / 6,924 |
| QuickJS | 727,617 | 891 | 417 | 1,948,422 | 2,102,675 | 15.18 / 14.66 | 46.09 / 44.51 | 7,152,144 / 7,152,016 | 4,967 / 4,965 |
| jq | 957,803 | 719 | 215 | 1,792,132 | 2,205,019 | 14.49 / 14.56 | 41.59 / 44.22 | 6,667,424 / 6,667,440 | 4,146 / 4,147 |
| PHP | 13,622,978 | 12,592 | 6,767 | 30,164,957 | 34,441,137 | 221.47 / 222.06 | 641.22 / 642.30 | 116,569,624 / 116,537,688 | 35,803 / 35,767 |
| Lua | 306,612 | 778 | 200 | 879,520 | 934,523 | 6.43 / 6.44 | 19.92 / 21.30 | 5,118,056 / 5,118,056 | 3,819 / 3,819 |

These are all real application modules in `corpus/workloads/applications`.
MicroPython could not enter this particular control because the standalone
default frontend validation call rejected its exception-handling tag; the
test's frontend feature choice should be aligned with its Compile feature
choice before adding that module. We did not infer a backend failure.

An unchanged-code baseline/head check used:

```sh
GOCACHE=/tmp/wago-go-cache go test ./src/wago -run '^$' -bench '^BenchmarkCompileSmallScalar$' -benchtime=80ms -count=2 -benchmem
```

| Revision | ns/op (2) | B/op (2) | allocs/op |
| --- | ---: | ---: | ---: |
| Base `209e448c` | 11,725 / 11,527 | 15,274 / 15,271 | 72 |
| Test-only head `b1c0ae10` | 12,011 / 11,898 | 15,272 / 15,271 | 72 |

The 2–4% time difference is ordinary short-run noise; the production code is
identical. This comparison does not measure a lazy variant. The source bytes,
compiled code bytes, and allocations above are an **opportunity ceiling**, not
a claimed saving. It excludes resident source/decode state, dispatch stubs,
code-map bookkeeping, instance count, and actual function execution.

## Current architecture and minimum correct prototype boundary

`src/wago/api.go` compiles the entire validated module using
`railshotCompileValidatedModuleWith`/`railshotCompileModuleWith`, then creates a
single `Compiled` object with the complete code byte slice, `Entry`, and
`InternalEntry`. Direct and indirect call paths use offsets into the mapped
code image. `src/wago/compiled_snapshot.go` requires every internal entry to
point within that image; `src/wago/codec.go` serializes complete entry arrays.
Inserting unresolved offsets into the existing artifact would break that
contract. A first-use stub would also require source retention and code
ownership across compiled-module close, instance sharing, and serialization.

A bounded implementation would keep whole-module decode and validation eager,
capture immutable feature/optimization policy, retain only the body bytes and
metadata needed for later emission, and give every unresolved function a valid
stable call target. It must compile once under concurrent calls, handle
recursion and host reentry without holding a non-reentrant global lock, publish
new executable pages and metadata atomically, and keep code alive while calls
are active. Direct calls, imports/exports, `ref.func`/tables, plug-in calls,
tail calls, GC and exception paths, artifact round-trip, and cancellation need
tests. A complete image eager mode remains the comparison and fallback.

## Decision / next evidence

Do not promote this PR. First instrument native function entry use during
real startup, one-shot, and long-running workloads, and capture per-function
native bytes plus total process PSS/RSS. Without executed coverage, a retained
source estimate, and a safe publication/lifetime design, neither a memory win
nor a working lazy prototype is established. If coverage is high, stop. If a
low-coverage real workload has a compelling **net** resource opportunity,
implement the small fixed-entry/stub design behind an opt-in mode and run the
issue's concurrency and serialization correctness matrix before comparing
with eager production.

The external references motivate the question, but do not establish a Wago
result: [V8's Wasm compilation pipeline](https://v8.dev/docs/wasm-compilation-pipeline)
and [Titzer's in-place WebAssembly interpreter paper](https://arxiv.org/abs/2205.01183).

## Real-command function-entry followup

The disposable WABT rewriter `bench/experiments/925-profile-functions.py` adds a visited bit at the entry of each **local** function. The opt-in suite test runs the original real command against its existing corpus oracle, then runs two fresh instrumented instances, requiring identical results, stdout, stderr, output files, and visited sets. The complete suite was repeated twice with identical results:

```sh
cd bench
WAGO_925_PROFILE=1 GOCACHE=/tmp/wago-go-cache go test -tags=wago_codegenstats ./suite -run '^TestLazyFunctionEntryProfile$' -count=2 -v -timeout=120s -args -wago.corpus=all
```

The two full passes took 20.80 seconds. Per-function body byte attribution comes from a separate original-module standalone AMD64 backend compile with `CodegenStats`; it excludes shared stubs and metadata and may differ from Wago's ordinary production compile policy. It is **not** a lazy compiler or a measured saving. The original Wago native total remains listed separately above.

| Real command | Local funcs | Entered | Entered share | Backend body B | Entered body B | Unentered body B |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| FastTree phylogeny | 163 | 85 | 52.15% | 655,174 | 496,990 | 158,184 |
| jq transform | 719 | 229 | 31.85% | 1,783,584 | 740,318 | 1,043,266 |
| QuickJS script | 891 | 270 | 30.30% | 1,935,597 | 959,167 | 976,430 |
| SQLite query | 1,547 | 334 | 21.59% | 4,111,843 | 1,666,920 | 2,444,923 |
| PHP buckets | 12,592 | 727 | 5.77% | 30,071,824 | 2,825,081 | 27,246,743 |

The PHP input now has actual execution coverage, unlike the initial static element/export proxy. Its 27.25 MB of unentered body code makes it the clearest candidate, though another input or longer run may enter many more functions. Instrumentation increases source size (PHP 13,622,978→14,026,672 B) and compiled size (30,164,957→30,428,535 B); use it only to identify entered functions, never to estimate lazy speed or memory. The original command output is unchanged. The unentered byte sum is an upper bound before the costs of retained validated bodies, first-use stubs, synchronization, executable mapping, and serialization; no net retained RSS or compile-time reduction is claimed.

A safe first implementation still needs the fixed-entry and code-publication protocol described above. The profile gives a concrete target for that work and leaves this PR draft pending a working opt-in implementation and concurrency/serialization correctness tests.

## Input-specific native-code ceiling (not a lazy module)

`bench/experiments/925-unentered-body-ceiling.py` builds a **disposable**, input-specific module by replacing every local body not entered in the measured `php-buckets` run with `unreachable`. The original module has already passed Wago's eager full validation. The transformed module is not semantically equivalent on other inputs and is never a runtime fallback. The test compiles both modules anew and requires the transformed module's results, stdout, stderr, files, and pinned corpus oracle to match the original PHP command. Two independent test passes produced the same coverage and output:

```sh
cd bench
WAGO_925_PROFILE=1 GOCACHE=/tmp/wago-go-cache go test -tags=wago_codegenstats ./suite -run '^TestLazyFunctionEntryProfile/php-buckets$' -count=2 -v -timeout=120s -args -wago.corpus=all
```

| One PHP input | Original | Unentered bodies replaced |
| --- | ---: | ---: |
| Wasm bytes | 13,622,978 | 4,796,478 |
| Wago native bytes | 30,164,957 | 3,877,764 |
| Wago Compile ms, two runs | 679.04 / 678.19 | 98.05 / 97.11 |
| Compile gross allocated bytes | 116,537,544 | 42,044,136 |
| Compile mallocs | 35,766 | 22,450 |

This quantifies headroom for *that input*, not an achieved speed or memory saving. The transformed Wasm is much smaller than the original; a correct lazy module must retain the original validated source or equivalent representation. Even the crude native-plus-original-source arithmetic is 3.88 + 13.62 = 17.50 MB versus 30.16 MB native alone, a **12.66 MB upper bound** before dispatch stubs, decoded metadata, mapped-page granularity, and existing metadata. The 97–98 ms compile includes validation of the *smaller altered module* and excludes validation of all original function bodies; adding the earlier ~221 ms original validation figure still does not model lazy compilation, synchronization, mapping, or first calls. No separate-process retained PSS/RSS or live first-use implementation was measured.

### Exact implementation boundary

The current AMD64 compiler patches every direct call's `rel32` target against the final `Entry`/`InternalEntry` offsets in one image (`compile.go:patchModuleRelocs`). At instantiation, local funcref/table descriptors receive absolute pointers derived from those entries (`instantiate.go`), including fast internal entries. `Compiled.freezeExecution` copies these arrays into the immutable execution snapshot, and artifact validation/serialization requires valid offsets inside the complete code image. `runtime.MapCode` seals a copied image W^X and registers one executable range; `Compiled.Close`/instance references own that range. A first-use function cannot simply append bytes or replace an entry in the current image: existing direct branches, table pointers, execution snapshots, serialized artifacts, unwind/GC maps, and live instances would still refer to the old target.

A sound minimal prototype therefore needs a stable entry stub for **every** potentially unresolved local call target before relocation and table setup; an instance-independent way for that stub to identify the compiled module and function; a bounded per-function `uncompiled/compiling/ready/failed` state with recursion and host reentry handling; publication of separately mapped RX function code and its GC/unwind/source metadata before a stub may jump to it; and code ownership that survives close while any call/instance is active. The current code-image/codec contracts have no such dispatch or publication channel. An incomplete `unreachable` or Go-only invoke stub would change behavior for direct/indirect calls and cannot be treated as the requested prototype.

**Disposition:** the low PHP entry coverage and input-specific ceiling justify a dedicated lazy-code architecture effort, but a safe #925 first-use compiler is not complete in this branch. Keep the PR draft as unfinished research, with exact blocker above. Do not claim a retained-memory win or ready review from the proxy.
