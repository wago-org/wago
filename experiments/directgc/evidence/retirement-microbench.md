# Nursery retirement microbenchmark

Source: commits 5d65fd9 + db88ce2, Go 1.27, linux/amd64, AMD EPYC 9V74.
Core tests passed default, start-map tag, and tagged race. Timings use
`go test -p 1 ./experiments/directgc -run '^$' -bench '^BenchmarkRetirementTraversal$' -benchtime=100ms -count=3`.

Each iteration restores identical start bytes before retiring a 2 MiB nursery.
That common restoration copy is included in both timings. This is NOT complete
GC, a cold-cache experiment, or a Wago/V8 benchmark. Reported MB/s uses nursery
bytes, not actual bytes touched by each algorithm. All measured cases allocate
zero Go bytes/objects per iteration.

| Shape | Header-walk median | Map-scan median |
| --- | ---: | ---: |
| 16-byte objects | 474.6 us | 93.3 us |
| 32-byte objects | 261.7 us | 96.8 us |
| 1040-byte objects | 13.1 us | 89.6 us |
| Mixed 16/32/80/272/1040 | 62.4 us | 87.8 us |

The map scan wins on densely packed small objects but loses severely on larger
objects. Keep it opt-in. Matching end-to-end collector workloads and controls
are required before adoption; no universal collection speedup follows.
