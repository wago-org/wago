# Adjacent shared-memory store-to-load forwarding

Research date: 2026-10-14. Read-only investigation; no compiler changes,
compilation, benchmarks, remote operations, deployment, publication, or commit.

## Conclusion

**Allowed under the WebAssembly threads proposal's relaxed memory model, as
allowed-behavior refinement, for the precisely bounded plain-store/plain-load
case below.** Concurrent racing writes do not by themselves prohibit forwarding.
This conclusion is a reasoning argument from the formal rules, not a quoted
optimization permission or a machine-checked compiler proof.

**Forbidden under an additional promise to preserve every physical guest memory
operation.** The current PR prose reportedly makes that stronger promise. It
must be changed explicitly before adopting forwarding; conformance to Wasm alone
does not satisfy that promise. Profiling, instrumentation, or an embedding may
also impose stronger observation requirements.

## Specification status and primary evidence

The official [threads repository README](https://github.com/WebAssembly/threads/blob/main/README.md)
identifies its contents as a prototype specification and links its rendered
specification. That rendered document is labeled **2.0 + Threads, Draft
2023-10-10**. These are live primary proposal sources checked today, not a claim
that the relaxed-memory chapter is a newly ratified Core 3.0 chapter.

The [relaxed-memory chapter](https://webassembly.github.io/threads/core/exec/relaxed.html#relaxed-consistent)
requires observed execution behavior to correspond to a consistent pre-execution.
Its formal `hb-consistent` rule rejects reading a write when the read happens
before it, or when an overlapping write is strictly between that write and the
read in happens-before. `sc-last-visible` adds constraints involving atomic
accesses. `no-tear` constrains combinations of supplying writes. The accompanying
explanation explicitly says data races are defined, although nondeterministic;
this is not C/C++ race undefined behavior.

[Runtime time stamps](https://webassembly.github.io/threads/core/exec/runtime.html#relaxed-prechb)
distinguish total order from the happens-before partial order. Merely scheduling
another thread's access between two instructions does not establish a
happens-before chain between them.

The authors' primary paper, [Watt, Rossberg, and Pichon-Pharabod, *Weakening
WebAssembly*, extended draft](https://conrad-watt.github.io/papers/watt2019.pdf),
§5/Figs. 6–7 and §7.2, supplies the underlying model. It distinguishes defined
races from C++ undefined behavior, and discusses limits of broader mixed-size
hardware-compilation proofs. Its compilation discussion is not a proof of this
specific peephole, and this report does not use Clang's output as semantic
authority.

## Exact transformation and assumptions

Conceptually, transform only:

```text
i32.store(address, value)          i32.store(address, value)
r = i32.load(address)       ==>    r = value
CALL host(r)                       CALL host(r)
```

Both operations are ordinary non-atomic full-width i32 accesses to exactly the
same four bytes in the same memory instance. Address identity and stored-value
identity must be proved, including offsets and integer address computation.
Between them there is only pure, nontrapping computation: no memory access,
atomic, fence, callback, synchronization, yield, or custom instruction with
effects. The store, its check and trap, and the later import call remain.
Initially limiting this to fixed-size shared memories avoids a separate
memory-growth/check argument.

The [load/store instruction rules](https://webassembly.github.io/threads/core/exec/instructions.html#exec-load)
model shared accesses and their bounds checks explicitly. Removing an actual
load is justified here only because a successful store checked the identical
range; an out-of-bounds store must still trap before the call. A broader address
pattern, partial-width store, or skipped potentially trapping computation needs
a separate proof.

## Proof argument: insert a source read witness

Take an execution of the transformed code and reconstruct an execution of the
original code by inserting its omitted read R immediately after the retained
store S, before the later call. Let every byte of R read from S. Its address and
value match by hypothesis. S precedes R; no action between them can communicate
with another thread. Extend happens-before with this thread-local read without
adding synchronization edges. No write W can therefore satisfy S <hb W <hb R.
Existing event order and values remain unchanged. The new read writes nothing
and creates no atomic synchronization. All bytes choose one supplying store,
which satisfies any applicable no-tear restriction. The identical fixed bounds
remain valid after S succeeds. This reconstructs the same later call argument
and all other observations in a permitted source execution.

This is a subset argument: every resulting observation has a source witness.
It does not establish equality of all possible source and target outcomes.

For example, with initially zero x, thread A can execute `store x=1; r=load x;
host(r)` while thread B executes `store x=2`. An unoptimized run may report 2;
forwarding reports 1. Reporting 1 already has a valid source witness. The loss of
the 2 outcome is not a newly forbidden behavior. A demand to reproduce that
particular physical interleaving would be the stronger policy above.

## Coherence, tearing, and boundaries

- **Aligned plain i32:** alignment/tear constraints do not require observing a
  racing writer. Choosing all four bytes from S is a coherent, untorn witness.
  This does not say the original load is guaranteed to return S's value.
- **Unaligned or mixed-size competitors:** permitting additional torn outcomes
  does not invalidate the intact S witness. Nevertheless, keep the initial
  implementation restricted to exact full-width matching; forwarding part of a
  stored value or extending/truncating it requires explicit bit-level analysis.
- **Atomic accesses:** both candidate operations should initially be plain.
  Eliminating an atomic read can remove synchronization and SC constraints, so
  this proof must not be reused for it. An atomic store is outside this report's
  admitted transformation; retaining it does not make an atomic-load extension
  automatically safe. The proposal's atomic operations are sequentially
  consistent, rather than separately selectable acquire/release operations.
- **Intervening synchronization:** an acquire/release chain can order another
  write between S and R, invalidating the proof. Conservatively stop at all
  atomics/fences. This is about possible happens-before edges, not whether an
  instruction happens to emit a hardware fence.
- **Intervening host call/yield:** preserve the boundary and reload afterward.
  A host callback can itself overwrite the bytes; `store 7; host_write_9();
  load` must observe 9 in that single-threaded example. A scheduler yield alone
  is not universally a Wasm synchronization edge, but embedding and runtime
  obligations need a separate argument; this report excludes crossing it.

Host-function behavior is embedding-defined: see [runtime function
instances](https://webassembly.github.io/threads/core/exec/runtime.html#syntax-hostfunc).
The core ISA's [scope](https://webassembly.github.io/spec/core/intro/introduction.html#scope)
also separates embedding APIs from the core semantics. Neither source grants
permission to disregard stronger runtime or instrumentation contracts.

## Local source evidence and benchmark interpretation

At held experiment `f976b9bc8cb23a5217d5409846a52c22fa4d17bf`:

- `shared_store_forward.go:inspectFixedSharedStoredLoad` matches the immediately
  repeated pure address and full-width ordinary i32/i64 load. Its comment gives
  the own-write relaxed-model argument.
- `memory.go:memStore` retains `StoreIdx`, then skips only the matched load and
  pushes the stored register value.
- `compile.go` limits admission to a single defined fixed-size shared memory,
  memory32, explicit checks, no profiling, and the store-forward policy.
- `shared_store_forward_arm64_test.go` includes a callback-between-accesses test
  that writes 9 after storing 7 and expects 9; profiling coverage expects the
  optimization to be disabled. These are useful boundary checks, not a formal
  concurrency proof. The experiment also admits i64, which exceeds the initial
  i32 scope requested here.

The private source `0666c2e3eaa7536b3743ef6f36cf047e89828133` still routes threaded
scalar loads through the real indexed load path. Nothing was changed here.
The parent reports the earlier experiment was held for poor performance
repeatability; that is independent of the semantic conclusion in this note.

The local wasm.fyi `harness/adapters/native/src/aot.rs:249–256` generates an
identity callback with `noinline` (and GCC `noipa`) plus an opaque compiler
`memory` clobber. With the caller's candidate store/load both **before** that
call, forwarding them does not cross the callback barrier or erase the call.
That fact describes workload placement; C compiler behavior is not the proof of
Wasm legality. If the benchmark's contract promises a physical load per
iteration, that contract must be revised explicitly too.
