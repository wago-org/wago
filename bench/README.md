# Benchmarks

The benchmark implementation lives in `bench/suite`; the workload definitions
and artifacts live in the repository-level `corpus` directory. The benchmark
module is deliberately separate from the runtime module so comparison-engine
dependencies do not become runtime dependencies.

Run benchmarks from the repository root through `just`:

```sh
just bench                              # quick profile, all benchmark groups
just bench run algorithms exec          # representative raw algorithms
just bench run tag:polybench exec
just bench run tag:compute exec
just bench run tiny,fib_rec compile
just bench run all                      # every admitted workload
just bench check                        # one iteration, wiring only
```

`CORPUS` accepts `quick`, `algorithms`, `all`, `tag:<tag>`, or comma-separated benchmark IDs.
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
