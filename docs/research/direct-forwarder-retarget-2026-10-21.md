# Bounded direct forwarding calls

## Change and proof

Small forwarding functions add native frames even when their bodies only pass
unchanged parameters to another local function. The AMD64 direct-call lowering
now recognizes `local.get 0 .. N-1; call target; end`, with no declared locals.
Every parameter and result must have exactly the same WebAssembly value type
at every edge, including reference heap type and nullability. Equal machine
width or equal root classification is insufficient.

The matcher follows at most four edges, inspects at most 128 bytes in each
body, and requires a terminal non-forwarder within that bound. It reads at
most five bodies, including the terminal check. Cycles, imported targets and
type-changing forwarding edges reject the whole chain. It uses a five-entry
stack array and allocates no module-sized table. The `direct-forwarder` switch
controls this pass; `direct-forwarder-retarget` counts selected callsites in
diagnostic builds. No source names or module paths affect admission.

Retargeting runs after the existing inline attempt. It keeps one native direct
call and uses the final target's real local index and signature in the existing
ABI selector. The original source call mask is consumed exactly once by
`callInternal`; no root-mask preprocessing, call-frame classification, ABI,
dynamic type check, or handle check changes. The selected final signature is
checked again and a mismatch returns an explicit compiler diagnostic.

Wasm wrapper bodies, exported indexes, element/table contents and `ref.func`
identities are not rewritten. The pass does not retarget `call_indirect`,
`call_ref`, imports or `return_call`. A wrapper can still execute when called
through its export or reference; eligible direct edges inside that body can
skip further wrappers.

Normal return values, side effects, argument trap order and target trap source
remain unchanged. Native call stacks may omit up to four forwarding frames
per selected edge. Stack-exhaustion depth and optional frame detail can
therefore differ, as with existing inlining. Wasm does not prescribe that
native stack depth. A cycle is never collapsed into an unbounded jump.

## Regression tests

The diagnostic root test failed before the change: zero selected retargets
where one was required (12.888 seconds including Go build). It passed after
the change in 12.468 seconds. Final focused normal and combined
`wago_codegenstats,wago_regalloccheck` checks passed in 11.572 and 11.833
seconds respectively.

The GC fixture forwards a reference through four wrappers to an allocating
callee, keeps two independent local roots live, then keeps the returned
reference live across another allocating call. It requires exact root
admission and exact source/native mask counts with the switch enabled and
disabled. The two caller masks contain exactly two, then three local roots.
Tests run collect-every-allocation, forced major collection, Tiny collection
and codec reload.

Other tests cover one/four/five-hop limits, body size, short and long cycles,
imports, extra instructions, declared locals, reordered or repeated arguments,
reference-type variance and nullability. Runtime tests cover i32, i64, f32,
f64, v128 and multivalue target ABIs with register calls enabled and disabled.
A separate numeric-parameter/reference-result case enables the existing
module-wide tail-call register ABI and requires its mixed-register call
counter. Its returned reference survives forced collection alongside the two
independent roots. That staged ABI is tested on fresh artifacts: the public
codec currently rejects the required tail-call feature bit `0x100`. The
ordinary reference ABI retains the full public codec coverage above; no
codec check is relaxed and no failed load falls back to the input artifact.
Direct, indirect and call_ref paths retain distinct wrapper identity. Divide
traps preserve dirty value-pinned globals; an out-of-bounds argument traps
before the target's division. Fresh and codec-loaded artifacts retain the
actual target's Wasm trap source. Diagnostic tests require value pins and
prove indirect/call_ref sites receive zero retargets; their existing three
native branch callsites remain intact.

## Small benchmark

The benchmark makes 8,192 calls through either one or four reference wrappers
to a struct getter. It creates one object per invocation, verifies the entire
result, requires exact roots, and excludes compilation/instance setup from
timing. On an AMD Ryzen 7 8845HS, CPU 15, GOMAXPROCS=1, five 200 ms samples:

| Chain | Disabled median ns | Enabled median ns | Ratio |
| --- | ---: | ---: | ---: |
| One wrapper | 41,137 | 33,623 | 1.22x |
| Four wrappers | 72,426 | 33,018 | 2.19x |

The benchmark binary build took 12.262 seconds and the measurement process
5.113 seconds. These small-loop results do not establish full-compiler speed.

```sh
go test ./src/core/compiler/backend/railshot/amd64 ./src/wago -run '^TestDirectForwarder' -count=1
go test -tags=wago_codegenstats,wago_regalloccheck ./src/core/compiler/backend/railshot/amd64 ./src/wago -run '^TestDirectForwarder' -count=1
go test -c -o /tmp/wago-direct-forwarder.test ./src/wago
cd src/wago
GOMAXPROCS=1 taskset -c 15 /tmp/wago-direct-forwarder.test -test.run '^$' -test.bench '^BenchmarkDirectForwarder$' -test.benchtime=200ms -test.count=5
```

## Real compiler mechanism check

A private one-off public `Compile` probe compiled Dewdrop compiler Wasm
SHA-256 `c64fcf044b72cb7356ad598a35670045931337cfa7a6bb9d5916cd304b23fba6`
with `wago_codegenstats` and `WAGO_EXPLAIN=1`. It records 21,610 retargeted
sites in 2,557 caller functions. Admission remains exact with 25,794
safepoints, 68,809 native callsites and 2,291,438 bytes of root metadata.
All source callsites remain represented; this pass removes intermediary
execution frames, not source-plan entries. Generated code on this fresh-main
branch is 22,260,253 bytes. The process took 12.932 seconds including its Go
build. The temporary probe was removed; permanent tests have no host paths.

## Full validation

Full normal testing took 121.523 seconds and full `wago_regalloccheck`
testing took 117.085 seconds. Compiler, backend, runtime, API, plugin and
conformance tests passed. Both runs found only the missing generated schema
entry for the new optimization switch. `go generate ./cli/internal/settings`
updated `schema.json`; the complete normal settings/API rerun passed in
31.882 seconds. The complete checked settings/API rerun passed in
32.822 seconds. The additional reference-result ABI check passed with
`wago_codegenstats,wago_regalloccheck` in 10.763 seconds.

The separate `bench` module passed its complete checked test suite in
29.018 seconds. These are correctness-suite times, not performance results.
Tests used the pinned conformance corpora and interpreter, and
`DOTNET_SYSTEM_GLOBALIZATION_INVARIANT=1` for PowerShell on this host.

## Combined-stack full compiler job

The same compiler/request bytes and two-transform job run serially on CPUs
12,14 with GOMAXPROCS=2, fresh native caches and transform-result cache misses.
The control is the local safety/GC stack plus integer leaf inlining (PR1007).
The candidate adds only this production change. No task-owned build or test
runs during these measurements. All nine samples are retained in
[data](data/direct-forwarder-retarget-2026-10-21/).

| Engine | All wall times (s) | Median (s) |
| --- | --- | --- |
| Wago control | 45.358, 45.565, 45.440 | 45.440 |
| Wago with forwarding retargets | 42.074, 42.589, 42.232 | 42.232 |
| V8, same compiler and request | 3.957, 4.026, 3.990 | 3.990 |

The candidate median is 7.06% lower. It still takes 10.59 times the V8 median.
This is one shared host and workload; the result is not a general speed bound.
All output Wasm/cache hashes agree, Wasm validation passes and main returns42.
Offline probes retain Exact=true,25,794 safepoints and56,923 native callsites
on all six Wago artifacts. Generated code and metadata sizes are unchanged:
27,059,224 code bytes and3,311,043 metadata bytes. Retargeting changes call
addresses, not instruction counts. Median peak RSS is1,140,828KiB control and
1,137,932KiB candidate; this small difference is not a claimed memory gain.

## Project manifest switch follow-up

The generated settings schema accepted `direct-forwarder`, but project
manifest validation had a separate allowed-name list. Add the same option
there so a project can explicitly disable this pass. The existing catalogued
optimization regression failed before this fix in0.242 seconds with an
unknown-setting diagnostic. Complete project/settings packages pass after
the repair in9.954 seconds. Raw red/green results are in the data folder.
The fix changes configuration admission only; it has no speed claim.
