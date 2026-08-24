# Building wago with TinyGo

wago builds and runs under [TinyGo](https://tinygo.org) on `linux/amd64`,
`linux/arm64`, and `darwin/arm64` with **no cgo**. The decode → validate →
codegen → execute pipeline works end to end: the public `wago` API runs real
modules (recursion, i64, floats, linear memory, host imports, `call_indirect`)
identically to the standard toolchain. Releases provide TinyGo builds of the
Standard and Minimal profiles alongside their standard-Go equivalents.
The version manager is a separate standard-Go binary with native HTTPS support.

## Why this needs special handling

The standard toolchain enters native wasm code through a Plan9 assembly
trampoline (`src/core/runtime/trampoline_amd64.s`) that switches `RSP` to a
dedicated foreign stack and calls the WARP `WasmWrapper`. TinyGo cannot assemble
Plan9 `.s` files, so that symbol is unavailable.

We do **not** fall back to cgo. A cgo trampoline would impose a boundary
transition on every wasm invocation — exactly the latency wago is built to avoid.
Instead, TinyGo generates the trampoline as machine code at run time
(`src/core/runtime/trampoline_tinygo_amd64.go`), the same way the engine already
maps native code, and enters it through an `unsafe` func-value cast:

- TinyGo lowers an indirect call through a func value to the System V C ABI. For
  `f(serArgs, linMem, trap, results)` the four arguments land in `RDI, RSI, RDX,
  RCX` — precisely the `WasmWrapper` register mapping — and the func value's
  context word is passed in the next register, `R8`.
- We smuggle the native code pointer through that context word (so it arrives in
  `R8`) and bake the foreign-stack top into the generated thunk as an immediate.
  The thunk switches `RSP`, `call`s `R8`, and restores the Go context — mirroring
  the assembly trampoline exactly.

On arm64, `trampoline_tinygo_arm64.go` generates the corresponding AAPCS64 entry
and host-call resume thunks with the repository's AArch64 encoder. The standard
(`!tinygo`) build is unchanged and keeps using the assembly trampolines; build
tags select the right implementation automatically.

Prepared integer exports have their own TinyGo direct-entry thunks on
linux/amd64, linux/arm64, and darwin/arm64. They accept the linear-memory base
plus up to four integer slots in the platform ABI, preserve the callee-saved Go
context, switch to the foreign stack, and enter the JIT function without the
general WasmWrapper marshalling path.

## Building

TinyGo on Linux links with LLVM `lld`. Make sure `ld.lld` is on `PATH`
(`apt install lld`, or any LLVM toolchain).

```bash
# Build the Minimal/Tiny runtime. Two flags worth noting:
#   -scheduler=tasks : use the cooperative scheduler (see "Scheduler" below)
#   -o wago          : do NOT use a .bin output name — TinyGo treats .bin as a
#                      firmware image and fails with "ROM segments are non-contiguous"
tinygo build -scheduler=tasks -tags wago_runtime,wago_lean,wago_minimal \
  -o wago-runtime-minimal-tiny ./cli/wago

./wago-runtime-minimal-tiny run tests/fixtures/wasm/fib.wasm --invoke fib 20
```

The `wago_lean` profile does not include `run --watch`. Watch mode needs the
full process-tree supervisor and periodic content scan. A lean build rejects
`--watch` as an unknown flag instead of using a smaller supervisor that can
leave guest processes running.

`wago_lean` also omits optional Railshot native compaction, producer-specific
SIMD/SWAR needles, and AMD64 native WasmGC optimization. Restore one subsystem
with `wago_railshot_compact`, `wago_railshot_needles`, or
`wago_railshot_gcopt`; add `wago_railshot_full` to restore all three. Untagged
standard builds retain the full compiler.

The standard-Go manager can use TinyGo for the final link of a runtime-only
command binary:

```bash
wago compile --tinygo --invoke fib fib.wasm -o fib
./fib 20
```

Like every `wago compile` build, this compiles the Wasm once with the native
standard-Go compiler, embeds the resulting `.wago` artifact, then links the
loader under the `wago_precompiled` build tag. `--tinygo` selects TinyGo for that
final link. The helper is pinned to the validated native
`GOOS/GOARCH` even when the caller has cross-compilation variables exported. It
also compiles under `wago_target_tinygo`, so Linux artifacts contain cooperative
interruption safepoints for the final TinyGo runtime instead of assuming the
standard-Go helper's signal unwinder will be present. The emitted executable
does not retain Railshot's source compiler and does not generate Wasm machine
code at startup. It retains the small runtime-owned host-call thunk emitter
required to bind imported functions during instantiation.

Precompiled standalone builds intentionally require the destination to equal the
build host's `GOOS/GOARCH`. Native artifacts incorporate platform admission and
interruption decisions as well as the instruction set.
Linux standalone outputs use the portable `strip -s` interface by default;
release packaging may apply additional pinned-toolchain ELF section removal.
The Linux TinyGo CI matrix builds this path with default stripping and verifies
that a context cancellation interrupts an artifact containing an infinite loop
on both AMD64 and ARM64.

## Scheduler: use `-scheduler=tasks`

Build wago programs with **`-scheduler=tasks`** (cooperative, single-threaded).
This is what `make tinygo-build` / `make tinygo-test` and CI use, and the config
in which the entire suite — including the standard-Go GC-storm stress test — is
green and deterministic.

The reason is structural. wago runs native wasm code on a dedicated off-heap
*foreign stack* (it switches `RSP` for the duration of a call). TinyGo's default
collector is conservative: under a *threaded* scheduler it can stop a thread that
is mid-run with `RSP` on the foreign stack and try to scan from there to the
thread's registered stack base — across unmapped memory — and crash. wago does no
Go allocation while native code runs, so under the cooperative scheduler (one
goroutine, no preemption, collections only happen between bounded runs) the
hazard cannot arise. This is the TinyGo analogue of wago's standing "keep native
runs bounded" contract; the standard Go toolchain sidesteps it entirely with
precise stack maps.

Via make:

```bash
make build                         # standard-Go CLI -> ./wago
make build-runtime-standard        # everything runtime
make build-runtime-minimal         # standard-Go run-only runtime
make build-runtime-minimal-tinygo  # TinyGo run-only runtime
make build-engine                  # diagnostic Minimal/Tiny runtime -> ./wago-engine
make build-release                 # CLI plus Normal/Tiny builds of all profiles
make tinygo-build                  # TinyGo Minimal portability build
make tinygo-test                   # runtime + public-API suites under TinyGo
```

## Binary size

Historical monolithic `cli/wago`, linux/amd64 measurements (remeasure the split
CLI and runtime artifacts independently). `make build-engine` is now a diagnostic
alias for the current run-only Minimal/Tiny profile, not a separate authoritative
product. Current Linux release packaging also removes TinyGo's unused
`.eh_frame`, `.eh_frame_hdr`, and `.comment` sections and the now-unneeded ELF
section header table from each Tiny runtime asset after linking. Darwin release
packaging removes TinyGo's retained local Mach-O symbol and string tables with
Apple `strip -x`; manager and Normal runtime artifacts are unchanged:

| build | size |
|---|---:|
| `go build` (default) | 3.1 MB |
| `go build -ldflags="-s -w"` | 2.1 MB |
| `tinygo build` (default — includes DWARF) | 2.3 MB |
| `tinygo build -no-debug` | 0.68 MB |
| `tinygo build -no-debug -opt=z -gc=conservative` | 0.62 MB |
| &nbsp;&nbsp;+ `strip -s` | **0.43 MB** |
| &nbsp;&nbsp;+ `upx --best --lzma` | **0.16 MB** |

TinyGo's *default* build is no smaller than `go -s -w` because it ships debug
info; the win is `-no-debug` (~3.4× smaller than `go -s -w`). The biggest levers,
in order: `-no-debug`, then `strip -s` (drops the symbol table), then `upx`
(roughly halves again, at a few-ms startup decompression cost). `-gc=leaking`
saves only ~10 KB over `conservative` and leaks; `-panic=trap` saves ~20 KB but
replaces panic messages with a bare `SIGILL` — neither is worth it, so the
Minimal-runtime recipe uses neither.

The biggest reliable levers are `wago_lean`, `-no-debug`, `-opt=z`, and stripping
symbols plus unused Linux unwind/comment and section-header metadata.
`-gc=leaking` is not acceptable because it leaks, and `-panic=trap` replaces
useful panic diagnostics with a bare `SIGILL`, so `make build-release` uses
neither. The `-opt=2` build is about 0.8 MiB larger than `-opt=z`; use it only when
measured compile-time speed matters more than footprint. UPX was not available on
the measurement host, so no current UPX size is claimed.

The fixed-arity prepared-call path is also footprint-bounded. The original
implementation at `78d8273a`, measured against `10ff9733` with the release build
and platform strip commands, had these effects. The rebased review fixes changed
thunk ownership and error handling; use the current PR build-size check for the
final artifact delta.

| release runtime | baseline | fixed-arity path | delta |
|---|---:|---:|---:|
| Darwin/arm64 Standard/Tiny | 2,000,240 B | 2,000,328 B | +88 B |
| Darwin/arm64 Minimal/Tiny | 1,900,480 B | 1,917,080 B | +16,600 B (0.87%) |
| Linux/amd64 Standard/Tiny | 2,239,992 B | 2,245,736 B | +5,744 B (0.26%) |
| Linux/amd64 Minimal/Tiny | 2,122,432 B | 2,127,952 B | +5,520 B (0.26%) |

## Call latency

The runtime-generated trampoline **adds no latency to the standard build** — that
path still uses `trampoline_amd64.s` unchanged (the TinyGo files are build-tagged
off). Measured identical to baseline: host→wasm 6.4 ns/op, wasm→host 14.4 ns/op,
0 allocs.

Under TinyGo the boundary-crossing round trips are at **parity** with the standard
toolchain. `enterNative` looks up its specialized trampoline through a lock-free
single-slot cache (`lastThunk`), so the hot path is one atomic load — no lock, no
map. (An earlier mutex+map lookup per call cost ~20 ns; removing it is a ~4×
speedup and the bulk of "optimize TinyGo".)

| benchmark (`src/core/runtime`) | standard Go | TinyGo `-opt=z` | TinyGo `-opt=2` |
|---|---:|---:|---:|
| `CrossBoundaryCall` (host→wasm) | 6.4 ns/op | 6.6 ns/op | 5.5 ns/op |
| `HostCall` (wasm→host, two crossings) | 14.4 ns/op | 16.0 ns/op | 12.9 ns/op |
| `LinearMemoryAccess` (`encoding/binary`) | 0.66 ns/op | — | 1.6 ns/op |

All paths are single-digit-to-teens nanoseconds with **0 allocations** under both
toolchains. The func-value-cast entry does the same `RSP` switch + `call` as the
assembly trampoline, so the boundary cost matches.

The `LinearMemoryAccess` row is the one apparent gap, but it is not the trampoline
and not even linear memory itself — `Instance.Memory().Bytes()` hands back the raw,
zero-copy mmap `[]byte`, which is optimal. That benchmark measures the *host's*
access idiom, `binary.LittleEndian.{Put,}Uint32`, whose per-byte assembly + bounds
checks LLVM optimizes less aggressively than `gc`.

The typed accessors (`Instance.ReadUint32Le` / `WriteUint32Le` / …) do a single
bounds-checked aligned load/store and are **~2.2× faster than the `encoding/binary`
idiom under TinyGo** — 0.73 ns/op vs 1.57 ns/op (and faster than `encoding/binary`
on the standard toolchain too). Use them for hot host loops:

```go
v, ok := in.ReadUint32Le(off)   // not binary.LittleEndian.Uint32(in.Memory().Bytes()[off:])
in.WriteUint32Le(off, v)
```

None of this touches the wasm execution path, which runs wago's own JIT-emitted
machine code, not TinyGo-compiled Go.

### Public API hot path

For a repeatedly called local integer export, resolve it once and use the
matching fixed-arity method:

```go
fn, err := instance.PrepareFunction("step")
if err != nil {
    return err
}
result, err := fn.Invoke2(state, input)
```

`PreparedFunction.Invoke0` through `Invoke4` avoid the argument-slice allocation
that TinyGo emits at a variadic call site. Eligible signatures (up to four
`i32`/`i64` argument slots and one `i32`/`i64` result) take the direct-entry
thunk above. The ordinary variadic `Invoke` remains the compatibility path for
dynamic arity, references, vectors, and larger signatures.

This distinction matters when comparing complete public calls rather than only
the low-level trampoline. Fixed prepared integer calls are allocation-free under
both toolchains and are the closest TinyGo public-API latency path. General
`Instance.Invoke`, host imports, and instantiation still carry more TinyGo
allocator/runtime overhead; do not infer their performance from the
boundary-only table above.

The table below measures complete public operations with the release TinyGo
flags (`-scheduler=tasks -no-debug -opt=z -gc=conservative`). Values are medians
of ten 300 ms samples. ARM64 is an Apple M4 Max; AMD64 is a Ryzen 7 7800X3D with
both binaries pinned to otherwise-idle core 7.

| public benchmark | ARM64 Go | ARM64 TinyGo | ratio | AMD64 Go | AMD64 TinyGo | ratio |
|---|---:|---:|---:|---:|---:|---:|
| compile small scalar | 6.47 us | 10.97 us | 1.69x | 15.24 us | 13.63 us | **0.89x** |
| instantiate small scalar | 0.976 us | 3.44 us | 3.52x | 1.89 us | 4.03 us | 2.13x |
| `Instance.Invoke` | 74.2 ns | 212.6 ns | 2.87x | 71.3 ns | 214.0 ns | 3.00x |
| indirect invoke | 75.6 ns | 226.7 ns | 3.00x | 72.4 ns | 231.2 ns | 3.19x |
| direct host import | 252.7 ns | 927.8 ns | 3.67x | 418.6 ns | 921.7 ns | 2.20x |
| prepared `Invoke1` | 16.43 ns | **14.24 ns** | **0.87x** | 6.66 ns | 11.45 ns | 1.72x |

Prepared `Invoke1` is zero-allocation under both toolchains. The TinyGo
variadic invoke paths still allocate five objects per call, host import calls
allocate 19, and instantiation allocates 66; those are the next parity targets.

`make build-runtime-minimal-tinygo` uses `-opt=z` (size); it is already at parity above. `-opt=2`
trades ~size for a further ~15-20% on these wrappers and on compile-time Go (decode
/ validate / codegen) — adjust the Minimal-runtime recipe if you want
it. Reproduce: `tinygo test -scheduler=tasks -opt=2 -bench=. -run=^$ ./src/core/runtime/`.

At max optimization for both toolchains (`go test -gcflags=all=-B` vs
`tinygo test -opt=2 -nobounds`, dropping bounds checks), **TinyGo wins the call
paths** — host→wasm 5.5 vs 6.4 ns, wasm→host 11.6 vs 14.3 ns — because its LLVM
codegen for the trampoline + wrappers is tighter and those paths carry no bounds
checks. Go keeps its ~2× edge only on pure host-side *memory loops* (0.57 vs 1.1 ns;
`-nobounds` closes most of TinyGo's gap there), which is off the wasm path.

## Limitations and caveats

- **Scheduler.** Build with `-scheduler=tasks` — see the section above. Under a
  threaded scheduler a conservative collection can scan a thread stopped mid-run
  on the foreign stack and crash; `TestTinyGoBoundedRunStability` (50k runs with
  inter-run `GC()`) confirms the cooperative path is stable.

- **Deeply nested modules.** The decoder/validator is recursive
  (`maxInstructionNestingDepth = 20000`). TinyGo goroutine stacks are smaller and
  fixed, so pathologically deep modules can overflow the stack before reaching
  the limit. Real-world modules nest nowhere near this; the main goroutine's
  large stack handles them fine.

- **Tests that shell out are excluded.** TinyGo does not support `os/exec`, and
  its `testing` package does not honor `t.Skip`/`t.Fatal` (they print
  "incomplete, requires runtime.Goexit()" and *keep running* instead of aborting
  the test). So a test that builds a fixture by invoking `wat2wasm` cannot skip
  cleanly — it falls through into a nil module and crashes. Such files
  (`src/wago/callargs_test.go`, `src/wago/pinnedglobal_test.go`) are build-tagged
  `!tinygo`; they still run under standard Go. The TinyGo public-API coverage
  comes from the embedded-fixture tests in `wago_test.go` (which read checked-in
  `.wasm` via `os.ReadFile`, no subprocess). **When adding a new test that uses
  `os/exec` or relies on `t.Skip`/`t.Fatal` aborting, tag it `!tinygo`** or the
  `make tinygo-test` / CI gate will crash. When only part of an otherwise
  TinyGo-compatible test file needs WABT, guard those test functions with
  `requireExternalWAT`; the TinyGo implementation returns before the unavailable
  helper can run while the rest of the file remains covered.

- **Test suites that probe standard-Go internals.** `stress_test.go` (morestack
  relocation, the `_Grunning` contract, adversarial concurrent `runtime.GC()`)
  and the external `WAGO_SPECTEST_DIR` spec harness are standard-Go-only. The
  runtime stress test is build-tagged `!tinygo`, with a TinyGo-appropriate
  counterpart in `stress_tinygo_test.go`.

- **Conservative-GC liveness.** Compiler metadata owners and asynchronous
  Runtime shutdown tasks are held through explicit value leases and final-use
  barriers. `tinygo_lifecycle_test.go` repeatedly collects across decode,
  compile, instantiate, close, and queued teardown boundaries. Keep these
  ownership points explicit when adding callbacks or replacing value state with
  closures; TinyGo boxes captured variables differently from the standard
  compiler.

- **Platform.** Native execution is covered on `linux/amd64`, `linux/arm64`, and
  `darwin/arm64`. Darwin/amd64 is a compiler/encoder portability target until
  wago gains a native Darwin/amd64 JIT ABI under either Go toolchain.
