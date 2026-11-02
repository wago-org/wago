# Independent Daybreak ARM64 repair re-review

Review source: `b9433aa2050c8069baa3edbaa50bd4e8e9c95b26`  
Evidence head read: `da19f1a8d29fec7305d937a42622a198b8e79717`  
Recovered base: `4f1509996d5a3601f1bd6061b638153153c7f465`

Verdict: no new production finding in the assigned repair slices. The dynamic
import repair, helper-fixture corrections, and diagnostics comparator changes
are independently supported by source review and focused Linux/ARM64 QEMU
execution in ordinary, wagodebug, and wago_regalloccheck. This is review
evidence, not final port approval, performance evidence, or native Darwin
qualification. Native Darwin execution remains with the separate parent
harness.

## Finding disposition

| Finding | Status | Independent conclusion |
| --- | --- | --- |
| Dynamic Go-host/cross-instance local-state join | CLOSED for the bounded source defect | `nativeLeafContextShortcut` now treats every candidate GC-frame plan as requiring a return map, including exact zero-root unwind plans. Both alternatives therefore home the live parameter local and join in the same state. Nil and noncandidate plans retain the ordinary shortcut. |
| Regression strength / expectation weakening | CLOSED | The regression compiles one body with two dynamic imports, retains parameter local 0 across the call, observes two distinct zero-root return maps, and executes both tagged Go-host and different-instance fallback paths for four inputs. Restoring only base `call.go` makes the unchanged regression fail at local 0. No checker or expected output was weakened. |
| Synchronous helper ABI fixtures | CLOSED | Bare fixtures now provide the production 64-slot capacity. Externref `table.fill` checks helper 55 with four logical/physical parameter slots `(i32, externref word, i32, i32)`, no result, exact 32/64/32/32-bit loads, control-frame offsets `hcArgs+0/+8/+16/+24`, dispatch and arity. Two lower v128 values account for physical slots 1-4, so operands occupy slots 5-7 and the table constant slot 8. Production `callHostSync` still enforces parameter/result capacity. |
| Helper-call hints | CLOSED | Externref `table.set` is correctly call-capable because the write barrier can enter the collector; funcref remains helper-free. Existing loop/non-direct hint paths remain exercised by the focused test. |
| Worker scalar diagnostics mismatch | CLOSED for selected synthetic comparisons | Sequential compilation retains scalar scratch while workers call `FinishWorker` and discard it; scalar admission duration is timing data. The comparator excludes those resource/timing fields without mutating inputs. It still compares emitted code byte-for-byte, entries, internal entries, prepared-table state, code/frame size, compiler path, source offset, and all other stats. `fib_rec` and `memory_tree` retain recursion/direct-call relocation-sensitive output coverage. |
| Cancellation `TestSharedEntryGuardCancellationAndGC` | OPEN, unchanged | The documented QEMU failure remains in all three modes. Static route review still finds no connection to this repair: the memory-touching loop is ineligible for the dynamic Go-host fast alternative and already used the generic cross-instance path with candidate-plan recording. No deadline, expectation, or classification was changed. Native Linux/Darwin discrimination remains pending; this review does not label the failure a QEMU defect. |
| New findings | NONE | No additional verified defect was found in the assigned scope. |

## Executed scope

`run.sh` exported the exact review source twice, substituted base `call.go` only
in the negative tree, and used Go 1.27.1 with `GOOS=linux`, `GOARCH=arm64`,
`CGO_ENABLED=0`, and QEMU 10.0.13. The final matrix is:

| Lane | ordinary | wagodebug | wago_regalloccheck |
| --- | --- | --- | --- |
| Focused repair tests | PASS (24 test/subtest events) | PASS (24) | PASS (24) |
| Diagnostics synthetic comparisons | PASS (8 test/subtest events) | PASS (8) | PASS (8) |

The focused selector covers the dynamic join execution/policy tests, table
lowering fixtures, physical bulk slots, table-set hints, and comparator
regression. The diagnostics selector covers `tiny`, `fib_rec`, `dispatch`,
`many_funcs`, `memory_tree`, plus compact/shared-adapter `many_funcs`. It does
not duplicate AssemblyScript or broad combined gates. The negative control
builds successfully and exits 1 with:

`arm64: function 0: direct Go import local-state join differs at local 0`

An initial local invocation ran the QEMU binaries from the review worktree, so
the diagnostics tests could not resolve the immutable archive's relative corpus
path. The runner was corrected to execute from the archived package directory
and the complete final matrix was rerun; only the final logs are retained.
No benchmark or timing comparison was run.

## Evidence and limits

`environment.log` records source/base/evidence hashes, Go and QEMU versions, and
the QEMU hash. `test-binary-sha256.txt`, `results.tsv`, and `logs/` retain the
final binaries' hashes, exit codes, and full focused output. The original
`native-run-41396042416` evidence and `darwin-arm64-repairs` evidence were read
and verified but not modified. Archived `_probes` are not qualification inputs.

Read-only sibling cross-check repeated at Slipstream
`dcb45b56386327a1f71fb039ed02e13562e1e2d7` and Starshine
`b28144a7c50f1dc360825f7f68220022101b9921`. Searches of their source/runtime
routes found no copy of the Wago ARM64 shortcut/join or worker comparator, and
the previously inspected host/runtime files retain the recorded hashes. No
equivalent sibling defect was verified; this is not sibling runtime
qualification.

Exact limit: these results establish only the selected Linux ARM64 QEMU source
slices. They do not establish Darwin-native execution, cancellation delivery,
wider backend/conformance/AssemblyScript behavior, full CI, performance, or
port readiness. The November 4 bar is unchanged.

