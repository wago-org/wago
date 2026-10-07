# Integer state across bounded loop backedges

`src/core/compiler/backend/railshot/amd64/integer_loop_pressure_test.go` covers
repeated integer state updates for [#810](https://github.com/wago-org/wago/issues/810).
This is a test and benchmark contribution. Current main already computes the
expected results. It does not implement a compiler optimization or complete the
broader issue.

The profile has 12 shapes: i32/i64, 1/8/24 mutable states, and two division/shift
schedules. Each state starts from an input-dependent value. Each iteration
computes all next states using XOR, signed/unsigned division/remainder, variable
shift/rotate, and add. A short conditional precedes the next-state assignments.
A plain loop without this boundary was numerically correct, but compilation
streamed the updates and removed the intended transient operand pressure. The
conditional is a bounded fixture mitigation, not a production change.

Each shape runs 180 inputs, or 2,160 executions in total. Inputs include signed
boundaries, shift counts at and beyond the type width, and requests for 0, 1, 2,
and 7 iterations. A high-bit-bearing request checks normalization to zero under the in-guest
0..7 iteration bound; it does not independently prove i32 truncation. Divisors are +3 and -3, excluding divide-by-zero and signed
overflow traps. Distinct linear-memory cells expose every final state to a Go
oracle, using the existing independent width-aware division and shift helpers.
There is no reduction to a common checksum.

Guest counters independently record invocations, completed iterations, and the
odd/even conditional arms. Thus zero-iteration calls still have work evidence.
Checks cover all state cells, i32 cell padding, unchanged input arguments,
guarded result storage, surrounding memory guards, markers, and returned work
count. Intended-reason controls corrupt these observations. A real one-pass
substitute succeeds for one iteration and rejects a seven-iteration request;
its native code also rejects for lacking a backedge.

Compile options select one worker, explicit memory bounds and the baseline
AMD64 feature mask (0). The test maps and checks the exact native bytes before
calling the entry adapter through `Engine.CallPrepared`. The adapter calls the
internal function; the Wasm body has no calls. Ordinary numerical tests do not
require compiler diagnostics. The `wago_codegenstats` build records path,
code/frame size, pinning and spill/reload counts, and requires spill/reload
activity at high pressure. Those are whole-function compiler counters.

A separate GNU objdump check qualifies instruction layout. Exactly N divisions
and N shifts/rotates must lie after the loop-body marker and before the decoded
backedge. The backedge targets a decoded instruction at or before that marker;
a separate entry exit targets a decoded instruction after the backedge and at
or before the end marker. The high-pressure profile also requires frame-relative
move reads and writes inside that interval. Decoder controls reject bad targets,
missing/late arithmetic, missing frame traffic, calls and duplicate markers.

Decoded frame moves are not the same metric as compiler spills/reloads. These
checks do not prove full CFG reachability, per-value liveness, individual spill
lifetimes, original-source attribution or the transport contracts in #807.
GNU objdump absence explicitly skips the native layout gate. This scope does not
change #847's cyclic-copy scheduler or introduce loop-parameter permutations.

Run from the repository root:

```sh
go test ./src/core/compiler/backend/railshot/amd64 -run 'Test(IntegerLoopCarried|LoopPressure)' -count=1
go test -tags=wago_regalloccheck ./src/core/compiler/backend/railshot/amd64 -run 'Test(IntegerLoopCarried|LoopPressure)' -count=1
go test -tags=wago_codegenstats ./src/core/compiler/backend/railshot/amd64 -run 'Test(IntegerLoopCarried|LoopPressure)' -count=1 -v
go test ./src/core/compiler/backend/railshot/amd64 -run '^$' -bench '^BenchmarkIntegerLoopCarried$' -benchtime=100ms -benchmem
```

The benchmark uses schedule 1, low/high pressure, and seven guest iterations
per execution operation. Compilation includes decode, validation and native
code generation, excluding executable mapping. Execution reuses a prepared
instance. Setup and correctness checks are outside timing; the guest counters,
markers, state stores, and runtime call boundary remain timed. All resources
close at subbenchmark cleanup. `native-B` measures generated guest code, not the
release binary. Go allocation traffic excludes native mapping and retained RSS.

Always transplant the exact test onto the pinned baseline and compare identical
inputs, compiler, build tags and CPU conditions. Use short alternating pairs
and retain all samples, reporting medians, ranges and variance. This tests-only
comparison measures repeatability; unchanged source is not evidence of timing
equivalence. These synthetic stress profiles do not establish application wins.
Native ARM64, other value types and independent transport proofs remain separate
qualification work.
