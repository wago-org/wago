# Wago host-call specialization — consolidated report

Owner thread: [host-call specialization](t3-thread://v1/mcp:8faf6900-ac97-419e-b761-141f062b89c2).
Base: `90f2b57dafa7f8c3cc7e3aca38b91148f15e1d1e`.
Design authority: Slipstream `bd640ddf0:docs/host-call-boundary-redesign-2026-11-01.md`.
Campaign branch: `campaign/wago-hostcall-record`.
Disposition: census, classification/admission, implementation, review and qualification complete. One candidate is pushed and READY TO MEASURE for Linux amd64. No PRs or performance measurements were made.

## Census

Three standard-speed sol workers audited registration, runtime/compiler dispatch, and host surfaces in parallel against the exact base. Their reports are `census-registration.md`, `census-runtime.md`, and `census-surfaces.md`. A separate sol worker performed design admission, one implemented the admitted class, and one reviewed the completed candidate. All six workers completed. Findings were cross-checked read-only against pinned Slipstream and Starshine source before admission.

Wago already resolves callback shapes/signatures once into indexed bindings. There is no general per-call `reflect.Call` or scalar boxing to remove. Reflection for native-leaf code/type identity happens during registration. Generic synchronous guest calls marshal through a control frame, park native execution, dispatch on a normal Go stack with the required authority/GC/lease handling, and resume. The inline-capacity generic engine route copies P u64 inputs, clears R result slots and copies R outputs back. Scalar portals and borrowed views already remove those extra copies where their transport proof applies. Addressable thunks can add wrapper/control-slot transport. Owner/root identity, traps, cancellation, reference translation and reentry are observable obligations.

The production fixed catalog in this repository is three conditional `wago:abi` functions: `result.get`, `result.drop`, `value.drop`. Preview 1 is an external companion provider, not bundled core source: the benchmark module pins `github.com/wago-org/wasi v0.3.1-0.20260916034125-6a6684d2ecd2`, supplying 46 functions. Thus the finite production-plus-pinned-Preview-1 inventory is **49 complete bindings, all BOUNDARY under their current complete contracts**. The 56 compatibility/conformance callbacks enumerated in the surface report are test fixtures, not additional production built-ins. There is also an open-ended embedder/plugin callback surface.

Every pinned WASI handler is wrapped in caller-state resolution, locking and possible lazy initialization. Even constant/unsupported operation bodies therefore do not establish a DIRECT complete binding. String/output and I/O validation order differs from Slipstream, including incremental writes and external effects before some output checks. Those semantics cannot be silently replaced by a different sibling implementation.

## Classification and admission

| Class or surface | Final verdict | Reason |
| --- | --- | --- |
| Existing certified unary/binary i32 register arithmetic | DIRECT, existing | Instruction proof excludes memory, calls, closure state, allocation, blocking and reentry. |
| Multiple distinct certified i32 leaves per instance | DIRECT, ADMITTED AND IMPLEMENTED | Per-binding proof/ownership removes the former same-PC restriction; transactional all-or-nothing publication preserves fallback and lifetime. |
| Arbitrary numeric Go callbacks | BOUNDARY; DIRECT NOT VIABLE from signature alone | Callbacks may grow stacks, collect, block, panic, capture state or reenter. |
| Current WASI memory/metadata copies | SPECIALIZED NOT VIABLE in this Wago-only scope | External provider state/gate wrapper and lack of authenticated semantic-lowering identity prevent safe boundary bypass. |
| Constant/denial WASI bodies | DIRECT NOT VIABLE as complete current bindings | The surrounding lock/state/initialization contract remains. |
| Caller, reference, plugin-gated, resource and GC callbacks | BOUNDARY | Preserve authority, revocation, token/root translation, leases, owner lifetime and normal Go-stack execution. |
| Three `wago:abi` operations | BOUNDARY | Go-map handles, lifetime mutation, possible cloning/allocation and formatted trap paths. |
| Deferred events substituted for synchronous calls | NOT VIABLE | Changes completion, effect ordering and failure semantics. |
| Native leaves counted as Go safepoints | NOT VIABLE | Removed callbacks invalidate bounded-Go-segment scheduling; retain native scheduling/cancellation. |
| Additional ordinary signatures such as `func() int32`, or public effect annotations | DESIGN FINDING ONLY | Would expand the public accepted host API; not shipped. |
| New distinct-leaf class on RV64 | Generic fallback retained | Its separate wrapper registry embeds control identity; the shared amd64/arm64 ownership design is not copied into it. |

No SPECIALIZED memory class passed admission. No raw-assembly class was admitted. Other width/FP/void shape expansions remain separate investigations, not claimed failed proofs or implemented candidates.

## Candidate

- Branch: `candidate/wago-hostcall-direct-distinct-i32`.
- Pushed head: **`77c6130c87c04c2a6d856402bc47158470e5bc2f`**.
- Qualified runtime source head: `0ab4994f229b995bdce5cc89e557cb8c40c0cea4`; runtime source is unchanged through the pushed head.
- Base is exactly the requested main revision; no GC-port or campaign-record changes were imported.
- State: **READY TO MEASURE — Linux amd64**, with the concurrent ordinary-root baseline failure explicitly retained.
- Candidate worktree is clean; both candidate and campaign branches are pushed. No PR exists.

The new route independently proves every selected unary/binary i32 callback, retains code by exact PC/type/arity identity, and binds each instance-owned import cell to its own direct wrapper. It leaves the frame-wide trampoline generic for distinct leaves so addressable paths cannot accidentally execute the wrong index-zero callback. Every binding must qualify; any unknown, effectful, gated or GC-domain binding keeps the complete instance generic.

The installation snapshots cells/control/flags and restores them before releasing code on publication or native-owner failure. Reference-counted code sharing preserves per-import/owner identity through close. Legacy single/same-PC behavior remains, including arm64 staged-only handling. The decoder, generated wrapper ABI, arm64 +192 register entry and public host API are unchanged. Native flags retain the scheduling restriction; isolated single-import admission is not broadened. RV64/TinyGo use the generic route for the new class.

The code-evident per-call saving for newly admitted direct calls is the ordinary Go callback/park-resume/dispatch route and applicable generic transport. Existing wrapper homes, spills and context work remain where the existing emitter requires them. No latency or speedup is claimed.

## Qualification

| Gate | Result |
| --- | --- |
| Actual direct-route differential execution vs forced generic | PASS; poisoned staging marker proves the direct route ran |
| Raw poisoned argument/result homes, overlap and adjacent sentinels | PASS |
| Import permutations, unary/binary bodies, aliasing, locals/globals, table/ref/tail/reexport/cross-instance and traps/recovery | PASS |
| GC/gate/profile/closure/effect rejection; cancellation/scheduling | PASS |
| Acquisition/publication/owner failure rollback, cache sharing and close | PASS |
| Full changed runtime/public packages in ordinary, `wagodebug`, `wago_regalloccheck` | PASS |
| Full checked root, both CLI runtime recipe modes, checked installer and checked bench module recipe | PASS |
| Exact three-package race suite | PASS twice |
| Complete ordinary root, no exclusions, `go test -p=1 -count=1 ./...` | PASS using the same clean baseline harness |
| Earlier concurrent ordinary-root command | RED retained: exact-base TinyGo cancellation abort reproduced independently |
| Six foreign OS/architecture cells, two packages each | 12/12 compile-only PASS; foreign execution unclaimed |
| Evidence-runner failure contract | PASS, 50 success/failure/signal scenarios |
| Documentation and diff checks | PASS |

The unmodified concurrent unit recipe is not described as all-green. Its TinyGo cancellation abort was reproduced from an exact-base, no-import generated executable, on stress attempt 619; it does not admit native host leaves. Plugin publication `text file busy` was also reproduced at the exact base. Earlier missing ICU, WABT version and interpreter prerequisites were repaired in the test harness. Initial red logs remain alongside successful runs. Both the exact base and candidate passed the complete ordinary root suite with package serialization and pinned CI-equivalent tooling, without test exclusions.

Execution qualification is Linux amd64 / Go 1.27.1. arm64 and other foreign execution remain unclaimed. RV64 new-class fallback is intentional. TinyGo's new class remains disabled. This is a candidate qualification, not a claim that every platform CI lane was executed.

An independent sol review found no runtime correctness issue. Its P2 finding that both replay scripts returned zero after recording failed commands was fixed in `947d4b4a0`. The real scripts now retain all results and exit nonzero for any failure; injected normal/signal failures at every command position are checked. Original JSON manifests already retained failures, so no red result was erased or relabeled.

Detailed manifests and copied candidate evidence are in `candidate-direct-distinct-i32/`. Full immutable candidate evidence is in [the candidate head](https://github.com/wago-org/wago/tree/77c6130c87c04c2a6d856402bc47158470e5bc2f/evidence/hostcall-direct-distinct-i32). Parent baseline logs are in `baseline-logs/`.

## READY TO MEASURE handoff

Branch: `candidate/wago-hostcall-direct-distinct-i32`.
Commit: `77c6130c87c04c2a6d856402bc47158470e5bc2f`.

Performance-sensitive paths: per-import installation in `src/wago/host_execution.go`, acquisition/lifetime in `src/core/runtime/native_leaf_native.go`, and the existing amd64/arm64 native wrappers and tagged compiler call paths selected by the new dispatch cells.

Proposed independent workloads: guest loops alternating distinct unary callbacks; mixed unary/binary arithmetic with independently checked wrapping checksums; alias/distinct permutations; context-bearing locals/globals/shared-memory modules; separate addressable/table/ref fallback cases; and repeated instantiate/close scenarios for code-cache/metadata footprint. Use matched exact-base/candidate builds and record actual route counts separately from measurements. arm64 needs native execution qualification first. Measurement requires the global lease and an independent measuring thread; this implementation thread has taken no timings.

## Discipline

All worker and parent changes are committed. Candidate-owned build temporaries, cross binaries and interpreter build products were cleaned. Shared caches and unrelated worktrees were not cleaned or changed. The GC-port branch/worktree/qualification and CI were not accessed or dispatched. No public host API expansion, unrelated source cleanup, upstream-main push, PR, Claude review, benchmark or performance measurement occurred.
