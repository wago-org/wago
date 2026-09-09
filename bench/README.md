# Benchmarks

The benchmark implementation lives in `bench/suite`; the workload definitions
and artifacts live in the repository-level `corpus` directory. The benchmark
module is deliberately separate from the runtime module so comparison-engine
dependencies do not become runtime dependencies.

Run benchmarks from the repository root through Make:

```sh
make bench                              # quick profile, all benchmark groups
make bench CORPUS=tag:compute BENCH=exec
make bench CORPUS=tiny,fib_rec BENCH=compile
make bench-all                          # every admitted workload
make bench-check                        # one iteration, wiring only
```

`CORPUS` accepts `quick`, `all`, `tag:<tag>`, or comma-separated benchmark IDs.
`BENCH` accepts `all`, `pipeline`, `compile`, `exec`, or a Go benchmark regex.
`BENCHTIME` and `COUNT` retain their usual meanings. `make bench-check` is a
correctness/wiring smoke test; its numbers must not be published as performance
results.

Every executable benchmark proves its catalog oracle before the timer starts.
Core exports compare exact return values, semantic workloads use their published
return/memory/vector oracles, and command workloads use self-checking exit status
or exact stdout/stderr hashes. A module that merely avoids trapping is not an
executable benchmark.

The default benchmark writes `bench/.bench-run.txt`. `make bench-chart`,
`make bench-website`, and `make bench-publish` consume that capture without
silently changing the selected corpus.
