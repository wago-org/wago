# #913: AMD64 leaf pin preservation — phase 0 coverage

Base: `209e448c392510a0325d5b282a0d86a776fb379c` (`origin/main`, fetched 2026-10-10). Linux AMD64, Ryzen 7 8845HS, Go test with `GOMAXPROCS=1`. This branch adds a reproducible coverage control and no production compiler change. It remains a draft: there is no measured real-workload opportunity for the initial conservative leaf class in the surveyed modules.

The control counts local functions with an integer register ABI, no declared locals, no calls, memory, globals, tail calls, SIMD, or exception handling. It uses existing function hints and the current inline target table. `incoming_call_sites` is saturated at 127 per target by the existing hint, so it is an upper-bound-oriented screening count, not a dynamic execution count. `non_inlineable_target_sites` counts those sites whose target the current inliner does not admit. The inliner replaces every call to an admitted target (`buildInlineCallerPlan`/`callOp`), so zero means no native call remains for this leaf class under the default policy. A synthetic 180-NOP leaf verifies the control detects one non-inlined eligible call.

| Module | Local functions | Eligible leaf targets | Incoming sites | Sites with non-inlineable target |
| --- | ---: | ---: | ---: | ---: |
| SQLite | 1,547 | 17 | 13 | 0 |
| QuickJS | 891 | 4 | 22 | 0 |
| jq | 719 | 12 | 121 | 0 |
| PHP | 12,592 | 31 | 4 | 0 |
| Lua | 778 | 14 | 42 | 0 |
| Brotli | 135 | 0 | 0 | 0 |
| AssemblyScript JSON startup twin | 39 | 0 | 0 | 0 |
| **Total** | **16,701** | **78** | **202** | **0** |

Reproduce: `GOCACHE=/tmp/wago-go-cache go test ./src/core/compiler/backend/railshot/amd64 -run 'TestLeafPinCoverage(Control|Corpus)$' -count=1 -v`.

Short unchanged-code reference benchmark (two samples each, 50 ms minimum/sample):

`GOMAXPROCS=1 GOCACHE=/tmp/wago-go-cache go test ./src/wago -run '^$' -bench '^BenchmarkCallPresence(Compile|Invoke)$' -benchtime=50ms -count=2 -benchmem`

| Work | Base ns/op | Branch ns/op | B/op; allocs/op both |
| --- | --- | --- | --- |
| Compile | 15,658; 15,688 | 14,142; 14,645 | 19,432; 96 |
| Invoke | 35.19; 35.23 | 34.05; 34.35 | 0; 0 |

These timing differences are noise or run-order effects: the branch changes only a `_test.go` file and this report. Production native bytes, instruction counts, executable size, compile memory, and execution semantics are unchanged by construction. The short samples do not establish a performance improvement. Peak/RSS and dynamic call counts were not measured. One EH-enabled MicroPython file did not validate under the default frontend feature set and was replaced by PHP; this is not a general corpus survey.

**Recommendation:** Do not add a pin-preserving AMD64 calling convention for this class on the surveyed default workloads. The broader #914 clobber-mask study is a separate, riskier path and explicitly depends on #913 coverage/correctness. If a future real workload exhibits hot non-inlined eligible calls, first inspect emitted register effects, then implement and qualify a narrow opt-in prototype with differential execution, GC/EH/tail cases, code bytes, compile resources, and repeated application benchmarks. Nothing here proves a safe call-preservation implementation.
