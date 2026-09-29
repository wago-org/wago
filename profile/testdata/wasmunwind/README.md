# Railshot direct-recursion unwind qualification

This fixture compiles a Wasm function through Wago's normal public API. Eight
recursive calls create nine guest activations. The deepest activation performs a
counted loop, then returns a checked integer or f64 result. Inlining is
disabled so the expected native depth is explicit. No custom native trampoline,
cgo, signal handler, or Go preemption override is used.

Run on a Linux/AMD64 host permitted to sample itself:

```sh
go build -tags=wago_profile -o /tmp/wasmunwind ./profile/testdata/wasmunwind
perf record --clockid mono -e cpu-clock:u -F 99 --call-graph dwarf,8192 \
  -o /tmp/wasm-unwind.data -- \
  /tmp/wasmunwind /tmp/jitted-wago-recursion reg maps
perf inject --jit -i /tmp/wasm-unwind.data -o /tmp/wasm-unwind.jit.data
perf script -i /tmp/wasm-unwind.jit.data -F ip,sym
```

Use a new output directory for each run. Select `reg`, `wrapper`, or `mixed` call lowering,
and `maps` or `none` for a metadata-free control. `fixture.json` records the mode,
completed work, duration, and metadata-loss status; `images.json` retains the
compiler rules and retirement history. Keep the generated ELF files available
when reopening a profile. Perf 7.0.14/libdw needs the `/tmp/jitted-` path prefix
described in [the native exporter fixture](../unwindprobe/README.md).

The fixture also saves `module.wasm` for capture through the public CLI. For the
integer `reg` mode, the equivalent checked workload is:

```sh
wago profile record --backend=perf \
  --module=/tmp/jitted-wago-recursion/module.wasm --export=recurse --args=8 --want=0 \
  --duration=2s --warmup=1 --include-code --unwind-maps --stack-bytes=8192 \
  --out=recursion.wagoprof
perf script -i recursion.wagoprof/perf.jit.data \
  --symfs "$(pwd)/recursion.wagoprof/symbols" -F ip,sym
```

This CLI path uses the effective default compiler configuration recorded in the
bundle. Unlike direct fixture output, it preserves JIT symbols in a relocatable
tree and removes the temporary discovery directory after capture.

The September 28, 2026 runs used Go 1.26.5 and perf 7.0.14/libdw on Linux/AMD64:

| Lowering | Compiler unwind maps | Nine guest frames | One guest frame | Outside guest code |
|---|---|---:|---:|---:|
| Register ABI | Present | 196 | 0 | 0 |
| Register ABI | Absent | 0 | 193 | 2 |
| Wrapper ABI | Present | 195 | 0 | 1 |
| Wrapper ABI | Absent | 0 | 196 | 0 |
| Mixed I32/F64 register ABI | Present | 194 | 0 | 0 |
| Mixed I32/F64 register ABI | Absent | 0 | 197 | 0 |

All six runs completed their result checks without metadata loss. The rules
come from Railshot's finalized frames and are converted to DWARF by the normal
jitdump exporter. These results qualify this direct-recursion path. They do not
qualify host/re-entry transitions, indirect/tail calls, exception paths,
inlined bodies, other signature shapes, ARM64, or complete mixed Go/Wasm stacks.
Compacted layouts have deterministic final-byte tests; these sampling runs use
the public API's ordinary compilation path.

A subsequent register-ABI run with entry-adapter rules completed 87,925 checked
invocations. Of 194 samples, 193 recovered all nine guest bodies, the entry
adapter, and the native runtime entry address; one sample was outside guest
code. This qualifies the ordinary adapter in this fixture. The separate
[compacted-adapter fixture](../adapterunwind/README.md) covers native sampling
through shared adapters and entries with shared return tails. Recovering the runtime entry address does
not establish unwinding across its foreign-stack transition.
