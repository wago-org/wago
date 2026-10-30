# Stage 2 checkpoint evidence

Baseline: `6ceb86221`; first implementation/test checkpoint: `e98051264`.
All observations here are **tests**, not benchmark timings. No benchmark was run.
Parent owns the full-suite and subsequent serial benchmark run. Ordinary Go
commands below use `GOMAXPROCS=8 GOFLAGS='-p=8'` and `-timeout=10m`.

## Focused commands (pass)

```sh
go test ./src/wago ./src/core/compiler/backend/railshot/amd64 \
  ./src/core/compiler/backend/railshot ./src/core/runtime/gc/native \
  -run 'TestGCLifetime|Test.*Policy|TestGCConstructor|TestReserveDeadStruct' \
  -count=1 -v -timeout=10m
go test -tags=wago_regalloccheck ./src/wago \
  ./src/core/compiler/backend/railshot/amd64 \
  -run 'TestGCLifetime|TestGCConstructor' -count=1 -v -timeout=10m
go test -tags=wago_codegenstats ./src/core/compiler/backend/railshot/amd64 \
  ./src/core/compiler/backend/railshot \
  -run 'TestGCLifetime|TestLifetimeDiagnostic|TestProductionPolicy' \
  -count=1 -v -timeout=10m
go test -tags='wago_regalloccheck wagodebug' ./src/wago \
  ./src/core/compiler/backend/railshot/amd64 ./src/core/runtime/gc/native \
  -run 'TestGCLifetime|TestReserveDeadStruct' -count=1 -v -timeout=10m
go test ./src/wago -run '^TestGCLifetimeStage2' -v -count=1 -timeout=10m
```

The last command records expanded physical reservation observations in
`accounting.txt`. The first four logs are `focused.txt`, `regalloc.txt`,
`diagnostics.txt`, and `storage-debug.txt`. The active consumer is tested with
regalloccheck **without** wagodebug; the combined build separately verifies
injected-failure policy fallback. Replay mismatch tests call the actual consumer
entrypoint and verify rejection with no assembler/emission state and no error;
ordinary lowering continues on its false result. Corruption covers full body,
record offsets/ends, layout context, type, locals, signature and option policy.
Code/root identity tests cover baseline Off and rejected bodies and deterministic
intended On code changes with 1/4/8 compilation workers. Persistent collector roots
survive helper collections in small/stress/Tiny configurations. Scalar tests cover
all four numeric types, signed zero, NaN payloads, parameter forwarding, reference
alias overwrites and scalar-local overwrite provenance. Tiny 16-byte exhaustion
traps identically Off/On before charging any allocation. Existing constructor
trap/within-invocation exhaustion and native reservation/root tests also pass.

## Accounting observations

128 invocations of the **exact** Stage 1 qualifying expression, from fresh
instances (ordinary release build):

| Mode | Off/On charges | Off/On handleless | Off/On materialized objects and published handles | Off/On observed nursery advance | Off/On retained nursery end |
| --- | --- | --- | --- | --- | --- |
| throughput | 128 / 128 | 0 / 127 | 128 / 1 | 3072 / 3072 B | 3072 / 3072 B |
| small nursery | 128 / 128 | 0 / 124 | 128 / 4 | 3072 / 3072 B | 48 / 48 B |
| collect every allocation | 128 / 128 | 0 / 0 | 128 / 128 | 3072 / 3072 B | 24 / 24 B |
| disabled collection | 128 / 128 | 0 / 0 | 128 / 128 | 0 / 0 B | 0 / 0 B |
| Tiny, including stress | 128 / 128 | 0 / 0 | 128 / 128 | 0 / 0 B | 0 / 0 B |

Nonqualifying expression: 128 charges and materializations Off/On in all modes.
No-GC expression: zero charges/materializations Off/On. Results remain 42.
Each charged object has **24 layout bytes** (3072 total here). The throughput
heap allocator's Align16 rounded request would be **32 bytes**, not 24.
The existing nursery/native dead stub instead uses its own object-size/alignment
machinery (`alloc.go`, `emitNativeDeadStructChargeStub`); observed bump advances
here are 24 bytes per call in both Off/On. This observation is not a changed
rounding contract. Neither layout size nor size-class rounding should be called
measured physical reservation traffic. The test directly observes nursery bump
movement, and reports the end bump separately. In disabled/Tiny modes zero
nursery movement means those modes use other storage, not zero charged bytes.

Benchmark metrics are `guest-charges/op`, `layout-B/op`,
`throughput-rounded-B/op` (calculated size-class quantity, only applicable to
throughput heap requests), `handleless/op`, `materialized/op`,
`published-handles/op` (one per materialized fixture object),
`observed-nursery-advance-B/op`, and `retained-nursery-B` (absolute end snapshot).
Reservations can include chunk high-water space; positive bump changes and
post-collection bumps are accumulated per invocation, not confused with total
live payload. These are fixture-specific observations, not universal physical
allocator accounting. Materialized totals are charges minus successful native
handleless charges, sampled each call to distinguish cycle resets and fallbacks.
No inference from `Stats.LiveObjects`, Go allocations or handle capacity is used.
The pure cycle-reset test and actual small-nursery runs cover counter reset.

## Opt-in and runnable benchmarks (NOT run here)

```go
NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).
    WithOptimization("gc-lifetime-analyze", true)
```

Compiler users: `CompileOptions.Optimizations["gc-lifetime-analyze"] = true`.
The existing CLI `--gc-lifetime-analyze` uses that same option.
Native GC allocation and struct helpers must also be available. Default is Off.

Use the self-contained portable runner from the repository root. It archives a
ref, copies only `src/wago/gc_lifetime_stage2_amd64_test.go`, then invokes the same
Go binary/environment as the trusted checkout. Temporary exports and go-test
build products are cleaned; redirect stdout to retain measurements. Benchmark
compilation/instantiation is outside timing, and accounting uses a separate fresh
instance after the timed invocation loop. Throughput and Tiny each run Off/On for
qualifying/nonqualifying/no-GC bodies. Go B/op is host cost only.

```sh
# Compilation-only portability checks, already passed; no benchmarks:
python3 docs/rfwgc-evidence/stage2/runtime.py --revision 6ceb86221 --check-only
python3 docs/rfwgc-evidence/stage2/runtime.py --check-only

# Parent benchmark entrypoints; run serially after all testing ends:
python3 docs/rfwgc-evidence/stage2/runtime.py --revision 6ceb86221 > /tmp/baseline-runtime.txt
python3 docs/rfwgc-evidence/stage2/runtime.py > /tmp/stage2-runtime.txt
# Equivalent current-tree direct benchmark:
GOMAXPROCS=8 GOFLAGS='-p=8' go test ./src/wago -run '^$' \
  -bench '^BenchmarkGCLifetimeRuntime$' -benchmem -count=10 -timeout=10m
# The existing Stage 1 compile benchmark is still available:
GOMAXPROCS=8 GOFLAGS='-p=8' go test ./src/core/compiler/backend/railshot/amd64 \
  -run '^$' -bench '^BenchmarkGCLifetimeCompile$' -benchmem -count=10
```

## Limits and cleanup

No full allocation/charge erasure or runtime collector rewrite. Slow paths still
allocate real objects, and native handle batches still reserve identities. The
consumer rejects calls, control flow, escapes/unknowns, reference or packed
fields, multiple objects, parameter writes, EH/inlining/custom instructions,
profile builds and injected-failure builds. Runtime Tiny/stress/disabled modes
fall back. No source percentage is a Wago coverage result.

No full-suite/conformance qualification is claimed: pinned Release3 interpreter
bootstrap remains blocked by absent dune/OCaml, and WABT is 1.0.42 rather than
pinned 1.0.41. Previously recorded diagnostics24 failures remain baseline history.
No newly discovered sensitive engine bug was fixed. No push/PR/rebase/backlog
work; spec-v1/spec-v2 remain dirty and untouched. Read-only mirror fetches at
start, mid-cycle and evidence completion all returned unchanged Slipstream master
`db6014de2316f13dd08ea8618d614c28132f2688`.

Go test removed its temporary binaries/build trees; the portable runner used
TemporaryDirectory for every exported checkout. No build binary is retained in
the repository. Only source, scripts and text evidence are committed. Initial
preparation errors (invalid `-p8` spelling, archive mise trust, toolchain/GOROOT
mismatch) were corrected before the retained passing runs; they are not blockers.
