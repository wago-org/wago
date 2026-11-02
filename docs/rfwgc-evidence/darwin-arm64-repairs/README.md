# Bounded ARM64 repair recovery

Final tested source: `b9433aa2050c8069baa3edbaa50bd4e8e9c95b26`.
Recovered base: `4f1509996d5a3601f1bd6061b638153153c7f465` (includes d246 runtime).
Verdict: the assigned focused slices pass actual Linux ARM64 QEMU execution in
ordinary, wagodebug, and wago_regalloccheck. **Native Darwin parent rerun remains
required.** This is not Darwin, full backend, full CI, conformance, performance,
or port-inclusion qualification.

## Source slices

| Slice | Source commits | Verdict |
| --- | --- | --- |
| Dynamic import local-state join | `d8dc1a6dd`, execution supplement `a9151a44b` | PASS focused all three modes |
| Helper capacities, physical argument ABI and hints | `bc6242e66` | PASS focused all three modes |
| Worker scalar diagnostics | `02eaf8b91`, completion `b9433aa20` | PASS selected synthetic comparisons all three modes with diagnostics enabled |

The five-line inherited call.go diff was reviewed, not assumed correct. The
native-leaf eligibility predicate previously received `false` on the Go-host
branch, but `prepareGCFrameCallsite` returned true on the cross-instance branch
for an exact, zero-root unwind plan. Host restoration retained register-local
state while the cross-instance path homed it. Recovered `states.test` reproduced
`cross=2 host=0`. The repaired predicate also considers a candidate GC frame plan
before either alternative emits; such calls require return maps even with zero
roots. Ordinary noncandidate native shortcuts remain eligible. The production
join check stays, with the state pair added to its diagnostic. No blanket
fallback or weakened GC/capacity/clobber check was introduced.

The regression decodes and validates a two-import numeric module, keeps its
parameter live after the call, checks two distinct zero-root return maps, and
executes both tagged Go-host and different-instance fallback through the ARM64
host thunk. Each branch checks inputs 0, 7, 0x7fffffff and 0xffffffff: host result
is 3*x, continuation is 4*x modulo i32. The fallback uses different linear
memory and instance contexts. The regression also checks shortcut eligibility
with no candidate plan, with a recorded map, and with an unwind candidate.
An overlay replacing only call.go with its base blob causes the same committed
regression to fail at local 0 (negative.log, expected exit 1).

Bare helper fixtures now provision the 64-slot synchronous frame. Production
`callHostSync` still enforces its capacity. Externref table.fill uses GC helper
55 with (i32 destination, externref word, i32 count, i32 table index), four
parameters and no result. Its assertion checks exact emitted marshalling bytes:
physical spill slots 5/6/7/8 to control-frame offsets hcArgs+0/+8/+16/+24,
32/64/32/32-bit loads, helper dispatch, and packed arity. Two v128 values beneath
the operands consume four physical slots; the remaining logical depth and slot
checks stay. Ordinary inline bulk prefixes remain checked. Externref table.set
now expects call/loop-call/non-direct-call hints; funcref remains helper-free.

Worker comparisons exclude scalar scratch peak/retained/discarded resource
counters: `scratch.finishStackWorker` calls `scalar.FinishWorker`, while sequential
compilation retains reusable scratch. Per-function ScalarAdmissionNanos also
needs exclusion, just like CompileNanos (AMD64 already does this). The first
scratch-only patch still failed every selected diagnostics case; those logs are
retained as final-*-diagnostics.log. A deterministic comparator regression checks
input nonmutation and rejection of changes to code size, frame size, compiler
path and source offset. Full emitted-byte, Entry, InternalEntry and prepared-table
equality checks remain. Recursion/direct-call relocation outputs are exercised
by fib_rec and memory_tree; this work does not add a separate relocation oracle.

## Exact bounded census

| Lane at final source | ordinary | wagodebug | wago_regalloccheck |
| --- | --- | --- | --- |
| Focused | 24 PASS / 0 FAIL / 0 SKIP | 24 / 0 / 0 | 24 / 0 / 0 |
| Diagnostics (`wago_codegenstats` added) | 8 PASS / 0 FAIL / 0 SKIP | 8 / 0 / 0 | 8 / 0 / 0 |

Counts include parents and subtests, not unique workloads. Focused selects 11
parent tests. Diagnostics selects two parents, five ordinary synthetic workloads
(tiny, fib_rec, dispatch, many_funcs, memory_tree), and many_funcs with shared
adapters. The ordinary worker comparison checks workers 2/4/8, five repetitions
each, against workers=1. The shared-adapter comparison checks workers 4 versus 1.
The AssemblyScript cases were intentionally outside this bounded selector.
All six builds and executions exited zero. No benchmarks were selected.

`run-review.sh` records exact build and execution commands. It uses an immutable
Git archive of the final source, a private home cache/export, explicit Go 1.27.1
GOROOT, CGO_ENABLED=0 and Linux/ARM64. The QEMU binary/version and test binary
hashes are in receipt.json. All 17,790 exported tracked blobs were checked
against Git blob hashes; the compressed source manifest also records SHA256.
Submodule contents were neither exported nor needed by these selected tests.

`run-join.sh` records the first committed compile/policy-only validation at
d8dc1a6dd; `run-final.sh` records the intermediate 02eaf8b91 failures;
`run-negative.sh` and negative-overlay.json describe the base-call.go control.
Paths in these scripts intentionally identify this worker's owned staging.
Historical test output includes incidental elapsed fields and existing compiler
diagnostics. No timings were compared, benchmark run, or measurement lease taken.

## Recovery, cross-check and remaining work

Recovered /tmp/wago-arm64-repair/baseline.log and four prior ARM64 binaries before
building replacements. No prior ARM64/QEMU/go-test process was running. Their
hashes and the baseline log are retained, but their exact dirty-source identity
is unknown and they are not qualification pins. The empty inherited temporary
.go.5233535196653571793 file is untouched. The first exported build attempts hit
mise trust and then inherited GOROOT mismatch; explicit toolchain paths resolved
both. receipt.json records these bootstrap failures and the harness type errors.

Read-only sibling recheck: Slipstream dcb45b56386327a1f71fb039ed02e13562e1e2d7
and Starshine b28144a7c50f1dc360825f7f68220022101b9921 contain no copy of the
Wago join/shortcut implementation under the searched src/scripts paths. Host
resource and external-runtime routes were inspected; no equivalent defect was
verified. File hashes and scope are in receipt.json. This is not sibling runtime
qualification. The original native-run-41396042416 evidence remains unchanged.

Remaining: native Darwin rerun of repaired selectors and affected public import
examples/linking; wider backend/diagnostic/AssemblyScript and conformance failures
from the native-run README remain unqualified. The other worker's src/wago
platform tests remain outside this ownership. No production edits outside the
ARM64 directory, driver changes, push, PR, dispatch, subagents, or sibling writes.
Unrelated worktree/index/submodule changes were preserved. No new deadline
blocker was found in these bounded slices.

The fixture/comparator/evidence-only changes cannot affect shipped runtime
performance. The join repair changes native-leaf eligibility for candidate frame
plans and needs independent measurement after correctness review.

READY TO MEASURE: branch `experiment/rfwgc-port`, source
`b9433aa2050c8069baa3edbaa50bd4e8e9c95b26`; sensitive paths
`arm64/call.go:nativeLeafContextShortcut`, `emitDynamicHostOrCrossInstance`, and
`emitCrossInstanceCall`. Proposed workloads: numeric dynamic Go imports and
native aliases with unwind-only plans, multi-import context callers, and
no-plan numeric native-leaf controls. Parent owns native Darwin rerun and the
global measurement lease; this implementation worker performed no measurements.
