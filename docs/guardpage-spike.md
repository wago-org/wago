# Guard-page bounds-check elision (experimental spike)

**Status: experimental, opt-in behind the `wago_guardpage` build tag. Not wired
into the default `Call`/`CompileModule` path.**

This proves that wago can use the MMU to eliminate per-access linear-memory
bounds checks — the technique WARP uses on targets with passive memory
protection — **in pure Go, with no cgo**, by installing its own SIGSEGV/SIGBUS
handler via a raw `rt_sigaction` syscall and an assembly stub.

## How it works

1. **Reservation** (`NewJobMemoryGuarded`): reserve ~8 GiB of virtual address
   space `PROT_NONE` with `MAP_NORESERVE` (4 GiB max wasm32 memory + 4 GiB+64 KiB
   to cover the max memarg offset reach), then commit (RW) only basedata + the
   used wasm pages. `linMem` is placed on a page boundary so — because wasm
   memory is 64 KiB-aligned — `linMem+size` ends exactly on a guard page, making
   the trap **byte-exact** for wasm despite page-granular protection.
2. **Codegen** (`amd64.ElideBoundsChecks`): `memEffectiveAddr` skips the
   `lea`/`cmp memBytes`/`jbe`/trap sequence and emits only the address
   computation + the load/store. An out-of-range `linMem+addr+offset` lands on a
   `PROT_NONE` page and faults.
3. **Handler** (`sigtrap_amd64.s`, installed by `InstallGuardTrapHandler`): a
   pure-asm SA_SIGINFO handler (SA_ONSTACK, with a raw `rt_sigreturn` restorer).
   It derives **everything per-fault** from the faulting thread — there is no
   per-call shared state:
   - It scans a registry of live reservations (`guardRegions`, populated by
     `NewJobMemoryGuarded`) for one containing the fault address. A fault outside
     every reservation chains to Go's saved handler, so real Go faults still
     crash/panic.
   - It then reads the wasm frame's saved `linMem` (`[RBP-16]`) and trap pointer
     (`[RBP-24]`) — wago's ABI stores both there — and only acts if `[RBP-16]`
     matches that reservation's linMem base. This rejects a wild non-wasm pointer
     that coincidentally lands inside a live reservation.
   - It writes `TrapLinMemOutOfBounds` to the frame's `*trap` and rewrites only
     the signal's saved **RIP** to `nativeTrapExit` (a `leave; ret`). On signal
     return that stub unwinds the faulting wasm frame into wago's existing
     **post-call trap-propagation** path, carrying the trap up through any
     nesting back to `CallGuarded`.

Because the handler needs nothing from the call site, `CallGuarded` holds **no
lock** and guarded calls run **fully in parallel**. The `leave; ret` bailout
reuses wago's normal trap unwind instead of a bespoke longjmp, so nesting "just
works" and there's no save-area/RSP rewrite to get wrong.

## Results

`BenchmarkGuardPageMemSum` (4096-load array sum, same wasm both ways):

| mode | ns/op |
|---|---|
| explicit bounds checks | 3566 |
| guard-page (no check) | 2686 |

**−24.7%**, 0 allocs. Tests cover in-bounds, OOB load/store → trap, page-exact
boundary, and reuse-after-trap on one engine; stable over 50+ runs and clean
under `-race`.

## Run it

```
go test -tags wago_guardpage ./src/core/compiler/backend/amd64/ -run TestGuardPage
go test -tags wago_guardpage ./src/core/compiler/backend/amd64/ -run '^$' -bench GuardPageMemSum
```

## Adversarial testing (`guardadversarial_test.go`)

Tried hard to break it; these all hold:

- **Fault propagation**: 3-frame-deep nested calls and a 2000-frame recursion
  both surface the trap cleanly (direct/indirect/host calls all go through
  `emitWrapperCall`'s post-call trap check, so the `leave;ret` redirect lands in
  the same unwind path).
- **Go faults still work**: with the handler installed, a genuine Go nil
  dereference still panics (chains to Go's saved handler) — it does not swallow
  real faults.
- **Reservation edges**: max u32 address, `addr + 2 GiB` offset, and high
  addresses all trap inside the 8 GiB reservation — none escape to another
  mapping.
- **Concurrency / GC**: 32 goroutines running **truly in parallel** (no lock),
  each with its own guarded memory and a distinct sentinel, show no cross-talk —
  every fault traps to its own buffer and every in-bounds load returns its own
  value; plus a 20k-iteration GC-pressure loop and 2000 create/close cycles
  (8× the slot table, no leak). The whole suite is clean under `-race`.
- **Straddling store**: a boundary-crossing `i64.store` does **not** partially
  write before faulting on this x86 (the fault is detected pre-commit). Note this
  is hardware behaviour for scalar stores; bulk `rep movsb` (memory.copy/fill)
  would partial-write — but those are **not** elided (only scalar load/store
  are), so they keep their explicit checks.

The earlier single-global-state hole (a wild pointer being misread as a wasm
trap) is **closed**: the handler now classifies by the reservation registry *and*
the `[RBP-16]==linMem` check, so a non-wasm fault inside a reservation fails the
linMem match and chains to Go. A residual theoretical case — a wild pointer that
lands in a live reservation *and* whose frame's `[RBP-16]` happens to equal that
reservation's linMem base — requires two independent coincidences and is not
reachable in practice.

## Limitations (why it's a spike, not the default)

- **Owns process-wide SIGSEGV/SIGBUS handlers.** It chains to Go's saved handler
  for non-wasm faults, but that chain is best-effort; a production version must
  forward robustly so Go's own nil-deref panics keep working.
- **8 GiB virtual reservation per memory** (address space only, not committed);
  the live-reservation table is fixed at 256 entries.
- **Not integrated**: only single functions (`CompileFunction`) via `CallGuarded`;
  not wired into `CompileModule` or the default `Call`. `memory.grow` would need
  to re-`mprotect` more pages and is not handled.
- `go vet -tags wago_guardpage` reports two warnings inherent to the technique
  (a frame-pointer clobber in the `leave;ret` stub; a `uintptr→unsafe.Pointer`
  for the mmap base). Default builds don't compile these files, so default vet is
  clean.

This confirms the route is real: the only thing standing between wago and
MMU-class memory performance is owning a signal handler — which pure Go *can* do,
at the cost of the no-signal simplicity the default design keeps.
