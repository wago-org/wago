# Rejected pre-wrapper native register thunk

Keep the existing native leaf wrapper. An experimental i32 register ABI entry used a branch thunk immediately before the unchanged wrapper, a separate dispatch capability, and caller-side argument/result loads. It preserved typed arithmetic validation and passed focused runtime tests, but actual prepared Invoke workloads were slower in every alternating pair.

Three alternating pairs on the shared Apple M4 Max benchmark lock measured ordinary compact imports at 687 µs before versus 732 µs with the prototype (about 6.6% slower). Native thread identity measured 360 µs before versus 421 µs (about 17.1% slower). Calls remained zero-allocation and passed their original workload oracles. Timing variation changes the exact size of the loss; no pair showed a gain.

The source patch, binaries and timing rows are archived privately. All prototype runtime, compiler and test changes were removed; no new ABI or prototype data was published. A future attempt must reduce actual call overhead and retain the minimal dependent callback loop control.
