# Native struct sampling diagnostic

Reviewed source `b062646a`; decoder/lifetime unit tests pass. Two 6-second captures after 100 warmups, one locked invocation thread, GOMAXPROCS=1. Kernel/hypervisor CPU excluded; other Go threads excluded. Exact same-build source-map on/off emitted and loaded byte parity passed; checksum and allocation counts passed. No V8 run or speedup claim.

| Repeat | Samples | Lost | Native image % | Go symbol % | Other unknown % |
|---|---:|---:|---:|---:|---:|
|0|586|0|50.68|49.32|0.00|
|1|588|0|42.86|56.97|0.17|

## Native lowering origins

Wasm offsets were checked against function 3 body bytes in the exact artifact: 24 = fb00 struct.new; 35 = fb0e array.set; 42 = fb02 struct.get; 46 = i32.add. Percentages below use ALL sampled thread CPU weight, not just native weight.

| Origin | Repeat 0 % | Repeat 1 % |
|---|---:|---:|
|struct.new|24.91|18.20|
|array.set|18.94|15.48|
|struct.get|2.73|2.89|
|i32.add|2.22|3.91|

All observed native PCs mapped to source ranges, despite only 66.3% static byte coverage. This does not prove unobserved gaps are cold. Constructor plus array publication ranges account for 86.5% and 78.6% of native sampled weight. Scalar get contributes much less, making constructor/publication/barrier inspection the better next diagnostic than another get-forwarding speed guess.

The hottest repeated native PC is offset 1200 (0x4b0), inside array.set addressing after its bounds check. Disassembly is retained. CPU-clock software samples can skid and reflect adjacent dependency chains; this does NOT establish the shift instruction itself as costly. The source range includes validation, address calculations, remembered-parent logic and fallback code. No sampled call-stack or path-frequency claim is made.

## Go-side observations

Repeat 0, top flat Go symbol weights:
- `github.com/wago-org/wago/experiments/directgc.(*Collector).retireNurseryHeaders`: 12.46% of all sampled weight.
- `github.com/wago-org/wago/experiments/directgc.(*Collector).u32`: 9.22% of all sampled weight.
- `github.com/wago-org/wago/experiments/directgc.(*Collector).stride`: 7.00% of all sampled weight.
- `github.com/wago-org/wago/experiments/directgc.(*Collector).collect`: 3.24% of all sampled weight.
- `encoding/binary.littleEndian.Uint32`: 2.39% of all sampled weight.
- `runtime.memclrNoHeapPointers`: 2.39% of all sampled weight.
Repeat 1, top flat Go symbol weights:
- `github.com/wago-org/wago/experiments/directgc.(*Collector).retireNurseryHeaders`: 14.46% of all sampled weight.
- `github.com/wago-org/wago/experiments/directgc.(*Collector).stride`: 11.90% of all sampled weight.
- `github.com/wago-org/wago/experiments/directgc.(*Collector).u32`: 9.52% of all sampled weight.
- `github.com/wago-org/wago/experiments/directgc.(*Collector).collect`: 4.76% of all sampled weight.
- `runtime.memclrNoHeapPointers`: 2.55% of all sampled weight.
- `github.com/wago-org/wago/experiments/directgc.(*Collector).put32`: 1.87% of all sampled weight.

Go symbol lookup labels the interrupted PC, not a full/inlined call stack. Native unresolved weight includes these Go PCs; only subtracting the separately symbolized Go share leaves the other-unknown column. Do not add Go weight to native-unresolved as if disjoint.

## Limits and next step

Weights are software CPU-clock periods in nanoseconds, not hardware cycles, exact elapsed call costs or whole-process CPU. Roughly 600 samples/run give coarse precision. Own local jobs were paused but external host contention is not excluded. No default policy or performance claim follows. Native struct range priorities repeat, while total native/Go split varies. Inspect constructor and array-set emitted sequences and existing proof/barrier reuse opportunities, then test a single correctness-preserving hypothesis with independent throughput evidence. Retained maps are valid only for these captures; future processes need contemporaneous journals.

Raw samples, image lifetimes, source ranges, counters, binary/artifact/native hashes and decoder test logs are retained. No installation, signal-handler override, cgo or kernel/security-setting change was used.
