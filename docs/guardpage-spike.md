# Guard-page bounds-check elision (experimental spike)

**Status: experimental and opt-in behind the `wago_guardpage` build tag.** The
default build still uses explicit bounds checks; tagged builds can select
signals-based checks through `RuntimeConfig`.

This proves that wago can use the MMU to eliminate per-access linear-memory
bounds checks — the technique WARP uses on targets with passive memory
protection — **in pure Go, with no cgo**. Linux installs SIGSEGV/SIGBUS handlers
via raw `rt_sigaction` and assembly stubs; Darwin/arm64 calls libSystem's
`sigaction` through a dynamic import and uses an assembly signal-context
rewriter. Darwin deliberately does not install Mach exception ports: a Mach
receiver implemented as a Go goroutine can deadlock while all scheduler Ps are
inside `enterNative`, whereas signal delivery runs synchronously on the faulting
thread.

Supported tagged hosts are currently `linux/amd64`, `linux/arm64`, and
`darwin/arm64`.

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
3. **Handler** (`sigtrap_{amd64,arm64}.s`, installed by
   `InstallGuardTrapHandler`): a pure-asm SA_SIGINFO/SA_ONSTACK handler.
   It derives **everything per-fault** from the faulting thread — there is no
   per-call shared state:
   - It scans a registry of live reservations (`guardRegions`, populated by
     `NewJobMemoryGuarded`) for one containing the fault address. A fault outside
     every reservation chains to Go's saved handler, so real Go faults still
     crash/panic.
   - It validates the faulting ABI's `linMem` (`RBX`/frame state on amd64, saved
     `X26` on arm64) against the reservation. This rejects an unrelated fault
     that merely lands inside a live reservation.
   - A fault below the grown logical size lazily commits its 64 KiB wasm page.
     A true OOB fault writes `TrapLinMemOutOfBounds` and rewrites the saved PC to
     the architecture's existing native trap-exit landing pad. Darwin also marks
     the replacement arm64 PC as non-pointer-authenticated before signal return.
   - Faults that fail classification tail-chain to Go's saved handler. Darwin
     preserves distinct SIGSEGV and SIGBUS predecessors, keeping normal Go
     nil-fault behavior intact.

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

Darwin/arm64 measurement on an Apple M4 Max (`BenchmarkMemSumBounds`, 500 ms,
5 runs) was **334.8 ns explicit vs 330.8 ns guard at the median** (~1.2%), with
0 B/op and 0 allocs/op in both modes. The small delta is noisy, so Darwin guard
support remains experimental; the benchmark primarily locks zero-allocation and
no-regression behavior until larger real workloads are measured.

## Using it via the config API

Guard-page mode is selected through the wazero-style `RuntimeConfig` (see
`src/wago/config.go`); it is wired end-to-end through `CompileWithConfig` →
`Instantiate` → `Invoke`:

```go
cfg := wago.NewRuntimeConfig().WithBoundsChecks(wago.BoundsChecksSignalsBased)
mod, err := wago.CompileWithConfig(cfg, wasmBytes) // elides the inline checks
// err is non-nil unless the binary was built with -tags wago_guardpage.
inst, _ := wago.Instantiate(mod, nil)              // guard-page memory + handler
res, _ := inst.Invoke("f", wago.I32(addr))         // OOB faults -> trap, not crash
```

The default config (`BoundsChecksExplicit`) is unchanged; signals-based requires
a binary built with `-tags wago_guardpage` (the config layer rejects it
otherwise, so the flag is never a silent no-op).

## Run it

```
go test -tags wago_guardpage ./src/core/compiler/backend/railshot/ -run TestGuardPage
cd bench && go test -tags wago_guardpage -run '^$' -bench BenchmarkMemSumBounds -benchmem
go test -tags wago_guardpage ./src/wago/ -run TestConfigSignalsBasedEndToEnd
go test -tags wago_guardpage ./src/core/runtime ./src/wago
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

- **Owns process-wide SIGSEGV/SIGBUS handlers.** It preserves and tail-chains to
  separate prior handlers for non-wasm faults; applications that replace those
  handlers after Wago initializes still need coordination.
- **8 GiB virtual reservation per memory** (address space only, not committed);
  the live-reservation table is fixed at 256 entries.
- Signals-based instances accept owned or shared guard-backed memories and
  reject plain imported mappings; the layout requirement is never softened.
- `go vet -tags wago_guardpage` reports two warnings inherent to the technique
  (a frame-pointer clobber in the `leave;ret` stub; a `uintptr→unsafe.Pointer`
  for the mmap base). Default builds don't compile these files, so default vet is
  clean.

This confirms the route is real: the only thing standing between wago and
MMU-class memory performance is owning a signal handler — which pure Go *can* do,
at the cost of the no-signal simplicity the default design keeps.
