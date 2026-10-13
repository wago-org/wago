# #921: memory64 exact bounds proofs — real corpus gap

Branch baseline: `8097344a63a79e96230e09c883b2138232151370`.

`GOCACHE=/tmp/wago-go-cache go test ./src/wago -run '^TestMemory64WorkloadCoverage$' -count=1 -v` decodes all **121** shipped `corpus/workloads` Wasm files and checks local and imported memory types for 64-bit addresses. A known Wasmtime memory64 regression fixture is a positive detector control. Result: **0 real workload modules use memory64**, so there are 0 real memory64 functions and scalar load sites to qualify for this issue. The regression suite includes synthetic memory64 tests, but they cannot establish practical coverage or executed check frequency in an application.

Scalar loads are counted only when their decoded memory index resolves to a
memory64 type. Implicit indices select memory zero; imported memories precede
local definitions. `TestMemory64ScalarLoadCountMixedMemories` checks both address
width orders with local, mixed imported/local, and imported memories, including
implicit and explicit memory-zero loads and explicit memory-one loads. The
function metric is named `functions_in_memory64_modules`: it counts all local
functions in modules that contain memory64, not only functions that access it.
The corrected survey still reports 121 files and zero memory64 modules or loads.

Wago AMD64 `memAddr64` currently checks carry on offset addition and access-width addition before comparing with the current byte size. The [VMIL 2024 paper](https://home.cit.tum.de/~engelke/pubs/2410-vmil.pdf) studies a different runtime and discusses the overflow semantics that make simple 32-bit proof reuse unsafe. A memory64 certificate would need an unchanged 64-bit address value, effective offset, width, memory identity and growth epoch, with the first trap point preserved. No proof cache was introduced without a real application to measure.

The focused tests pass. This branch changes only the survey test and this
record relative to its branch baseline; it introduces no production bounds
optimization. Historical samples used the earlier baseline
`209e448c392510a0325d5b282a0d86a776fb379c`
(`GOCACHE=/tmp/wago-go-cache go test ./src/wago -run '^$' -bench '^BenchmarkStagedMemory64StoreLoad$' -benchtime=50ms -count=2 -benchmem`): reported base
`36.54/35.63 ns/op`, reported head `35.53/35.17 ns/op`, all `0 B/op`,
`0 allocs/op`. Production changes occurred between that historical baseline
and this branch's parent, so these samples do not establish an unchanged-code
control for the current branch. They establish no proof-cache gain. No current
branch performance comparison, real-application peak/resident memory, dynamic
check count, or measured code-size delta exists for this branch.

Recommendation: the survey is ready for review. Acquire at least one reproducible real memory64 application with repeated accesses before implementing a cache. Do not extrapolate from memory32 workloads or synthetic wins.
