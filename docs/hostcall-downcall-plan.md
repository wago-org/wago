# Plan: eliminate `resumeNative` (host-call "call-down")

Status: **feasibility spike done — naive approach proven GC-unsafe.** A real
implementation requires runtime-boundary integration (cgocallback-class work).

## Goal

Today a `wasm → host` call unwinds the whole native activation back to Go
(`hostCallStub` parks → `afterNativeCall` → `callWithHostLoop`), runs the host
function, then **`resumeNative`** re-enters native. That second Go↔native crossing
is the bulk of the round-trip cost. Goal: host call returns straight to the
calling wasm frame, no park / no `resumeNative`.

### Measured (M4, guard mode)
| Path | wago | wazero |
|---|---:|---:|
| host→wasm | 15.5 ns / 0 alloc | 20.7 ns / 2 alloc |
| wasm→host→wasm | ~63 ns / 0 alloc | 270 ns / 7 alloc |

Ablation of the 63 ns: register save/restore ~3 ns, Go marshaling ~3 ns, defers
~1.5 ns, per-call setup ~1–2 ns — **the remaining ~40 ns is the second crossing
itself** (park unwind + `resumeNative` re-enter). That ~40 ns is the prize.

## Why `resumeNative` exists

Native (wasm) code runs on a dedicated 4 MiB **foreign stack** (`Engine.stack`)
so its frames — which have **no Go stack maps** — never sit on a goroutine stack
where Go's GC would scan them or `morestack` would relocate them. The goroutine
stays `_Grunning`; `g` is kept live (X28 / TLS). To run *arbitrary* host Go code
safely (it may allocate, recurse, grow the stack, and be scanned by GC),
park/resume **fully unwinds the native activation back to a clean Go stack**,
runs the host there, and re-enters. `engine_unix.go:147` states this explicitly.

## The spike (performed and removed — finding preserved here)

A throwaway arm64 spike (`hostcall_down_arm64.{go,s}` + test, behind
`-tags wago_downcall_spike`) was built, run, and then **deleted** once it produced
the finding below. It kept the exact native stub from `TestHostCallRoundtrip` and
only pointed `hcTrampoline` at a `hostCallDownStub` instead of the park stub. The
stub, entered from native via `BLR`:
1. reads the goroutine SP that `enterNative` saved (`[linMem-24] → +0`);
2. switches RSP to the goroutine stack;
3. preserves the wasm activation's callee-saved regs (Go clobbers X19–X27, V8–V15);
4. `BL ·hostDispatchDown` — arbitrary Go, on the real goroutine stack;
5. switches RSP back to the foreign stack and `RET`s into native.

No g0 / no hardcoded struct offsets — it rides the goroutine's own stack, where
`g.stackguard0` is already correct.

**Result: `TestHostCallDownRoundtrip` PASSES** — the mechanism is functionally
correct (native → Go dispatcher → native, right result).

## The finding: the naive downcall is GC-unsafe

`BenchmarkHostCallDownRoundtrip` **crashes** under load:

```
goroutine N m=nil [preempted (scan)]:
runtime.newobject(...)
  runtime.hostDispatchDown()  hostcall_down_arm64.go
  runtime.hostCallDownStub()  hostcall_down_arm64.s
created by ... gcBgMarkStartWorkers
```

When GC preempts the goroutine while it is inside `hostDispatchDown` (any
allocation or safepoint in host code), it scans the goroutine's stack. The walk
goes `hostDispatchDown → hostCallDownStub → <return address into native>` —
`findfunc` on that native PC fails, and the native frames below have no stack
maps, so the scan dies.

**Root cause:** the downcall introduces a **Go safepoint (`hostDispatchDown`)
directly above native frames on the same goroutine.** Park/resume never does this
— during a host call the native activation is fully unwound, so GC only ever sees
a clean Go stack. Stack *depth* is irrelevant (the g0 / bounded-stack variants
don't help): the hazard is an un-walkable frame beneath a scannable one, not
overflow. `morestack` is a second, independent instance of the same problem
(relocating frames above `enterNative`'s saved raw SP).

## What a real solution requires

Removing `resumeNative` safely means telling the runtime **where the native↔Go
boundary is**, so GC's stack scan and traceback **stop at `hostCallDownStub`**
instead of walking into native frames — exactly the bookkeeping
`runtime.cgocallback` performs for C→Go. Options:

- **A. Genuine cgo entry.** Enter native through a cgo-compatible path so host
  calls are real `cgocallback`s; the runtime manages the boundary, stack, and
  scan. Correct for any host code, but adds cgo-class overhead to the *common*
  host→wasm path — the opposite of the goal.
- **B. Fake the cgo boundary.** On each downcall, set the g's cgo/syscall fields
  (`g.syscallsp`, `g.syscallpc`, the cgo unwind markers) so the scanner treats
  `hostCallDownStub` as the stop, then clear them on return. Most of the win with
  less per-call overhead, but rides undocumented runtime invariants and breaks
  across Go releases — high maintenance risk.
- **C. Don't remove it; shrink it.** Keep park/resume but cut its measured
  overhead (already did: FP-save removal, result-count packing). Remaining
  ~40 ns is control-flow-bound.

The "bounded shallow-import + park/resume fallback" idea from the first draft is
**withdrawn**: it does not address the GC-scan crash (which is depth-independent),
so it provides no safe fast path on its own. Any fast path still needs A or B.

## Prior-art research (decisive)

A survey of how other systems run non-Go code under Go's GC, cross-checked against
the Go 1.26.5 runtime source, settles the question.

**Correction to the framing:** async-preemption safety and GC-stack-scan safety are
**two different mechanisms**. `isAsyncSafePoint` (`preempt.go`) only inspects the
*top* PC — a hand-written asm / no-stackmap frame is never async-preempted, which is
why an asm trampoline is safe from *preemption*. But the GC **stack scan** walks
*every* frame via the unwinder and still `throw("unknown pc")`s on a native return
PC. Suppressing preemption does **not** make the scan safe. Our spike crashed
because `hostDispatchDown` is ordinary, preemptible, stack-mapped Go code — GC
preempted it at a safepoint and scanned down into the native return address.

**What wazero (pure-Go, no cgo) actually does — the key precedent.** Its optimizing
JIT does **not** run host calls in place. Compiled wasm runs with SP repointed to a
**non-pointer Go slice** (all wasm values are scalars, so the GC never walks wasm
frames as Go frames and no heap pointer is stranded), entry is via an
async-preempt-unsafe asm trampoline, and — critically — a host call's `exitSequence`
**restores SP to the real goroutine stack first, then returns to Go** to run the
host function as normal Go frames, and re-enters for the result. **That is exactly
park/resume.** The reference pure-Go engine reaches the same conclusion we did:
you cannot run arbitrary Go host code while native frames sit beneath it on the
scanned stack.

**cgo (wasmtime-go/wasmer-go, purego)** pays the full `cgocall`/`cgocallback`
boundary (`entersyscall` → `_Gsyscall` → run on g0 → `cgoCtxt` traceback threading):
correct, but ~40 ns/call plus P-release and signal-mask work — heavier than our
whole round-trip, and it's what we're trying to beat.

**The `_Gsyscall` bound is real but unusable for the host body.** Entering
`_Gsyscall` (via linkname-able `entersyscall`) does bound `scanstack` at
`g.syscallsp` and lets GC scan the goroutine *without stopping it*. But everything
between `entersyscall`/`exitsyscall` must be **nosplit, non-allocating,
pointer-free, and perfectly paired** — you must `exitsyscall` (back to `_Grunning`)
to run any real Go, at which point the scan hazard returns. It bounds a *foreign*
region, not a Go callback.

**The one future primitive that would actually work:** Go proposal
[**#78189 `runtime/jit`**](https://github.com/golang/go/issues/78189) (2026,
`GOEXPERIMENT=jit`, unmerged) — a registry of executable regions checked in
`initAt`/`next` *before* the fatal "unknown pc", plus a `ScanStack` callback wired
into `markroot` so a JIT reports **its own GC roots**, an `UnwindDeclare` for
tracebacks, and cooperative `Preempt()` polling — all while staying `_Grunning`.
This is precisely the "let native code stay resident across a host call" primitive.
Track it; do not build a shipping engine on it yet.

## Conclusion

**The fastest *safe* option is the optimized park/resume we already have.** This is
now backed by prior art, not just our spike: even wazero exits-and-re-enters for
host calls. Every route that deletes `resumeNative` is one of —
- **unsafe** (naive downcall, `TOPFRAME`-stop, hand-rolled `_Gsyscall`): GC walks
  into stackmap-less native frames → corruption;
- **slower on the common path** (real cgo boundary: ~40 ns + P-release);
- **a foundational rearchitecture** (wazero-style non-pointer value stack) that
  trades away the foreign-stack native-ABI speed the exec path depends on, and
  *still* parks for host calls; or
- **unmerged experimental runtime support** (#78189).

wago is already **4.3× faster than wazero and zero-alloc** on this path, using the
same fundamental pattern wazero uses. **Recommendation: keep park/resume, keep the
banked wins (FP-save removal + result-count packing), and stop here on removal.**
`resumeNative` is load-bearing for GC safety, not shaveable overhead.

If host-call latency ever becomes strategically critical, the only real paths are
(a) prototype against `GOEXPERIMENT=jit` / #78189 as it matures, or (b) commit to
the wazero-style value-stack rearchitecture as a deliberate project — measured, not
assumed, since it likely regresses exec.
