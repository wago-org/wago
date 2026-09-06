# Raw-call regression repair

Follow-up to [the audit repair results](audit-repair-results-2026-09-05.md).
The unchanged full `bench` binary reproduces the Go 1.27.1 slowdown. The fix moves
trap setup into its sole caller, `Engine.Call`, and expresses its existing
minimum-length check directly. The compiler can keep arguments live in registers
across that setup instead of passing two slices to a separate helper and then
reloading arguments for native entry. This removes a Go call and redundant tests;
it does not pad or realign the trampoline, change the benchmark, or change guest
code. `Engine.Call` stays at the same text address in the compared full binaries.
Its body shrinks from 870 to 837 bytes, and the 101-byte helper disappears.

The precise CPU mechanism behind the original sensitivity to the full linked
binary remains unproven. The source change removes real work and passes the
original full-binary benchmark on both Go versions. A full-width argument-store
experiment and a reordered trap-reset-loop experiment did not fix the slowdown;
neither experiment is included.

## Safety properties

The 24-byte trap-buffer check still runs before any state write or native entry.
Calls still reset stale non-interrupt traps, preserve atomic interruption,
clear the trap payload, bind the current trap pointer, and reset the exception
handler pointer. Empty linear-memory slices retain the prior behavior. The
atomic compare-and-swap loop, native scheduler handoff, foreign-stack code,
KeepAlive calls, and ARM64 signal ordering are unchanged.

`TestEngineCallTrapResetAndBinding` uses a native no-op that does not clear the
trap itself. It checks zero, stale, and interrupted traps, payload clearing,
binding replacement, and rejection of every short buffer length before binding
changes. Existing short-buffer, concurrent trap-reset, single-P GC, finalized
buffer-owner, GC/preemption stress, host reentry, cancellation, and close tests
provide the wider regression checks.

## Measurements

All timing uses prebuilt binaries, one benchmark process at a time, 12 rotated
or alternating samples, GOMAXPROCS=1 and -cpu=1. Raw-call samples run for 300 ms;
core, public, and other execution controls run for 200 ms. Host: Linux amd64,
AMD Ryzen 7 8845HS, kernel 6.12.101+deb13. Go versions: 1.27.1 and 1.22.12.
Host power/thermal settings are unchanged. An additional 300 ms sweep pins CPU 2.
No builds or tests run concurrently with timing. Raw artifacts and the runner
are in `/tmp/wago-raw-call-fix`; prior baselines are in `/tmp/wago-audit-repair`.

The audited baseline is `f0f4951138b8`. The pre-fix source is `5fa6aa7ec`.
Benchmark fixtures are identical across each comparison. All raw-call rows
verify fib(1) == 1 and report zero bytes and zero allocations.

| Full raw-call benchmark | Audited baseline ns/op | Before ns/op | After ns/op |
|---|---:|---:|---:|
| Go 1.27.1 | 25.61 | 36.12 | 21.84 |
| Go 1.22.12 | 31.56 | 29.10 | 27.95 |

The Go 1.27 result is 39.5% faster than the pre-fix branch and 14.7% faster than
the audited baseline. The Go 1.22 result is 4.0% faster than the pre-fix branch
and 11.4% faster than the audited baseline. All raw differences are significant
at p < 0.001 in these samples.

The core boundary control falls from 21.13 to 19.25 ns (-8.87%), with zero
allocations. Core host and linear-memory controls show no detected timing
change. Public InvokeAddOne, PreparedInvokeAddOne, host, and cross-instance
controls retain allocation counts; their initial timing uncertainty motivates
the separate CPU-pinned check. Direct scalar/prepared/cross-instance calls are
zero-allocation; the public host call retains its one scoped-handle allocation.

Raw fib-loop execution falls from 45.12 to 28.48 ns. Recursive fib, host
roundtrip, and JSON serialization/deserialization controls show no detected
timing change. The original benchmark selectors are retained.

To reproduce, build full benchmark binaries in `bench` with `go test -c`, using
the same Go toolchain and fixtures on both revisions. Alternate executions with
`-test.run '^$' -test.bench '^BenchmarkExecCallOverhead_wago$'
-test.benchmem -test.benchtime=300ms -test.cpu=1`. Use 12 samples per side.
Compare with benchstat; do not substitute a smaller linked test binary for this
gate. Core controls use `^Benchmark(CrossBoundaryCall|HostCall|LinearMemoryAccess)$`;
public controls use `^Benchmark(InvokeAddOne|PreparedInvokeAddOne|InvokeHostFuncDirect|InvokeCrossInstanceDirect|InvokeCrossInstanceIndirect)$`.

The CPU-2 run confirms the raw-call change: 37.29 to 22.75 ns (-38.98%), zero
allocations. The pinned public controls are:

| Operation | Before ns/op | After ns/op | Before/after allocations |
|---|---:|---:|---:|
| InvokeAddOne | 99.51 | 98.72 | 0 / 0 |
| PreparedInvokeAddOne | 17.99 | 17.96 | 0 / 0 |
| InvokeHostFuncDirect | 566.1 | 557.9 | 1 / 1 |
| InvokeCrossInstanceDirect | 109.0 | 108.3 | 0 / 0 |
| InvokeCrossInstanceIndirect | 110.8 | 109.4 | 0 / 0 |

The pinned host improvement is 1.45%; the other pinned public time differences
are not significant. These controls show no measured regression. Captures:
raw-go{127,122}-*, core-*, public-*, execution-*, pinned-{raw,public}-*.

## Verification and merge gate

After the change, the following pass on Linux amd64:

- Full core runtime `-race` suite on Go 1.27.1, including single-P foreign-call
  GC, buffer-owner finalization, and bounded GC/preemption stress.
- Focused public API `-race` tests for interruption, cancellation, deadlines,
  close during calls, reentry, arity, and nested host panic.
- Full core/runtime and public API tests on Go 1.22.12.
- `make test-guard`, `make test-concurrency`, `make test-corpus` (explicit and
  guard), `make test-semantic-corpus`, `make spec`, and `make spec3-signals`.
- `make tinygo-test` with TinyGo 0.41.1 / Go 1.22.12. The new trap-binding test
  also passes under TinyGo. Runtime/public test execution takes 0.038/3.143 s;
  the separate TinyGo LTO link takes over 30 s, confirmed by process inspection.
- `make lint-vet`, `make lint-fmt`, `make docs-check`, and `git diff --check`.

Runtime and public API test binaries also cross-build with CGO_ENABLED=0 for
Linux ARM64, Darwin amd64/ARM64, and Windows amd64/ARM64. No native execution on
those other hosts is claimed. The existing PR #562 native matrix must pass for
the pushed head before merge; its earlier green results belong to f0f4951138b8.
Full root installer tests and unrelated package race suites were not repeated for
this runtime-only follow-up; their audit results and host limitations remain in
the linked report. No benchmark was weakened or replaced to clear the raw-call
gate. The prior Go 1.27 raw-call performance gate is now resolved by these results.

Test captures: runtime-race.txt, public-race.txt, gate-*.txt, gates-status.txt in
`/tmp/wago-raw-call-fix`. WABT is pinned to 1.0.41; the Core 3 interpreter and
revision use `9d36019973201a19f9c9ebb0f10828b2fe2374aa` as in the audit report.
