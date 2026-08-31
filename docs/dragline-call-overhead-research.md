# Dragline/Railshot host-to-Wasm call-overhead research

Research snapshot: 2026-08-31. Sources are pinned to Wago `5e724d02`, wazero
`f4779551`, and Wasmtime `d8a0da6d` unless the source is a versioned specification.
This note concerns repeated synchronous host-to-Wasm calls in the no-cgo Go
runtime. It does not propose weakening trap, reference, close, re-entry, shared
memory, or cancellation semantics.

## Conclusion

Wago already has the right high-level shape: `PrepareFunction` resolves the
export, signature layout, wrapper/native entry, and eligibility once; the
fastest ARM64 path passes up to four integer arguments in registers and returns
one scalar directly. The remaining large, credible gains are narrower:

1. **Give AMD64 the ARM64 leaf and call-free/trap-capable Go-stack entries.**
   AMD64 still switches to the Engine foreign stack for every direct integer
   call, while ARM64 has separate Go-stack leaf and trap-capable transitions.
2. **Add prepared typed scalar handles, including floating point and mixed
   signatures.** Resolve and type-check once; return scalars directly instead of
   through `[]uint64`; select an ABI stub once instead of branching on widths,
   arity, and entry class on every call.
3. **Add a leased repeated-call session for trusted, sequential hot loops.** The
   session should hold the instance lifecycle/GC admission once, allowing each
   eligible call to skip the per-call close/admission atomic without racing
   `Instance.Close`.
4. **Cache only state proven stable.** In particular, cache the instance's
   `linMem` base in a prepared handle: Wago's `JobMemory` is mmap-backed and its
   address is explicitly stable. Continue reading mutable size/control fields
   from basedata, and retain refresh/rebind logic for imported, shared,
   multi-memory, GC, host-calling, signal-bounds, and re-entrant cases.
5. **Keep generic, contextual, and reference-bearing calls separate.** They can
   reuse resolved handles and caller-owned argument/result storage, but their
   checks are semantic work, not accidental overhead.

The most important design rule is to specialize by *proved entry class*, not by
wishful fast-path detection at runtime. A leaf may omit foreign-stack setup and
trap checks only if compilation proved it call-free and trap-free; a call-free
trap-capable function may use a minimal trap record; anything that can call,
re-enter Go, use shared native state, or grow an unknown stack stays on the full
transition.

## Applied ARM64 finding

The first measured bottleneck was narrower than the general architecture gap:
ARM64 signal-backed instances rejected every private prepared entry before the
existing `DirectLeafPrepared` proof could select the Go-stack leaf trampoline.
That forced even `tiny.add`, which is compiler-proven call-free, trap-free, and
memory-independent, through the complete guarded wrapper transition.

Allowing only `DirectLeafPrepared` and the already-supported
`DirectTrapPrepared` class to survive that bounds-mode rejection reduced the
five-sample median for signal-backed Dragline `tiny.add` on an Apple M4 Max from
69.14 ns/op to 6.843 ns/op: a 90.1% reduction, or 10.1x throughput improvement.
The result remains allocation-free and is 2.85x faster than the matched wazero
median of 19.50 ns/op. A guard-page regression test compiles a module that owns
linear memory, proves the selected export carries the leaf metadata, and checks
the direct result. This does not admit memory-touching, calling, reference,
host-reentrant, or otherwise unproved functions to the leaf transition.

## What Wago pays today

`Instance.Invoke` serializes the instance, creates an invocation ID, acquires
lifecycle and GC leases, probes a four-entry export cache, validates slot
counts, marshals values, optionally starts cancellation, binds/refreshes native
state, enters native code, checks traps, replays host calls, reconciles roots,
and decodes results. Those steps are visible in the current
[`invokeWithToken`](https://github.com/wago-org/wago/blob/5e724d02b33089b4588c2f0eec68e012990cdb16/src/wago/api.go#L4388-L4531).
That is the correctness-first general path and should remain one.

[`PrepareFunction`](https://github.com/wago-org/wago/blob/5e724d02b33089b4588c2f0eec68e012990cdb16/src/wago/prepared_function.go#L59-L140)
already moves export resolution, signature inspection, result layout, and native
entry selection out of the loop. Fixed-arity methods avoid constructing a
variadic argument slice, and the private path reduces lifecycle admission to a
logical-close check. The direct integer path still performs four width branches,
selects among entry kinds, loads `linMem`, checks the trap cell when required,
and returns a reused result slice
([ARM64](https://github.com/wago-org/wago/blob/5e724d02b33089b4588c2f0eec68e012990cdb16/src/wago/prepared_direct_arm64.go#L32-L117),
[AMD64](https://github.com/wago-org/wago/blob/5e724d02b33089b4588c2f0eec68e012990cdb16/src/wago/prepared_direct_amd64.go#L33-L72)).

The architecture gap is concrete. ARM64 has a Go-stack trap-free leaf entry and
a Go-stack call-free trap entry; only potentially calling functions move to the
foreign stack
([assembly](https://github.com/wago-org/wago/blob/5e724d02b33089b4588c2f0eec68e012990cdb16/src/core/runtime/engine_int_arm64.s#L5-L80)).
AMD64 has only the foreign-stack transition
([assembly](https://github.com/wago-org/wago/blob/5e724d02b33089b4588c2f0eec68e012990cdb16/src/core/runtime/engine_int_amd64.s#L5-L32)).
That makes AMD64 leaf/trap entry parity the first implementation target.

## Lessons from primary implementations

### wazero

wazero resolves an export to a persistent `callEngine` holding the executable
address, a per-type preamble, parameter/result counts, execution context, and a
reusable native stack
([source](https://github.com/tetratelabs/wazero/blob/f4779551afb474c7f2ac79929ce2b3390197544c/internal/engine/wazevo/module_engine.go#L161-L225)).
Its ordinary `Call` allocates a parameter/result slice, while `CallWithStack`
exists specifically so the caller can reuse storage and avoid that allocation
([API](https://github.com/tetratelabs/wazero/blob/f4779551afb474c7f2ac79929ce2b3390197544c/api/wasm.go#L391-L419),
[implementation](https://github.com/tetratelabs/wazero/blob/f4779551afb474c7f2ac79929ce2b3390197544c/internal/engine/wazevo/call_engine.go#L188-L249)).

wazero's full Wazevo call still clears exception state, installs a recovery
defer, optionally watches context cancellation, enters through the cached
preamble, and handles stack growth and other exit codes
([source](https://github.com/tetratelabs/wazero/blob/f4779551afb474c7f2ac79929ce2b3390197544c/internal/engine/wazevo/call_engine.go#L249-L355)).
The applicable lesson is not to copy that entire path; it is to make resolved
function objects and reusable parameter/result storage the baseline comparator,
then beat it with safely narrower entry classes.

### Wasmtime and Cranelift

Wasmtime explicitly documents typed functions as its fast API: type validation
happens once, avoiding dynamic checks and allocations on every call
([source](https://github.com/bytecodealliance/wasmtime/blob/d8a0da6d661605713798c1c9c76be5c28e3159ff/crates/wasmtime/src/runtime/func.rs#L130-L156)).
The typed path uses stack storage whose parameter and result representations
overlap, then invokes a cached `VMFuncRef`
([source](https://github.com/bytecodealliance/wasmtime/blob/d8a0da6d661605713798c1c9c76be5c28e3159ff/crates/wasmtime/src/runtime/func/typed.rs#L148-L222)).
`VMFuncRef` packages the callable address with callee context and calls through
one shared argument/result area
([source](https://github.com/bytecodealliance/wasmtime/blob/d8a0da6d661605713798c1c9c76be5c28e3159ff/crates/wasmtime/src/runtime/vm/vmcontext.rs#L551-L640)).

Even Wasmtime's unchecked call does not bypass runtime entry: it still installs
store state and trap handling around the cached function reference
([unchecked call](https://github.com/bytecodealliance/wasmtime/blob/d8a0da6d661605713798c1c9c76be5c28e3159ff/crates/wasmtime/src/runtime/func.rs#L975-L1034),
[entry/trap setup](https://github.com/bytecodealliance/wasmtime/blob/d8a0da6d661605713798c1c9c76be5c28e3159ff/crates/wasmtime/src/runtime/func.rs#L1449-L1483)).
Its first-party benchmark therefore separates typed, untyped, unchecked, sync,
async, and hooked calls
([benchmark](https://github.com/bytecodealliance/wasmtime/blob/d8a0da6d661605713798c1c9c76be5c28e3159ff/benches/call.rs#L82-L180)).
Wago should use the same matrix instead of reporting one blended "call
overhead" number.

Cranelift's IR distinguishes direct calls from signature-checked indirect calls
and provides a non-stable `fast` convention intended for internal performance
([IR reference](https://github.com/bytecodealliance/wasmtime/blob/d8a0da6d661605713798c1c9c76be5c28e3159ff/cranelift/docs/ir.md#function-calls)).
That supports Wago's existing adapter-free private-entry approach: use a custom
guest ABI behind small architecture assembly stubs, without exposing it as a
stable public ABI.

### Go ABI and stacks

Go's `ABIInternal` passes ordinary scalars in integer and floating-point
registers, but the specification explicitly says that ABI is unstable. Assembly
authors are directed to the stable ABI0 interface, for which the toolchain can
generate wrappers
([Go internal ABI specification](https://go.dev/src/cmd/compile/abi-internal)).
Wago should therefore keep a tiny ABI0-visible assembly boundary and translate
there to its private guest ABI; directly coupling generated Wasm code to the
current Go register ABI would create a toolchain-version correctness hazard.

Most Go functions include a stack-growth check; `NOSPLIT` omits it. Go also
warns that system/signal stacks do not grow and are not GC-scanned
([runtime stack documentation](https://go.dev/src/runtime/HACKING#hdr-Stacks)).
Consequently, Go-stack entry is appropriate only for compiler-proven bounded
functions whose guest execution cannot call arbitrary code or require stack
growth. General Wasm execution should continue to use Wago's owned foreign
stack, not `runtime.asmcgocall`: the Go runtime's implementation switches to the
scheduler stack and handles cgo-specific stack/G state that a no-cgo JIT does
not need
([AMD64 source](https://go.dev/src/runtime/asm_amd64.s),
[ARM64 source](https://go.dev/src/runtime/asm_arm64.s)).

## Recommended implementation order

### 1. AMD64 leaf/trap transition parity

Add `EnterPreparedLeafInt` and `EnterPreparedTrapInt` equivalents on AMD64 and
make the same compile-time entry metadata used by ARM64 select them. The leaf
stub should preserve Go's fixed registers, call the proved call-free/trap-free
entry on the Go stack, and return the scalar. The trap stub should additionally
publish only the minimal SP/return-PC state used by the cold trap path. It must
not admit guest calls, host calls, unbounded frames, GC safepoints, or signal
bounds until each interaction is proved safe.

This removes the foreign-stack save/switch/restore sequence from the most common
tiny-call benchmark without changing the general entry.

### 2. Prepared typed scalar handles

Add typed preparation APIs for the common shapes rather than growing
`PreparedFunction.Invoke` into a larger runtime switch. A practical first set is
0-4 parameters and 0-1 result over `i32`, `i64`, `f32`, and `f64`, with a raw
`uint64` fallback for uncommon/multi-value signatures. Preparation should:

- validate the exact signature once;
- cache native entry, stable `linMem`, entry class, and a bounded shared ABI stub;
- select i32 truncation/extension and FPR/GPR placement once;
- return zero or one scalar directly, not `[]uint64`;
- keep reference types on the checked general path.

Use a bounded table of architecture stubs keyed by signature shape and entry
class, not one generated stub per export. This keeps code size predictable while
moving width/arity/entry branches out of the invocation loop. It also extends
the direct path to floating point, which the current integer-only admission
rejects.

### 3. Repeated-call lifecycle lease

For callers that can promise sequential use, add an explicit closeable session
which acquires the instance invocation/GC lease once and prepares one or more
local exports. Calls through that session can omit the per-call logical-close
load and admission bookkeeping; session close releases the lease. Do not hold a
process-wide native-execution mutex across the session, and do not permit host
re-entry unless the existing parked-activation protocol is retained.

This is safer than an unchecked public raw-entry pointer: `Instance.Close`
continues to wait for a concrete lease, and the fast contract is represented in
the type/lifetime of the session.

### 4. Stable-context caching and cold errors

Cache `linMem` in the prepared handle. Wago documents the JobMemory mapping as
stable, including growable reservations
([source](https://github.com/wago-org/wago/blob/5e724d02b33089b4588c2f0eec68e012990cdb16/src/core/runtime/basedata.go#L48-L60));
memory size and other mutable controls remain in basedata. Keep the instance and
compiled module alive across the native call.

Keep trap decoding, error formatting, signature mismatch reporting, and closed
instance errors out of line. The success path should contain one predicted
closed/lease check, the ABI transition, and—only for trap-capable entries—one
trap-code test. Do not use Go `panic`/`recover` for ordinary native traps.

### 5. Preserve explicit slow features

Do not make every fast call pay for cancellation, hooks, references, or shared
state. `InvokeContext` already avoids installing cancellation machinery when
`ctx.Done()` is nil
([source](https://github.com/wago-org/wago/blob/5e724d02b33089b4588c2f0eec68e012990cdb16/src/wago/api.go#L4310-L4353)).
Keep contextual prepared calls as a separate API/path. Wasmtime similarly keeps
call hooks and entry/trap setup around calls, and wazero makes close-on-context
termination optional rather than unconditional.

## Correctness constraints

The WebAssembly embedding semantics allow external invocation to fail on a type
mismatch and require traps/exceptions to propagate; an embedder may move type
checks earlier only when the resulting invocation remains type-correct
([Core specification](https://webassembly.github.io/spec/core/exec/modules.html#invocation)).
Therefore:

- preparation may discharge signature checks, but only for the exact prepared
  instance/export;
- `call_indirect`/`call_ref` checks stay in generated guest code;
- memory base caching is allowed only because the backing address is stable;
- a successful fast call must leave all trap/exception state reusable;
- reference ownership, GC roots, host re-entry, cancellation, and Close races
  must fall back unless their lease/state protocol is active.

## Measurement and stop gates

Measure each change against both Wago's current prepared path and wazero's
*resolved* `api.Function`, using `CallWithStack` where the benchmark wants
caller-reused storage. Run alternating samples on ARM64 and AMD64 with frequency
scaling controlled. Report median, range, B/op, and allocs/op for:

| Case | Purpose |
| --- | --- |
| `() -> ()` trap-free leaf | irreducible transition cost |
| `(i32) -> i32` and `(i64,i64) -> i64` | integer ABI and truncation cost |
| `(f64,f64) -> f64` and mixed integer/float | FPR ABI coverage |
| call-free function with a cold trap | minimal trap publication/check |
| one internal direct call | foreign-stack boundary threshold |
| one memory load and one `memory.size` | cached base versus mutable context |
| multi-result and reference-bearing calls | fallback cost and correctness |
| Background context, cancellable context, hooks on/off | optional policy cost |

Also benchmark the transition stubs directly and inspect generated assembly.
Reject a specialization if it adds hot-path allocations, broadens unsafe entry
eligibility, regresses the full call path materially, or creates per-export
unbounded code/stub storage. Validate traps, stack overflow, `memory.grow`, close
races, host re-entry, signal bounds, GC/reference roots, and repeated recovery
before enabling any new entry class by default.
