# Building wago with TinyGo

wago builds and runs under [TinyGo](https://tinygo.org) on `linux/amd64` with **no
cgo**. The decode → validate → codegen → execute pipeline works end to end: the
CLI and the public `wago` API run real modules (recursion, i64, floats, linear
memory, host imports, `call_indirect`) identically to the standard toolchain.

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

The standard (`!tinygo`) build is unchanged and keeps using the assembly
trampoline; the build tags select the right implementation automatically.

## Building

TinyGo on `linux/amd64` links with LLVM `lld`. Make sure `ld.lld` is on `PATH`
(`apt install lld`, or any LLVM toolchain).

```bash
# Build the CLI. Two flags worth noting:
#   -scheduler=tasks : use the cooperative scheduler (see "Scheduler" below)
#   -o wago          : do NOT use a .bin output name — TinyGo treats .bin as a
#                      firmware image and fails with "ROM segments are non-contiguous"
tinygo build -scheduler=tasks -o wago ./cli/wago

./wago run tests/testdata/fib.wasm --invoke fib 20   # => fib(20) = 6765
```

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
make build             # standard Go dev build -> ./wago
make build-release     # size-minimized TinyGo release CLI (~0.43 MB) -> ./wago
make tinygo-build      # TinyGo debug build (CI portability check) -> ./wago-tinygo
make tinygo-test       # run the runtime + public-API suites under TinyGo
```

## Binary size

`cli/wago`, linux/amd64:

| build | size |
|---|---:|
| `go build` (default) | 3.1 MB |
| `go build -ldflags="-s -w"` | 2.1 MB |
| `tinygo build` (default — includes DWARF) | 2.3 MB |
| `tinygo build -no-debug` | 0.68 MB |
| `tinygo build -no-debug -opt=z -gc=conservative` | 0.62 MB |
| &nbsp;&nbsp;+ `strip -s` (= `make build-release`) | **0.43 MB** |
| &nbsp;&nbsp;+ `upx --best --lzma` | **0.16 MB** |

TinyGo's *default* build is no smaller than `go -s -w` because it ships debug
info; the win is `-no-debug` (~3.4× smaller than `go -s -w`). The biggest levers,
in order: `-no-debug`, then `strip -s` (drops the symbol table), then `upx`
(roughly halves again, at a few-ms startup decompression cost). `-gc=leaking`
saves only ~10 KB over `conservative` and leaks; `-panic=trap` saves ~20 KB but
replaces panic messages with a bare `SIGILL` — neither is worth it, so
`make build-release` uses neither.

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
and not even linear memory itself — `Instance.LinearMemory()` hands back the raw,
zero-copy mmap `[]byte`, which is optimal. That benchmark measures the *host's*
access idiom, `binary.LittleEndian.{Put,}Uint32`, whose per-byte assembly + bounds
checks LLVM optimizes less aggressively than `gc`.

The typed accessors (`Instance.ReadUint32Le` / `WriteUint32Le` / …) do a single
bounds-checked aligned load/store and are **~2.2× faster than the `encoding/binary`
idiom under TinyGo** — 0.73 ns/op vs 1.57 ns/op (and faster than `encoding/binary`
on the standard toolchain too). Use them for hot host loops:

```go
v, ok := in.ReadUint32Le(off)   // not binary.LittleEndian.Uint32(in.LinearMemory()[off:])
in.WriteUint32Le(off, v)
```

None of this touches the wasm execution path, which runs wago's own JIT-emitted
machine code, not TinyGo-compiled Go.

`make build-release` uses `-opt=z` (size); it is already at parity above. `-opt=2`
trades ~size for a further ~15-20% on these wrappers and on compile-time Go (decode
/ validate / codegen) — pass `make build-release` a different recipe if you want
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
  `make tinygo-test` / CI gate will crash.

- **Test suites that probe standard-Go internals.** `stress_test.go` (morestack
  relocation, the `_Grunning` contract, adversarial concurrent `runtime.GC()`)
  and the external `WAGO_SPECTEST_DIR` spec harness are standard-Go-only. The
  runtime stress test is build-tagged `!tinygo`, with a TinyGo-appropriate
  counterpart in `stress_tinygo_test.go`.

- **Platform.** `linux/amd64` only, same as the standard build.
