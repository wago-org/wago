# RR-1 / RR-2 invocation-repair re-review (Claude)

Verdict: **NOT APPROVED.** RR-1 (as reported) and RR-2 are CLOSED at the target.
The repair introduces a new verified blocker, **RR-3**. This report is advisory
input to the parent. It makes no overall port approval, no inclusion or PR-readiness claim, and no measurement claim.

## Pins

| Role | Commit |
| --- | --- |
| Verdict target (source) | `9ad40bce4a7397dbadf0b0528f2587ed59d7d441` (immutable `git archive`, docs excluded) |
| Handoff evidence commit | `7c4ab69aa3b8c1d3e38ee000ac8bf4642c2941f8`: zero non-doc diff from the target, so not executed separately |
| Previous runtime | `d2464100f1945d38f9d1e25196855fe9699d9875` |
| Baseline | `9713878ab452848f7eb7549a90be94b938a898be` |
| Review input | `0efde30d7`, `../final-review-design-recheck/README.md` (directory unchanged since then) |

- Toolchain: go1.27.1 linux/amd64, explicit `GOROOT=/home/dev/.local/share/mise/installs/go/1.27.1`, `GOTOOLCHAIN=local`, `GOPROXY=off`, `GOENV=off`, minimal `PATH`. Runner: `attempt-*/bin/run.sh`. It refuses to overwrite a log.
- The in-tree `src/wago/design_recheck_adversary_test.go` is byte-identical to the review's `_probes` copy: sha256 `bff4fda5…b98b3c` at all three locations.
- Production changes in the repair: `reference_store.go`, `host_execution.go`, `gc_conversion_store.go`, `gc_conversion_store_host.go`, `gc_conversion_store_other.go`. No backend or GC-native file changed in `9ad40bce4`.

## Per-finding verdicts

| ID | Verdict at 9ad40 | Evidence |
| --- | --- | --- |
| RR-1: a topology writer waits for a storage-only guest call | **CLOSED** | `TestDesignRecheckStorageOnlyCallTopologyWriter` none, global and table PASS in all 3 modes and under race. Source: the conversion-only branch releases the reader before domain or native waits. |
| RR-2: a nested store checks the stale callee identity | **CLOSED** | NestedSameRuntimeClear (both relays), NestedPublish and CallbackChangesPublisherAuthority PASS in 3 modes and under race. The root and its id come from `hostLoopActivation` (`host_execution.go:348`). Every Linux route for helpers 53–58 reaches `dispatchActive` (hostcall, prepared_gc and the bounded helpers all go through `activation.dispatch`). A foreign root or a stale id fails closed. |
| DR-2: ordinary cross-Runtime scalar imports | Retained | Durable `TestDesignReview*` / `TestDesignRepair*` PASS. ForeignRelayAfterLatePublisher PASS. |
| DR-3: unsupported-platform publication | Not executed | `_other.go` changed only by the rename. No Darwin or Windows runner is available, so this is a coverage gap. |
| **RR-3 (new): releasing a conversion-only invocation waits for the topology writer** | **OPEN: blocker** | See below. |

### RR-3

**Cause.**
- `gcInvocationSnapshot.unpin` (`reference_store.go:1520-1561`) always takes `lockGCDomainTopology()`. That is the uncancelable `topology.Lock()` writer.
- It does so even for an empty snapshot.
- `unpin` runs on every conversion-only return (`lease.unlock`), on every canceled admission (`reference_store.go:1717-1719`), and on every callback resume.
- True-dynamic invocations (for example, a funcref-table module) still hold the topology reader across native execution, which is intended.
- So a storage-only call cannot finish returning while any true-dynamic call runs in its Runtime.
- `gcTopologyGate` prefers writers (`gc_admission.go:40`), so the pending release also blocks every later reader admission. That includes every other storage-only call.

**Executed** (archived probes `_probes/rr_recheck_adversary_test.go` and `_probes/rr_recheck_handoff_test.go`):

| Probe | 9ad40 (3 modes + race) | d246 (3 modes + race) | baseline (3 modes) |
| --- | --- | --- | --- |
| A storage-only `table` call returns while a dynamic call spins | **blocked >2 s** | returns | blocked: pre-existing and different (see note) |
| A second storage-only `InvokeContext`, 1 s deadline | **deadline exceeded** | OK | OK |
| A canceled admission (300 ms deadline, waiting on a domain gate) returns | **blocked >2 s past the deadline** | prompt `context deadline exceeded` | prompt |
| Shared-memory handoff: storage-only signaller → dynamic waiter | **wait cycle; never completes** (broken from Go after 3 s) | completes | completes |
| `none` (scalar) control | returns | returns | returns |

- Note: the stacks in `diag-*.log` show the cause of each block.
  - At 9ad40 the call is blocked in `unpin → lockGCDomainTopology → topology.Lock`.
  - At the baseline the first call blocks earlier, on the global `nativeExecutionMu` in `callPreparedPrivate` (`instance_native_context.go:712`). It never runs natively. That is native serialization, not topology.
  - The second-admission, canceled-call and handoff regressions exist only at 9ad40.
- The handoff cycle is a liveness deadlock for valid threads programs: the waiter holds the reader, the release is pending as a writer, and the signaller is blocked as a reader.
- **Repair direction** (not a patch):
  - Do not take the topology writer on release. For example, decrement the claims under the store mutex. Take the writer only when a domain actually reaches `refs==0 && claims==0`, and defer that unlink without blocking the caller. Alternatively, make the unlink cancel-safe or asynchronous.
  - Never block a canceled admission on the writer.
  - Re-run these probes.

## Lifecycle review (source-read; executed where noted)

- **Snapshot immutability.** The conversion-only view is a freshly allocated, sorted `gcInvocationDomainSet` (`reference_store.go:1621-1635`), not the linked-list iterator. Later topology writers cannot change it.
- **Pins.** Membership in a registered collector and claim overflow are validated all-or-nothing under the topology reader plus `s.mu`, before any increment. The decrement and the last-pin unlink happen under the writer. `Close` of collectors and entry release happen after the unlock. Retirement is blocked while `invocations` is non-empty (`canReleaseEntriesLocked`).
- **Partial acquisition.** `lockContext` unwinds partially acquired gates, then calls `unpin`. That is correct, except for the RR-3 wait.
- **Callbacks.** Suspend releases the exact gates and keeps the pins. Resume runs before native execution is reacquired (`host_execution.go:470-484`, `606-608`), so there is no lock-order cycle with `nativeExecutionMu`. Resume refreshes to a fresh admission, which is not wider than a new call.
- **Store validation.** Single and bulk stores validate the old and new receipt domains against the acquired view. `slots` is a map, so an out-of-range index cannot panic before `prepareLimit`'s bounds check.
- **True-dynamic and empty views.** These still take the original branch (`reference_store.go:1727-1731`, unchanged).
- **Minor (not a blocker).**
  - `snapshot.active` is written without `s.mu` but read under it. Only same-owner goroutines reach it, and race reports 0.
  - A nested entry into a *different* conversion-only instance whose view is a strict superset of the root's acquires the gates the root already holds. A late publisher can cause this. This is source-derived only and not executed. The shape is the same as the pre-existing partial-ownership path.

## Execution matrix (`attempt-20261102T111026Z-claude/logs`, exits in `exits.tsv`)

The selector covers the review adversaries, the durable DR tests, `TestDesignInvocationSnapshot*` and the RR-3 probes.

| Cell | ordinary | wagodebug | wago_regalloccheck | race (ordinary) |
| --- | --- | --- | --- | --- |
| 9ad40 matrix | exit 1: 18 top-level PASS; only the 3 RR-3 probes FAIL; 0 skip | same | same | same, 0 DATA RACE |
| d246 RR-3 controls | exit 0: 5/0 | 5/0 | 5/0 | 5/0, 0 DATA RACE |
| baseline RR-3 controls | exit 1: only the first-call return (native mutex) | same | same | - |

- Units:
  - Top-level counts are `^--- PASS` lines.
  - The 18 are 5 recheck adversaries, 8 durable DR/review tests and 5 snapshot tests.
  - The RR-3 `none` subtest passes in every cell.
- Raw failed attempts, kept and uncredited:
  - `rr-9ad40-ordinary.log` is a build failure of my own probe v0 (`_probes/rr_recheck_adversary_test.go.v0-buildfail.txt`, a byte/uint32 typo).
  - The `r2`, `diag` and `handoff` logs are the first executions.

## Coverage limits

- Not duplicated: the full suite, QEMU ARM64/RV64, TinyGo, and Darwin/Windows. The parent owns those.
- No QEMU, TinyGo or native-Darwin claim is made. Native Darwin run 41431926958 and the da19 diagnostics are separate.
- The 2 s and 3 s bounds are blocked/not-blocked limits, not timings. No benchmarks were run.
- No production edits, push, PR or subagents.

## Sibling cross-check (read-only)

- Slipstream `dcb45b56386327a1f71fb039ed02e13562e1e2d7` has no topology reader/writer gate or per-invocation snapshot (no `RLock` or topology matches under `src`).
- Starshine `1d6c0af47140299d8f3c01ed03060d279c43ee03` has no native store.
- Neither has an analogue. No sibling defect is asserted.
