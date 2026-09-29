# Native unwind exporter qualification

This is a Linux/AMD64 native fixture, **not Railshot-generated Wasm**. It emits a
34-byte frameless recursive function and invokes nine activations on a private
stack. The leaf spends most of its time in a counted loop. `unwind.hex` contains
little-endian DWARF32 `.eh_frame` followed by `.eh_frame_hdr`, with CFA changes at
native offsets 4, 19, 20 and 33. The return address is at CFA minus eight bytes.

Build from the repository root and use an environment permitted to sample itself:

```sh
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -tags=wago_profile \
  -o /tmp/unwindprobe ./profile/testdata/unwindprobe
perf record --clockid mono -e cpu-clock:u -F 99 --call-graph dwarf,8192 \
  -o /tmp/unwind.data -- env GODEBUG=asyncpreemptoff=1 \
  /tmp/unwindprobe /tmp/jitted-wago-unwind offline
perf inject --jit -i /tmp/unwind.data -o /tmp/unwind.jit.data
perf script -i /tmp/unwind.jit.data -F ip,sym
```

The output directory must be new. Modes are `none` (no unwind records), `offline`
(fixture frame tables exist only in jitdump), `mapped` (also place the same tables
after the aligned native code), and `rows` (generate offline DWARF from recovery
rows through the same encoder used for compiler metadata). Replace `offline`
with `rows` above to test that conversion. Disabling Go asynchronous preemption is specific
to this small foreign-stack test trampoline; it is not a profiler requirement.
Keep the generated ELF files available when reopening the profile.

With perf 7.0.14's libdw backend, the generated ELF pathname must start with
`/tmp/jitted-`. Its JIT-specific module-base workaround tests that literal prefix.
The same fixture under `/output/...` symbolized leaves but returned empty call
chains. This is a collector integration constraint, not a reason to infer frame
pointers or fabricate missing callers. The source locations that establish the
layout and path behavior are:

- [perf genelf.c](https://github.com/torvalds/linux/blob/master/tools/perf/util/genelf.c): `jit_add_eh_frame_info` consumes the frame table before the header.
- [perf unwind-libdw.c](https://github.com/torvalds/linux/blob/master/tools/perf/util/unwind-libdw.c): `__report_module` applies the `/tmp/jitted-` base-address workaround.

The September 28, 2026 qualification used Go 1.22.12 and perf 7.0.14 on Linux/AMD64.
The final `WriteUnwind` exporter run had 195 samples, all with nine recursive
frames. A no-metadata control had 191 samples with one recursive frame each.
Separate offline and mapped runs also recovered the recursive stack. This proves
the wire/layout integration for this fixture. It does not qualify Wago ABI
transitions, arbitrary prologue PCs, exception paths, tail calls, shared traps,
ARM64, libunwind, or complete mixed Go/Wasm stacks.

The row-generated path was then tested with Go 1.26.5 and the same perf/libdw
version: 186 samples recovered all nine recursive frames, with one sample outside
the fixture. `rows` explicitly marks other general-purpose and vector registers
unrecoverable, preserving the compiler metadata's unknown-register contract.
For actual compiled Wasm recursion, use [the Railshot fixture](../wasmunwind/README.md).
