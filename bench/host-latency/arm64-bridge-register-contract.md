# ARM64 bounded bridge register contract

The bounded-segment proof does not authorize removing native floating register
saves. A direct production-bridge probe holds eight distinct bit patterns in
V8–V15, calls a real Go callback which overwrites those registers, grows its Go
stack and runs GC, then checks every pattern after native resumption. The current
scalar and fixed-view bridges preserve all patterns. Omitting the four restore
pairs corrupts V8 in both routes. The production bridge was restored byte-exactly.

The compiler's [bounded CFG proof](../../src/core/compiler/backend/railshot/shared/host_segments.go)
caps scalar work and cuts host-call edges; it permits numeric floating operations.
[Module admission](../../src/wago/host_segments.go) accepts numeric parameters,
results and locals, and the [compile admission](../../src/wago/api.go) publishes
that proof as a host-segment marker. Neither proves floating registers dead at a
native callback boundary.

ARM [host-call lowering](../../src/core/compiler/backend/railshot/arm64/call.go)
flushes operands, spills locals, and rederives globals after return. The
[local spill state transition](../../src/core/compiler/backend/railshot/arm64/localstate.go)
homes all dirty pins, using lazy reloads in STACK_REG mode. Those transitions
establish local-value coherence; they do not by themselves change the complete
native calling convention or the runtime bridge's preservation contract.
[Export adapter lowering](../../src/core/compiler/backend/railshot/arm64/compile.go)
also surrounds the internal entry with its own live state. A reduced-save variant
needs its own complete proof and admission marker, including callers and wrapper
state, rather than relying on a microbenchmark that happens to use few registers.

[The live bridge](../../src/core/runtime/inline_host_arm64.s) records the native
callee-saved prefix in the control frame before switching to the real Go owner
and restores it before native continuation. The regression invokes the actual
`CallWithHostBaseScalarBoundedLive` and `CallWithHostBaseFixedViewBoundedLive`
methods, with matching import/slot metadata, a straight-line bounded native
fixture, and off-heap control/trap/result buffers. A test-only Go ABI helper
explicitly zeroes V8–V15. All eight saved values are checked while parked and
again after continuation, across three calls per route.

Run the retained ARM probe:

```sh
go test -tags wago_inline_host_experiment ./src/core/runtime \
  -run '^TestInlineBoundedBridgePreservesNativeFP$' -count=3
go test -race -tags wago_inline_host_experiment ./src/core/runtime \
  -run '^TestInlineBoundedBridgePreservesNativeFP$' -count=3
```

M4 Max / Darwin 26.6.2 / Go 1.27.1: both routes pass, including race count3;
full normal and experimental runtime suites pass. Linux ARM64 cross-compilation
passes; it was not executed on Linux. No AMD64, Windows or TinyGo execution is
claimed for this ARM-only probe. The experiment removing restoration fails the
intended preservation assertion and was reverted before validation. No timing
or Wasmtime-parity claim is made for an invalid variant.

The initial fixture incorrectly used a potentially movable Go-stack result
buffer across a uintptr-based native boundary. Stack growth exposed that fixture
mistake; off-heap buffers now match Wago's actual entry ownership. Initial
failure logs are retained separately from the corrected passing fixture.

[Evidence](measurements/2026-10-05-m4-max/native-fp-contract/README.md) includes
source snapshots, passing and expected-failing logs, the omitted-restore diff,
restored bridge hash and cross-compilation metadata. The only retained code
addition is the experiment-tagged regression and its test-only helper.

The retained regression also runs with `WAGO_ARM64_NO_INLINE_HOST=1`, exercising
generic fallback resumption rather than skipping. Three race repetitions pass
for each mode. A subsequent bank-offset experiment was rejected: the actual
atomic yield fixture has a control base eight modulo sixteen, so the existing
relative FP offset104 already gives aligned pair accesses. Offset112 misaligns
that fixture's accesses. The saved-bank layout and production bridge remain
unchanged; see [shift evidence](measurements/2026-10-05-m4-max/fp-bank-shift-rejected/README.md).
