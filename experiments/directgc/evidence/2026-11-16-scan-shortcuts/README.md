# Direct collector scan shortcuts

Collector-only diagnostics, not end-to-end Wasm or V8 measurements. No claim that
these changes meet the V8 target. Baseline source: 41e5d3b5, with the identical
tracing benchmark copied from the candidate. Retirement candidate: 871a76fc;
tracing candidate: 86651c2a, then b5b79060 for dense references. These small
independent candidates were subsequently integrated; combined performance has
not yet been measured.

Go 1.27 toolchain, Linux amd64, Intel Xeon Platinum 8573C. GOMAXPROCS=1.
Five fresh processes per candidate/control, alternating order each round.
Retirement: 150ms/subbenchmark; tracing: 300ms/subbenchmark. Build/test work was
completed before timings. No statistical significance claim from five samples.
Raw output is included. Header control ratios span 0.898–1.111, showing noise.

Median candidate/baseline retirement map time: 0.552–0.600 across eight shapes.
This includes common input restoration and measures map scanning, not a full GC.
Header traversal is unchanged. Scanning only even indices relies on 16-byte
nursery bounds and object strides; clear still covers the full map.

Pointer-free tracing median times (ns/op, baseline → candidate):

| graph | baseline | candidate |
|---|---:|---:|
| scalar struct | 14251 | 12914 |
| scalar array | 14060 | 13901 |
| reference struct | 19374 | 19333 |
| sparse reference array | 123368 | 126228 |
| dense reference array | 154748 | 152565 |

The sparse reference case regressed ~2.3%; the dense reference case did not.
The shortcut trusts immutable collector-owned pointer-free metadata, as do
existing allocation/collection contracts. It is not corruption hardening.
All native direct emitters reviewed publish matching flags; compact collectors
are separate and unchanged. TinyGo and complete integrated backend validation
remain necessary before reporting the overall GC checkpoint qualified.

Reproduce with `go test -c ./experiments/directgc`, then alternate baseline and
candidate binaries with `-test.run '^$'`, `-test.count=1`, and benchmark selectors
`^BenchmarkRetirementTraversal$` or `^BenchmarkFullTracingShapes$`.
