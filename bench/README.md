# Benchmarks

The benchmark implementation lives in `bench/suite`; the workload definitions
and artifacts live in the repository-level `corpus` directory. The benchmark
module is deliberately separate from the runtime module so comparison-engine
dependencies do not become runtime dependencies.

See [prepared semantic adapter schedules](semantic-adapter-schedule.md) for
vector operation units and the adapter work-count controls.

Run benchmarks from the repository root through `just`:

```sh
just bench                              # quick profile, all benchmark groups
just bench run algorithms exec          # representative raw algorithms
just bench run tag:polybench exec
just bench run tag:compute exec
cd bench && go run ./cmd/benchpub -corpus website -count 3 -benchtime 200ms -out out
just bench run tiny,fib_rec compile
just bench run all                      # every admitted workload
just bench check                        # one iteration, wiring only
```

`CORPUS` accepts `quick`, `website`, `algorithms`, `all`, `tag:<tag>`, or comma-separated benchmark IDs.
The `website` profile moves from tiny mechanisms through numeric, AssemblyScript,
semantic-library, and PolyBench workloads to Embench, Sightglass, and full command applications.
It includes only workloads that completed repeated runs on both published architectures.
`BENCH` accepts `all`, `pipeline`, `compile`, `exec`, or a Go benchmark regex.
The remaining positional arguments set count, duration, and output; environment
variables remain available for automation. `just bench check` is a
correctness/wiring smoke test; its numbers must not be published as performance
results.

Every executable benchmark proves its catalog oracle before the timer starts.
Core exports compare exact return values, semantic workloads use their published
return/memory/vector oracles, and command workloads use self-checking exit status
or exact stdout/stderr hashes. A module that merely avoids trapping is not an
executable benchmark.

There are no compile-only corpus entries. Compilation remains a measured stage
for every executable workload, but admission requires the same artifact to run
end-to-end and pass its oracle.

The default benchmark writes `bench/.bench-run.txt`. `just bench render`,
`just bench website`, and `just bench publish` consume that capture without
silently changing the selected corpus.

## Batched execution accounting

`BenchmarkExec` and `BenchmarkWazeroExec` prepare a callable function, check its
result, and calibrate a batch before timing repeated calls. These rows measure
warm repeated calls, not the first invocation. Compile, instantiate, export
lookup, oracle work and calibration are outside their timers.

For these rows, `ns/op`, `B/op` and `allocs/op` all describe one operation:
one prepared call for scalar entries, or the complete vector case-set for a
semantic vector entry. Go's iteration count is the number of batches; the legacy
`calls/batch` metric counts these operations, not individual vector-case calls.
Memory snapshots include the timed loop and timer bookkeeping; wall-clock
timing excludes that bookkeeping. They count Go allocation traffic for the
process, not retained memory, native allocations or RSS. Run performance samples without concurrent benchmark jobs.

`TestExecBatchAllocationUnits` runs the actual batch loop in an isolated child
with a fixed batch. It checks completed calls, excludes deliberate setup
allocations, and verifies both zero-allocation and 64-byte allocating controls.
`BenchmarkExecBatchAccounting` keeps those controls available for comparing the
measurement harness itself; they are synthetic, not guest workloads.
`BenchmarkExecBatchBoundary` uses a fixed batch and reports untimed reporting
cost per trial, including timer transitions. On Go 1.27.1 the corrected boundary
performs five memory-stat reads versus two previously: the two explicit snapshots
and one additional read from the changed timer sequence. Boundary bytes and
allocations include the full helper, while the guest metrics remain per operation.
These process-wide observations can include background traffic.

```sh
go test ./bench/suite -run '^TestExecBatchAllocationUnits$'
go test ./bench/suite -run '^$' -bench '^BenchmarkExecBatchAccounting$' -benchmem
go test ./bench/suite -run '^$' -bench '^BenchmarkExecBatchBoundary$' -benchtime=64x -benchmem
```

## Workload profiling

Use `wagoprof` for reproducible phase captures, native
code-image lifetimes, perf/jitdump and Samply export, and sampled hotness joined
to Railshot compiler statistics. Build from the repository root with
`scripts/build-profiler.sh /tmp/wagoprof`; run from `bench/` with
`/tmp/wagoprof record --workload json-as --iterations 1000 --out /tmp/json.wagoprof`.

The integrated profiling CLI uses the same implementation: from the repository
root, run `scripts/build-profiler.sh /tmp/wago --cli`, then
`/tmp/wago profile record --workload json-as --iterations 1000 --out /tmp/json-cli.wagoprof`.
Profiling commands are absent from ordinary manager and runtime builds.
