# Integer leaf inlining with exact GC roots

## Problem and scope

The AMD64 compiler previously discarded every inline target when a module
used exact GC frame roots. Small integer helpers therefore kept native call
frames even when they could not allocate or access references.

Keep the existing inline candidate and size rules, then restrict the table
to straight-line i32/i64 leaves. Parameters, results and declared locals
must be integer types. Admitted instructions are integer constants,
local operations, comparisons, arithmetic, integer width conversions,
integer sign extension, drop, untyped select, nop and the final end.
Memory, globals, GC instructions, calls, control flow, reference operations,
floating-point operations and unreachable remain outside this new path.

At an accepted inline call, consume exactly one entry from the original
source call-mask stream, including the existing add specializations. There
is no native call to record and no new safepoint in the body. The source
root analysis and its mismatch diagnostics do not change. Existing inline
local reservation appends integer homes after the caller's original locals,
so it introduces no new collector root and preserves existing local homes.
Operand root offsets are still produced from the final caller frame.

Exact-GC callers retain their original call/frame/pin classification. The
existing all-calls-inlined analysis counts Wasm calls; it does not prove
the caller lacks collecting GC helpers. Address/type fact invalidation
at the original call remains unchanged.

The `gc-numeric-leaf-inline` codegen counter records accepted sites under
the GC frame-root plan. It is available with `wago_codegenstats` or
`wago_profile`, like the existing peephole counters. No name, source path,
or function ordinal controls admission.

## Regression proof

The runtime fixture keeps one reference local live across an allocating
call, executes an integer add leaf, creates a second object, executes an
i64 leaf with a declared local, then calls the allocator again. The second
call must retain both roots. Tests check:

- Four original call masks remain in the source plan.
- Exactly two numeric calls are inlined; two native calls remain.
- The remaining allocating calls carry exactly one, then two local roots.
- Root admission stays exact with inlining enabled and disabled.
- Values survive collect-every-allocation and Tiny collection, including
  codec reload.
- Memory/global/GC/call/control bodies and noninteger locals are rejected.
- Both signed integer divisions preserve dirty pinned globals on division by
  zero and signed overflow, including codec reload. Diagnostic tests require
  actual global value pins and one accepted inline call per division caller.

The existing versioned-loop root-plan fixture uses `memory.size; drop` in
its callee so it still tests a real, non-inlined call. Its original exact
callsite and safepoint assertions remain unchanged.

The diagnostic regression failed before the change: zero selected leaves,
where two were required. That run took 16.066 seconds including compilation.
Focused normal checks pass in 16.097 seconds; checks with
`wago_codegenstats,wago_regalloccheck` pass in 16.635 seconds.

```sh
go test ./src/core/compiler/backend/railshot/amd64 ./src/wago -run GCNumeric -count=1
go test -tags wago_codegenstats,wago_regalloccheck ./src/core/compiler/backend/railshot/amd64 ./src/wago -run GCNumeric -count=1
```

## Measurement

`BenchmarkGCNumericLeafInline` compares the same 8,192-iteration integer
loop with inlining disabled and enabled. Both variants require exact GC
root admission and verify the complete loop result. Setup is outside the
timed section. This isolates an integer helper call inside a GC module;
it is not a full Dewdrop measurement.

On an AMD Ryzen 7 8845HS, pinned to CPU 15 with GOMAXPROCS=1, five 200 ms
samples gave medians of 9,396 ns with inlining disabled and 3,818 ns with
inlining enabled: 2.46 times faster, or 59.4% less time. Both cases report
zero warm allocations and zero allocated bytes. The normal test binary
build took 12.306 seconds; the benchmark process took 3.098 seconds.

```sh
go test -c -o /tmp/wago-numeric-leaf.test ./src/wago
cd src/wago
GOMAXPROCS=1 taskset -c 15 /tmp/wago-numeric-leaf.test -test.run '^$' -test.bench '^BenchmarkGCNumericLeafInline$' -test.benchtime=200ms -test.count=5
```

A later full-job check applied only this production change to the installed
safety stack `03b40547187f9db18b9479ef2c9cc771bad1b15f`. Both hosts used the
same compiler and request bytes, fresh caches, CPU affinity 12,14, and
GOMAXPROCS=2. Task-owned builds and tests were paused. The initial order was
control/candidate/candidate/control, followed by three V8 runs; a further
control/candidate pair checked the slow first control. Every sample is kept.

| Run | Control seconds | Numeric inline seconds | V8 seconds |
| --- | ---: | ---: | ---: |
| 1 | 55.088876 | 46.016506 | 4.017356 |
| 2 | 47.116836 | 45.915911 | 4.024901 |
| 3 | 47.499023 | 45.686767 | 3.993433 |
| Median | 47.499023 | 45.915911 | 4.017356 |

The Wago median falls 3.33%; the candidate still takes 11.43 times V8's
median. This is a small single-host sample, not a general performance
bound. All nine output hashes match and return main=42. All six Wago runs
retain exact roots, 25,794 safepoints and the expected callsite counts.
Median peak RSS changes from 1,134,332 to 1,136,764 KiB; this small difference
does not establish a memory improvement. Combined-stack native code grows
from 26,901,064 to 27,059,224 bytes (+0.588%), while artifact metadata falls
from 3,642,299 to 3,311,043 bytes. Their combined size falls by 173,096 bytes.
These actual macro artifact sizes differ from the fresh-main codegen probe
below because the installed stack includes other native fast paths.
The request SHA-256
is `077b0ff0271706cc3034f3e06c7a228f227c49aeec7786d21373b2072af89c72`;
V8 is `13.6.233.17-node.53` in Node `24.21.0`. Raw identity, sample and
summary records are retained in the Dewdrop workspace under
`docs/research/data/wago-gc-followup-2026-10-21/`.

## Real compiler admission and full validation

A one-off public `Compile` probe on this fresh-main branch with
`wago_codegenstats` and
`WAGO_EXPLAIN=1` compiled Dewdrop compiler Wasm SHA-256
`c64fcf044b72cb7356ad598a35670045931337cfa7a6bb9d5916cd304b23fba6`.
The new counter reports 11,886 accepted sites in 1,798 caller functions.
Admission remains exact: 25,794 safepoints and 56,923 native callsites.
The latter is exactly 68,809 original callsites minus 11,886 removed calls.
Generated code is 22,419,789 bytes. The probe took 12.940 seconds including
its Go build; the test itself took 0.558 seconds. These are admission and
mechanism checks, not a guest execution benchmark. The temporary probe was
removed; permanent tests have no host-specific compiler path.

Full normal testing took 140.100 seconds. It found the old empty-callee
fixture described above and one transient plugin publication `ETXTBSY`.
After the fixture change, rerunning both complete affected packages
(`src/wago` and `cli/manager/internal/plugin`) passed in 31.692 seconds.
Full `wago_regalloccheck` testing took 117.693 seconds: all compiler,
backend, runtime and API tests passed. Two plugin publication tests hit
`ETXTBSY`; the complete checked plugin package passed on retry in
18.313 seconds. The pinned conformance corpora and interpreter were used;
PowerShell tests used `DOTNET_SYSTEM_GLOBALIZATION_INVARIANT=1`.

The optional full combined `wago_codegenstats,wago_regalloccheck` run
reported existing peephole-count expectations in unrelated integer backend
tests. `TestCommuteSelfUpdate` was also reproduced on the exact unchanged base
`cd029409bd8985ab51d30e5f68714d13d00192fa` in a detached checkout: the isolated
combined-tag check took 4.623 seconds and failed with the same zero-versus-two
peephole count. The focused numeric and loop
root tests pass with both tags, including strict inline-count, global-pin,
source-mask and exact-root assertions. No assertion was weakened to admit
an alternate root mode.

The separate `bench` Go module also passes its complete
`go test -tags=wago_regalloccheck ./...` run in 29.834 seconds. This is a test
suite result, not a benchmark measurement.
