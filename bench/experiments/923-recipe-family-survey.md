# #923: offline instruction recipes — family selection remains open

Base: `origin/main` `209e448c392510a0325d5b282a0d86a776fb379c`.

`GOCACHE=/tmp/wago-go-cache go test ./src/wago -run '^TestInstructionRecipeFamilyCoverage$' -count=1 -v` counts static Wasm opcodes in six real modules. It is a first selection gate, not a backend cost or dynamic hotness profile.

| Module | i32 div/rem | i64 div/rem | FP min/max | Scalar memory |
| --- | ---: | ---: | ---: | ---: |
| SQLite | 367 | 136 | 0 | 76,503 |
| QuickJS | 132 | 41 | 2 | 29,991 |
| jq | 198 | 17 | 2 | 40,883 |
| PHP | 1,645 | 238 | 0 | 1,257,811 |
| Lua | 80 | 25 | 0 | 13,846 |
| yyjson | 14 | 22 | 0 | 5,080 |

Integer division is present, but Railshot already contains constant-divisor strength reduction (`magicdiv.go`) and adjacent quotient/remainder pairing (`divrem_pair.go`). FP min/max is scarce in this sample. Scalar memory operations are numerous but the current direct encoder is already a bounded method, and the count does not identify a costly missing recipe. The [Copy-and-Patch](https://fredrikbk.com/publications/copy-and-patch.pdf) and [TPDE](https://home.cit.tum.de/~engelke/pubs/2602-cgo1.pdf) papers describe broader compilation frameworks; their measured gains do not transfer to one Railshot lowering family. #923 explicitly excludes a general stencil or SSA backend.

No offline recipe generator or checked-in sequence was added without a measured real post-lowering hotspot and a concrete sequence that beats current emission. The focused test passes. Production source, static binary recipe table size, and native code are unchanged. Short unchanged-code `BenchmarkCompileCompareDivRemAMD64` control (`-benchtime=50ms -count=2 -benchmem`): value base `12217/12401 ns`, head `12050/12046 ns`, both `149 code-B`, `79 allocs/op`, about `16982 B/op`; branch base `13660/13571 ns`, head `12982/13789 ns`, both `166 code-B`, `81 allocs/op`, about `18503 B/op`. Timing differences are noise. Execution and peak memory for a recipe variant are unmeasured because no candidate was selected.

Recommendation: leave draft/inconclusive. Profile Railshot emission for a real corpus function, isolate one expensive existing path, then evaluate a small offline-generated sequence against the exact existing fallback and independent semantic tests.
