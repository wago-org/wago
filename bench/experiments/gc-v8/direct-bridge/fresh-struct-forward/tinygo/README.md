# TinyGo compatibility qualification

Candidate a9750059 (production/test source 1e9dba8b), TinyGo 0.42.0 with
Go 1.27.1 and LLVM 22.1.4, Linux/AMD64. The existing local TinyGo toolchain
includes the compatibility patches used by earlier qualifications; this is not
an independent clean upstream-toolchain certification. No performance timings.

- Default directgc and compactgc core tests: PASS.
- Register checker and AMD64/ARM64 encoders with `wago_regalloccheck`: PASS.
- Runtime and public API suites with `wago_regalloccheck`: PASS. The public API
  build compiles the changed compiler and runs the supported TinyGo execution
  tests. These are the repository's tasks-scheduler TinyGo recipe checks.
- Full AMD64 backend test package: BLOCKED by three compile-time indexes into
  the zero-length TinyGo scratch arena in unchanged `compile_test.go` lines
  111, 125 and 130. Separate canonical control 1cb2b993 reproduces exactly the
  same errors; both logs retained. No test changes or skips conceal the error.

Scope: compatibility of the ordinary build, not active native-direct16 tagged
execution. The new direct pressure/bit/receipt fixtures exclude TinyGo; their
ordinary-Go instrumented results are separately retained. This does not claim
all GC configurations pass under TinyGo or that the entire backend test suite
passed. Test-reported durations are not throughput measurements.

`qualify.sh` reproduces the candidate commands using configurable `TINYGO`,
`OUT` and externally selected writable caches. Backend-control evidence uses a
separate worktree at the stated control commit.
