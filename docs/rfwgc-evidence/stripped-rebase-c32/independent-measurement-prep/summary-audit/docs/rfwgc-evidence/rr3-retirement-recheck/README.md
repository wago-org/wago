# RR3 retirement repair: independent re-review (Claude)

Verdict: **RR3 is CLOSED on host linux/amd64 at `5847df30f`**, in all three modes and under race. RR1 and RR2 stay CLOSED. DR-2 cross-Runtime safety is retained. My probes found no new verified finding. This report is advisory input to the parent. It makes no overall port approval, no PR-readiness claim, no measurement claim and no target-platform claim.

## Pins

| Role | Commit |
| --- | --- |
| Verdict target (source, tests, evidence) | `5847df30f1af15d10b743d90a24b1429b290d358` |
| Original RR3 report | `3b8732ad3`, `../rr-invocation-recheck/README.md` |
| Prior runtime (RR3 introduced) | `9ad40bce4a7397dbadf0b0528f2587ed59d7d441` |
| Previous runtime | `d2464100f1945d38f9d1e25196855fe9699d9875` |
| Baseline (not re-executed here) | `9713878ab452848f7eb7549a90be94b938a898be` |
| Implementation evidence reviewed | `docs/rr3-retirement-repair/` at the target |
| Slipstream | `d6802f5e389bded786f49b4c2a21df0efab7017c` (read-only) |

- **Sources.** Each source tree is an immutable `git archive <pin> -- . ':!docs'` under `/home/dev/.cache/rr3-retirement-recheck-src/`. Hashes are in `attempt-*/source-archives.txt`. Pristine cells ran on the unmodified `5847df30f` tree. Probes ran only on separate `*-overlay` copies.
- **Toolchain.** go1.27.1 linux/amd64, with `GOROOT=/home/dev/.local/share/mise/installs/go/1.27.1`, `GOTOOLCHAIN=local`, `GOPROXY=off`, `GOENV=off` and a minimal `PATH`.
- **Runner.** `attempt-*/bin/run.sh` refuses to overwrite a log. Every exit code is in `logs/exits.tsv`, and each log header records its overlay hashes.
- **Production scope.** `9ad40bce4..5847df30f` changes only these non-doc files:
  - production: `reference_store.go` and the new `gc_invocation_retirement.go`;
  - tests: the 3 RR-probe copies and `rr_retirement_test.go`.
  - No backend, collector or H6 files changed.
- **Pinned hashes.** The pinned source matches `post-comparison-source.json`. That covers `reference_store.go` `0c223fc7…`, `gc_invocation_retirement.go` `bb88b4d8…`, and the test hashes.

## Per-finding verdicts (host linux/amd64)

| ID | Verdict at 5847df30f | Evidence |
| --- | --- | --- |
| **RR3:** releasing a conversion-only invocation waited for the topology writer | **CLOSED** | `TestRRRecheck{StorageOnlyReturn,CanceledAdmission}BehindDynamicReader` and `TestRRRecheckSharedMemoryHandoff` PASS in all 6 cells. The same unchanged probes FAIL at `9ad40bce4` under this runner (`control-9ad-rrprobes-ordinary.log`, rc 1; only the `none` subtest passes). |
| RR1: a topology writer waited for a storage-only call | CLOSED (retained) | `TestDesignRecheckStorageOnlyCallTopologyWriter` PASS in 6 cells. |
| RR2: a nested store checked a stale callee identity | CLOSED (retained) | `NestedSameRuntimeClear` (3 relays), `NestedPublish` and `CallbackChangesPublisherAuthority` PASS in 6 cells. |
| DR-2: ordinary cross-Runtime imports | Retained | 8 durable `TestDesignReview*` / `TestDesignRepair*` tests and `ForeignRelayAfterLatePublisher` PASS in 6 cells. |
| New findings | **None verified** | See the probes and residuals below. |

## Probe provenance (no assertion weakening)

- **Original probes.** The in-tree `rr_recheck_adversary_test.go` (`0a7136ca…`), `rr_recheck_handoff_test.go` (`77ef2c94…`) and `rr_recheck_flags_current_test.go` (`146214b6…`) are byte-identical to the `3b8732ad3` `_probes`. For the adapter, that means its `.txt` original. Deadlines and assertions are therefore unchanged.
- **Durable adversary.** `design_recheck_adversary_test.go` is unchanged.
- **Formatting exception.** The adapter is the only gofmt-dirty owned Go file (go1.27.1 `gofmt -l`). That matches `commit-format-exception.json`.
- **Implementer toolchain discrepancy.** The implementer's post-comparison and nested-control commands record **go 1.27.3**, not 1.27.1. My independent go1.27.1 cells reproduce the 100/0 focused result, so this is a provenance note, not a defect.
- **Development-1/2/3.** These are correctly retained as uncredited `PRE_COMPARISON_DEVELOPMENT`.
- **Implementer's own tests.** Source review of `rr_retirement_test.go` found real assertions:
  - no close under a reader;
  - no writer reservation;
  - coalescing;
  - unlocked callbacks;
  - closes exactly once.

## Execution matrix (`attempt-20261102T1200Z-claude/logs`)

The selector is the implementer's own regex: `^Test(RR|Design|R9(H2|Ownership)|GCExternConversion|HostCallNeedsGCSuspension|GCAdmission|GC.*Invocation|GC.*Start)`.

| Cell | ordinary | ordinary race | wagodebug | wagodebug race | wago_regalloccheck | regalloccheck race |
| --- | --- | --- | --- | --- | --- | --- |
| Pristine `5847df30f` focused | rc 0, 100 top-level PASS / 0 FAIL / 0 SKIP | same, 0 DATA RACE | same | same, 0 DATA RACE | same | same, 0 DATA RACE |
| `5847df30f` + both independent probes | rc 0, 6/0/0 | 6/0/0, 0 race | 6/0/0 | 6/0/0, 0 race | 6/0/0 | 6/0/0, 0 race |

- Units: top-level `^--- PASS` lines. A cell is one build-mode × race combination.
- Comparison runs (ordinary mode only): the nested real-module probe at `9ad40bce4` and `d2464100f`. These are v1 and v2, below.

## Independent probes (`_probes/`, archive overlay only)

### `rr3x_retirement_internal_test.go` (`ffc14d7a…`, target only)

This probe uses the new retirement state, so it does not build at earlier pins.

- **SyncCloseCallbackUnlockedAndReentrant.** An uncontended last-pin release retires synchronously on the caller.
  - The Close callback observes no store mutex, topology gate, domain gate or `nativeExecutionMu`.
  - A request made from inside the callback is coalesced (it never starts a second driver) and drained.
  - Each collector closes exactly once.
- **CoalescedBoundedWorkerBehindReaderAndQueuedWriter.** 64 last pins are released behind a live reader, and 16×8 concurrent duplicate requests are added.
  - Release returns promptly.
  - At most 2 extra goroutines exist while the retirement is pending.
  - There is no writer ticket and no held writer, and `TryRLock` still admits a reader.
  - Nothing closes under the reader.
  - A real queued writer is neither overtaken nor starved.
  - All 64 collectors close exactly once, and the topology ends empty.
- **NoLostWakeupStress.** 300 iterations race the final `RUnlock` against release, subscription and try-lock. Every iteration reaches idle and closes once.
  - Limit: most iterations probably took the synchronous path, so interleaving coverage is modest.
- **TerminalCloseRuntimeBehindReader.** `closeRuntime` runs with a live pin.
  - Terminal release is deferred while the snapshot is live.
  - The last unpin does not wait behind a reader.
  - The terminal request survives until the reader leaves.
  - The candidate (closed by unlink) and the still-linked domain (closed by terminal release) each close exactly once.
  - A late duplicate terminal or candidate request is a no-op, so there is no double Close.
- **BorrowedRootStaleAndForeign.**
  - A foreign owner never borrows: it waits on the root-held gate and gets a deadline error.
  - The same owner on an active root borrows without acquiring, and a nested unlock keeps root authority.
  - A covered static callee borrows.
  - An unregistered callee fails closed with the root intact.
  - A suspended (callback) root is not borrowed: reentry admits afresh with its own snapshot, and resume restores ownership.
  - After the root releases, the same owner admits afresh.
  - No pins leak, and no registered domain is retired.

### `rr3x_nested_late_publisher_test.go` (real modules)

- **Versions.**
  - v1 (`6c61b21c…`, retained as `.v1.txt`; a byte-exact reconstruction verified against the log headers) covers `none`, `before`, `during` and `during-pub`.
  - v2 (`1bdf921b…`) adds `unrelated-during` and `unrelated-during-pub`.
- **Setup.** A conversion-only root spins and then makes a direct-import call into a conversion-only externref-table provider. Meanwhile a GC conversion publisher is instantiated, and optionally publishes.
- **Hypothesis.** I tested this from source review: the borrowed-root completeness check eagerly rejects valid nested calls after any late publisher.
- **Result: not reproduced.**
  - The nested call returns 7 in every mode at `5847df30f`, `9ad40bce4` and `d2464100f`.
  - A publisher importing the provider's table waits for the root at all three pins.
  - An unrelated publisher binds during the root call at `5847df30f` and `9ad40bce4`, but the nested call still succeeds. At `d2464100f` it waits.
  - So the guard's rejection branch is reached only through internal fixtures (the implementer's and mine), not through this direct-import path. No finding is asserted.

## Source review of the repair

- **No release-side writer.**
  - `unpin` takes only `s.mu`. It removes the registry row, decrements claims, and queues zero-ref/zero-claim candidates.
  - It calls the driver only after unlocking.
  - The canceled partial admission (`lockContext` unwinds gates, then calls `unpin`) and the callback refresh (`old.unpin` before fresh admission) are both nonblocking.
- **Writer try without a ticket.**
  - `tryInvocationRetirementWriter` checks the gate and subscribes to `changed` under one `topology.mu` hold, and never increments `writers`.
  - Every way the blocking condition clears calls `notifyLocked`: a writer `Unlock`, the reader count reaching 0, or a canceled queued writer. So there is no lost wakeup.
  - The worker defers to queued writers.
- **Single worker.**
  - `running` and `idle` change only under `s.mu`.
  - A goroutine is spawned only on the false→true transition of `running`, or as a panic handoff when work is pending.
  - Callback-enqueued work sets `more`, and the same driver continues.
- **Revalidation.**
  - Under the writer plus `s.mu`, a candidate is unlinked only if `domainForCollectorLocked(d.collector)==d && refs==0 && claims==0 && unlinkDomainLocked(d)`.
  - Revival, or a prior unlink or terminal clear, makes the candidate fail this check.
  - Only a successful unlink appends the domain for Close, so there is no double Close.
- **Callbacks outside locks.** Collector Close and lifetime-entry release run after both `s.mu.Unlock` and `topology.Unlock`.
- **Terminal path.**
  - `canReleaseEntriesLocked` now also requires no live snapshots and quiesced closed instances.
  - `releaseGCRef` uses the same predicate.
  - The last unpin after `runtimeClosed` queues terminal cleanup.
  - `closeRuntime` and `storeObjectClosed` still take the queued writer. That is pre-existing and not on the return or cancel path.
- **`active` flag.** Writes to `active` now use `s.mu`. The resume copy `*old = *next` moved under `s.mu`.

## Slipstream comparison (consumed; read-only)

- **Hashes.** All 8 hashes in `slipstream-source.json` match `git show d6802f5e…` in `/home/dev/Projects/slipstream`.
- **Spot-read at the pin.**
  - `gc/native.goose:899-905`, `ng_drop_region`: unmap plus inactive marking, with no lock or wait.
  - `1408-1412`, owner release: a loop over drops.
  - `760-779`, `ng_close`: publishes stop, wakes the helpers, then joins them with `thread_wait` without the mutator lock.
  - `629-689`, `ng_dispatch`/`ng_join`/`ng_worker`: helpers never call `ss_ng_enter`.
  - `1171-1186`, `ng_collect`: joins under `ss_ng_enter`, which is safe only because helpers don't take that lock.
  - The other mapped sites were hash-verified, not re-read.
- **What Wago mirrors.**
  - Nonblocking release.
  - Close outside all progress locks.
  - A worker wait that holds no store, topology, domain, native or graph lock.
- **What Wago adapts.**
  - The retirement claim needs a writer, because Wago admits concurrent topology readers.
  - The writer is tried without a priority ticket, so shared-memory signallers keep reader admission.
- **What Wago does not copy.** The join-under-lock pattern is not blindly copied into the topology protocol.
- **Sibling cross-check.** Slipstream has no per-invocation topology or coalesced-retirement counterpart. Starshine `1d6c0af4…` has no native retirement store. No sibling defect is asserted.

## Residuals (not findings)

- **Deferred reclamation.** Retirement can be deferred indefinitely under continuously overlapping dynamic readers (readers never reach 0) or a stream of queued writers. That retains memory but does not deadlock, and it is the intended trade-off of a non-prioritizing writer.
- **Panics on the background worker.** On the contended path, Close and entry release run on the background worker. An internal panic there would crash the process instead of propagating to a caller. The observers are internal (`function_environment_linux.go:326`). This is source-only and not executed.
- **`idle` channel.** `gcInvocationRetirement.idle` is consumed only by tests. No production path waits for retirement to quiesce. Terminal cleanup is carried by the worker itself.

## Coverage limits

- **Host only.** The verdict covers host linux/amd64 only.
- **ARM64 QEMU.** The parent reports guest execution FAIL in all 3 modes (the build passes). The original `StorageOnlyReturn` second call fails with `create native deadline timer: invalid argument` (64-signal `timer_create` EINVAL under QEMU). The parent's audit of the cause is pending.
  - I credit no QEMU result.
  - I make no claim about native ARM64 kernels.
  - I do not infer a production regression from that failure.
- **RV64.** The RR probes are build-tagged amd64/arm64, so RV64 provides controls only.
- **Not executed here.** TinyGo, Darwin and Windows were not executed. Native Darwin at the old pin is separate.
- **Not duplicated.** The full suite, backend, QEMU, TinyGo and non-host runs were not duplicated. The implementation worker owns them at the exact pin.
- **No performance work.** Bounds of 2–5 s are blocked/not-blocked limits, not timings. No benchmarks were run and no measurement lease was taken.
- **No other actions.** No production or other-doc edits, push, PR or subagents.
- **Retained logs.**
  - The control-9ad run overlaid the 3 original RR probes (hashes above). Its header lists only the `rr3x_*` overlay files.
  - All logs are retained, including the v1 runs.
