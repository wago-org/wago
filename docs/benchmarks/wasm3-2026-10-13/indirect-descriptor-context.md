# Indirect descriptor context and ABI fallback

Retained ARM64 changes `bd3250c8` and `0c321bbe` defer instance-context loads to
the wrapper arm of indirect-call dispatch. A uniform private table populated by
function indices, with no function imports or initializer, can also defer the
canonical descriptor lookup. The runtime ABI tag fork remains: private tables
can contain wrapper entries, including eight-result functions. Bounds, null,
type, and wrapper ownership checks remain on their applicable paths. Indirect
integer call targets are copied outside the argument bank before argument
staging, matching the existing mixed/reference call behavior.

The first unconditional private-table prototype crashed on eight-result entries
because the runtime publishes wrappers for those signatures. That prototype
was removed before committing the guarded implementation. Public tests cover
private/exported tables, both register-ABI modes, fresh/reloaded artifacts,
eight arguments, one/two/eight results, a live prefix, both runtime-selected
callees, null entries, out-of-bounds indices, and high bits of i32 indices.

Measured five real wasm.fyi workloads on Apple M4 Max using source-pinned SDKs,
the shared measurement lock, four phases, three launches × five samples per
successful phase, RSS and code measurements. An initial 15-capture run measures
`bd3250c8`; two further 15-capture runs compare `0c321bbe` with retained
`f8cbc31b`, reversing Wago collection order. All 45 raw attempts and 180 phase
rows are audited in [indirect-descriptor-context.json](indirect-descriptor-context.json).

| Workload | First final-candidate change | Reversed change | Peer outcome |
| --- | ---: | ---: | --- |
| core-tab/indirect-call | −0.70% | +0.96% | Still approximately 1.75× / 1.89× peer time |
| threads/table-indirect-call | −4.12% | −9.41% | Unsupported peer host profile |
| wago/dispatch/apply | 0.00% | 0.00% | Successful pair |
| compact-imports/ordinary-baseline | −0.77% | −0.89% | Successful pair |
| tail-call/indirect-recursion | −0.48% | +1.02% | Existing peer translation failure for opcode 0x13 |

The ordinary indirect-call gap is unchanged within variability. Retain this
lowering for its repeated shared-memory indirect-call improvement, without a
claim of parity there or elsewhere. No resource failures occurred. The existing
peer translation failure is retained as a nonnumeric result, not discarded.

Compiler cost: three alternating 100-pass pairs over 218 Core 3 modules and 27
thread modules, GOMAXPROCS=1. Median elapsed time changed +0.84% / +2.99%, code
bytes +0.045% / +0.099%, allocation bytes +0.093% / 0%, and process peak RSS
+0.81% / +0.50%. These measurements are for `0c321bbe`, before the subsequent
ABI correctness guard change.

The portable execution matrix also exposed an existing AMD64 shortcut defect:
private-table dispatch demanded an internal ABI even with register ABI disabled
or eight results, although those entries correctly use wrappers. The same
matrix fails on retained `f8cbc31b` on native x86_64 Remote, reproducing the
unsupported-context trap. An added monomorphic optimization-off case reproduces
a native SIGSEGV on retained `f8cbc31b` on M4. Fix `34b940db` requires enabled
register ABI for monomorphic direct calls on both architectures and limits the
AMD64 immutable multi-target shortcut to the ordinary published integer ABI;
other signatures use the existing home-aware dispatch.

After the fix, 410 tagged selection/execution/call-boundary checks passed with
no failures or skips on M4, and the native AMD64 matrix passed on Remote.
Core 3 and official atomic conformance passed for the descriptor refinement.
No AMD64 performance claim is made. Candidate captures remain private until a
complete newer source snapshot is available for a local refresh. Nothing was
published to production.
