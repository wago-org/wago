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
   It checks the fault address against the reservation; if it's a wasm access it
   writes `TrapLinMemOutOfBounds` to the call's `*trap` buffer and rewrites the
   signal's saved **RIP** to `nativeTrapExit` (a `leave; ret`). On signal return
   that stub unwinds the faulting wasm frame into wago's existing **post-call
   trap-propagation** path, which carries the trap up through any nesting back to
   `CallGuarded` — exactly like an explicit-check trap. Faults outside the
   reservation chain to Go's saved handler so real Go faults still crash/panic.

The `leave; ret` bailout reuses wago's normal trap unwind instead of a bespoke
longjmp, so nesting "just works" and there's no save-area/RSP rewrite to get
wrong.

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
- **Concurrency / GC**: 8 goroutines × 100 calls and a 20k-iteration GC-pressure
  loop run with no crash/corruption; the whole suite is clean 10× under `-race`.
- **Straddling store**: a boundary-crossing `i64.store` does **not** partially
  write before faulting on this x86 (the fault is detected pre-commit). Note this
  is hardware behaviour for scalar stores; bulk `rep movsb` (memory.copy/fill)
  would partial-write — but those are **not** elided (only scalar load/store
  are), so they keep their explicit checks.

The one real hole: the handler reads a **single global** reservation range, so a
genuine wild Go pointer landing inside the live 8 GiB reservation during a
guarded call would be misread as a wasm trap. Astronomically unlikely, but it's
why productionising needs per-M state, not globals.

## Limitations (why it's a spike, not the default)

- **Owns process-wide SIGSEGV/SIGBUS handlers.** It chains to Go's saved handler
  for non-wasm faults, but that chain is best-effort; a production version must
  forward robustly so Go's own nil-deref panics keep working.
- **Single in-flight guarded call.** The handler reads package globals describing
  the current call; `CallGuarded` serialises with a mutex. Concurrent guarded
  execution needs per-thread/per-M state (e.g. keyed off the ucontext).
- **8 GiB virtual reservation per memory** (address space only, not committed).
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
