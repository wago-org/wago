# #915: control scratch retention experiment

Base `209e448c392510a0325d5b282a0d86a776fb379c`, Linux AMD64, Ryzen 7 8845HS, Go 1.27.1, `GOMAXPROCS=1` for short serial benchmarks. The first branch commit adds measurement controls only. The second adds a private `WAGO_EXPERIMENT_CONTROL_SCRATCH_TRIM=1` prototype on AMD64 and ARM64; ordinary compilation keeps the existing path. All measurements below use the same decoded module and compiler configuration per pair.

## Prototype and coverage

When an oversized `ctrl`, `ctrlMerges`, or `ctrlRoots` backing exceeds 256 entries, the worker releases it after two consecutive functions with hinted control depth at most 8. The first shallow function retains reuse for deep/small/deep sequences. Dropped pointer-rich backings are cleared. A retained small frame backing also has its cached merge-slot indices cleared when a cold sidecar is dropped; a real jq compile exposed the stale-index failure in the initial prototype, and a focused regression now covers it. This is a bounded heuristic, not a proved optimal threshold.

The checked-in corpus depth control found at least one saturated (>=255) function in SQLite and QuickJS and four in PHP. jq peaked at hinted depth 189 and Lua at 98. Saturated hints do not reveal exact depth or retained bytes. A synthetic 768-deep function followed by two tiny functions retained **81,920 logical control-scratch bytes before the trim and 0 afterward** in a live worker; fresh/reused code and metadata matched. This is scratch accounting, not a measured RSS drop.

## Short before/after measurements

Commands:

`GOMAXPROCS=1 GOCACHE=/tmp/wago-go-cache go test ./src/core/compiler/backend/railshot/amd64 -run '^$' -bench '^BenchmarkControlScratchRealCompile' -benchtime=50ms -count=2 -benchmem`

The candidate command adds `WAGO_EXPERIMENT_CONTROL_SCRATCH_TRIM=1`. Baseline uses the first test-only commit (`c6ab1652`, production code identical to `origin/main`). Two samples per module; SQLite ran one iteration per sample and the others two. These are short directional checks on a shared machine, with run-order noise.

| Real module | Base ns/op | Candidate ns/op | Base B/op → candidate B/op | Base allocs/op → candidate | Native bytes, both |
| --- | --- | --- | --- | --- | ---: |
| SQLite | 86,318,279; 88,955,516 | 88,778,858; 88,152,852 | 7,561,192 → 7,689,160 | 3,422 → 3,446 | 4,121,558 |
| QuickJS | 44,800,740; 44,947,473 | 44,757,223; 44,811,205 | 4,089,616 → 4,113,656 | 2,161 → 2,171 | 1,941,262 |
| jq | 37,243,098; 37,930,652 | 37,621,256; 39,033,326 | 3,625,128 → 3,730,976 | 2,316 → 2,333 | 1,787,508 |

First baseline/candidate B/op samples are shown; second samples differed by at most 288 B and one allocation. The extra allocation traffic is repeatable and is a material negative result. Compile timings do not establish a benefit. A synthetic deep/tiny compile showed a large timing swing with identical B/op and allocs/op, so it is not counted as a practical win. The deep/deep control is in the benchmark source. Peak heap, RSS, GC pause, and ARM64 execution were not measured.

With the switch off and on, all three real code-image SHA256 digests and entry counts were identical; the checked-in digest test prints these values. Focused worker tests compare code, relocations, feature flags, source/unwind metadata, and native execution against fresh scratch. ARM64 test code cross-compiles but did not execute on this AMD64 host.

With the switch on, the full AMD64 backend package and a focused Wago runtime call test passed. The initial full `src/wago` run lacked the pinned official fixture. After `git submodule update --init --depth=1 tests/conformance/spec-v3` restored revision `9d36019973201a19f9c9ebb0f10828b2fe2374aa` and the repository's `scripts/bootstrap-wabt.sh` supplied verified WABT 1.0.41, `WAGO_EXPERIMENT_CONTROL_SCRATCH_TRIM=1 go test ./src/wago ./src/core/compiler/backend/railshot/amd64 -count=1 -timeout=180s` passed both packages (14.416s and 3.984s). `git diff --check` also passes. No independent reviewer was available in this task.

**Recommendation:** leave the switch off and this PR draft. The retention improvement is real in a constructed 768-depth history, but the real application samples allocate 24, 10, and 17 more objects per compile, respectively, with no credible runtime gain. Do not promote without a real workload demonstrating meaningful resident-memory or GC benefit that outweighs this churn. No host/guest boundary or artifact format changes are involved.
