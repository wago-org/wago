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

## Native Windows qualification record

The original Windows AMD64 checked-suite failure at
`963065314d40338d88c1a48796694eec4c308ac5` remains under the historical
correctness hold. The [failed job](https://github.com/wago-org/wago/actions/runs/37921241870/job/113789755423)
used Go 1.27.1 on Windows Server 2025. The runtime reported 496 contiguous
16-byte span slots before `found pointer to free object`; detection occurred
while `TestMemoryCopyPatchScratchDoesNotAllocate` entered `AllocsPerRun`.
This signature is consistent with [Go issue #81238](https://github.com/golang/go/issues/81238),
which reports corruption after recovered hardware faults on affected Windows
hosts. The original job did not record its CPU or a memory-write witness, so
the signature does not prove that cause.

The PR head `904f97db0ac80a58ff82d9dcebff6a0ed461ea6e` already includes
[merged #949](https://github.com/wago-org/wago/pull/949), commit
`300a2ca9c0e1b88a19d03b4cceeaede7031e8f22`. That change replaces three
fault-based recovery fixtures with Go bounds panics and checks their cause.
The two files below are in `src/core/compiler/backend/railshot/amd64/`.
Their Git blob identities match exactly across the stated revisions:

| File | Original failed head and #949 parent `8097344a` | #949 merge, PR head `904f97db`, and main `1a6d91a8` |
| --- | --- | --- |
| `internal_error_test.go` | `ec0b86b0b9fb068cffc4eaa0f289adacff87548b` | `2ea2d67db11cf7927f5a52a05f1490f1bff4e316` |
| `source_context_enabled_test.go` | `517e3a0a9d4ba926265499f718a29031725e4941` | `a5ece2d03ab16db2da1f644e1b3cf5758b83d4ba` |

The latest [Windows AMD64 job for PR head `904f97db`](https://github.com/wago-org/wago/actions/runs/38457446922/job/115522368426)
passed the checked compiler, ordinary, diagnostic, profile, and guard-page
tests with Go 1.27.1. It tested merge
`ba08b3381e1318d12a18902ae3a4d536a242b20f`, with main parent
`069a26ea07bdd312b672d4fc56b989aa55e89e98`; it does not qualify a merge
with newer main. The #949 diagnostic record reports no AMX runner, so
qualification on an affected AMX host remains absent. These source and CI
facts support the fixture repair already present. They do not establish the
original corruption event or clear its historical hold. The #947 mapping
cleanup and passing retries also do not prove its cause or correction.
