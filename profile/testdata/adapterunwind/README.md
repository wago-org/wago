# Compacted adapter unwind qualification

This Linux/AMD64 fixture compiles several exported functions with identical
signatures and distinct checked results. Each body runs a counted loop. It uses
Railshot's internal `CompactNative` rollout option and the production runtime
engine, arena, mapping, and native-entry APIs. It does not use a custom native
trampoline, signal handler, cgo, or preemption override.

```sh
GOOS=linux GOARCH=amd64 go build -tags=wago_profile -o /tmp/adapterunwind ./profile/testdata/adapterunwind
perf record --clockid mono -e cpu-clock:u -F 99 --call-graph dwarf,8192 \
  -o /tmp/adapter-unwind.data -- \
  /tmp/adapterunwind /tmp/jitted-wago-adapters delta maps
perf inject --jit -i /tmp/adapter-unwind.data -o /tmp/adapter-unwind.jit.data
perf script -i /tmp/adapter-unwind.jit.data -F ip,sym
```

Use a new output directory per run. Modes are `legacy` (five LEA/JMP entry
thunks), `delta` (six PUSH/JMP target-delta thunks), and `tail` (six adapters
sharing their result-store/return tail). The fixture checks that the requested
layout actually occurred. `maps` enables compiler unwind metadata; `none` is
the control. Keep the generated ELF files when reopening the profile. The
`/tmp/jitted-` prefix is required by the tested perf/libdw version; see the
[native exporter fixture](../unwindprobe/README.md).

September 28, 2026 results with Go 1.26.5 and perf 7.0.14/libdw:

| Layout | Maps | Checked invocations | Body plus adapter and runtime entry | Body only | Outside guest code |
|---|---|---:|---:|---:|---:|
| Legacy shared | Present | 63,033 | 195 | 0 | 1 |
| Legacy shared | Absent | 62,815 | 0 | 195 | 0 |
| Target-delta shared | Present | 87,945 | 193 | 0 | 0 |
| Target-delta shared | Absent | 87,222 | 0 | 194 | 0 |
| Shared return tail | Present | 75,260 | 196 | 0 | 0 |
| Shared return tail | Absent | 75,028 | 0 | 194 | 1 |

Every run passed result validation and retained load/retirement history without
metadata loss. Full sharing resolves the explicit `shared-adapter` identity;
tail sharing resolves each function's retained `entry-adapter` at the body call
return address. The six runs do not establish performance differences.

These samples qualify unwinding out of the hot bodies through their adapter
call sites. They do not sample every brief instruction in the thunks or return
tails. Instruction-walking compiler tests separately check PUSH/POP CFA changes,
all three result-pointer registers, and final jump targets. Reaching the runtime
entry address does not qualify its foreign-stack transition or complete mixed
Go/Wasm stacks. This fixture does not qualify host calls, cancellation, traps,
indirect/tail calls, inlining, GC/EH, or other architectures.
