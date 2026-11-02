# RR3 nonblocking invocation retirement

Implementation evidence only; parent owns fresh review and final acceptance.
Review: `3b8732ad31098db5ea33a2f71a492d3fbcdcecf9`,
`docs/rfwgc-evidence/rr-invocation-recheck/README.md`. RR1/RR2 were closed at
`9ad40bce4a7397dbadf0b0528f2587ed59d7d441`; RR3 was introduced by its unconditional
unpin topology writer. No earlier failed source or raw log is overwritten.

## Current Slipstream contract

The full eight-site audit of Slipstream master
`d6802f5e389bded786f49b4c2a21df0efab7017c` was consumed and its pinned source read
before post-comparison lifecycle testing. RR3 is ABSENT there. The concrete
release/cancellation/rollback/resize/Close and helper-join mapping, line ranges,
source hashes, and Wago-only deviations are in `SLIPSTREAM-MAPPING.md` and
`slipstream-source.json`. Region release adds no progress-dependent wait;
Close runs outside progress locks. Wago mirrors those invariants. It does not
copy Slipstream's GC join-under-mutator-lock into Wago's topology protocol.

## Implementation

Unpin removes the registry row and decrements every claim under the store mutex.
It never waits for topology or a worker. Empty snapshots and non-last drops
require no retirement writer. Zero-ref/zero-claim candidates stay registered
and their collectors stay live until a writer can claim retirement. Terminal
store cleanup is queued when needed; the public GC-token terminal release path
uses the same live-snapshot predicate as ordinary store Close.

A cold per-topology scheduler coalesces candidates and terminal cleanup requests.
It tries the writer without reserving writer priority. On contention the caller
returns and one worker waits on a gate-change notification without holding any
store, topology, domain, native or graph lock. Subscription and failed try-lock
are atomic under the gate mutex, so a final reader's notification cannot be lost.
A queued retirement is never itself a queued writer, preserving reader admission
needed by shared-memory signallers. An uncontended batch may finish synchronously.

Under the writer and store mutex the worker revalidates exact registration,
zero refs and zero claims. Revival or prior retirement cancels a candidate;
successful unlink alone owns collector Close. Callbacks and token/finalizer
release run outside every retirement/execution lock. The same worker drains
callback-enqueued requests; pending work and idle/exit transitions share the
store mutex. No goroutine is spawned per candidate or duplicate request.

Snapshot active-state transitions now use the store mutex. The separately noted
nested borrowed-root case is guarded before topology admission: a different
callee cannot widen an already-active root snapshot or wait for its own held
domains. Missing authority fails closed with the root lease intact. Callback
reentry remains fresh admission after full suspension. An immutable 9ad control
fails this new regression at its deadline; `nested-control.json`, its probe and
raw log preserve that result. The earlier sibling shape check is superseded by
the current-master mapping above; no sibling defect is asserted.

Production paths: `src/wago/reference_store.go` and the narrow new
`src/wago/gc_invocation_retirement.go`. No backend, native collector, H6 scheduler,
CI or sibling edits. Existing unrelated worktree/index/gitlinks are preserved.

## Tests and provenance

The two original RR3 probes plus their current-flags adapter are byte-identical
durable copies in `src/wago/rr_recheck_*_test.go`; the original RR1/RR2 adversary
is also unchanged. `provenance.json` records hashes. Deadlines and shared-memory
notification progress assertions are unchanged.

Five additional lifecycle families cover nonempty/last-pin releases behind
readers, claim/registration revival, real store Close, coalescing, unlocked
collector callbacks, work queued during callbacks, sixteen rounds of eight
requests racing worker exit, stale candidates, nested borrowed-root late
publishers, partial canceled acquisition dropping the last pins, and callback
refresh releasing old pins behind a live reader. Original H2/H6, old-writer,
foreign/single/bulk publication and invocation/start tests remain mandatory.

Development-1/2/3 were started before the explicit current-master comparison;
all are retained as **pre-comparison development only**. Development-3 passed
all six cells but is not promoted. Post-comparison cells run independently after
consuming the audit and recording the mapping. The immutable 9ad nested-root
control is an expected failure, not a repaired-source pass.

All qualification drivers take an immutable pin, explicit Go GOROOT/tool paths
and fresh output directory, and check source/driver integrity. Target execution
uses the prior hashed test-only portability fixture and architecture probe.
TinyGo runs supported conversion compatibility smoke only. Darwin/Windows
cross-builds are compile-only; the parent owns actual native execution.

No benchmarks, performance timings, measurement lease, subagents, new review,
push or PR were performed. Completion and measurement readiness depend on the
recorded final pin and parent review; no self-approval is given here.

## Immutable qualification at 5847df30f

Source/test pin: `5847df30f1af15d10b743d90a24b1429b290d358`.
`summary.json` records command exit, complete skip lists, top-level census,
failed tests and source integrity. The summarizer exits **1** and sets
`qualified: false`: ARM64 guest execution failed. This is not a closure or
combined-head qualification claim. All drivers finished without edits.

| Gate | ordinary | wagodebug | wago_regalloccheck |
| --- | --- | --- | --- |
| Focused | PASS 100, 0 skips | PASS 100, 0 skips | PASS 100, 0 skips |
| Scoped race | PASS 100, 0 skips | PASS 100, 0 skips | PASS 100, 0 skips |
| Full Wago | PASS 2539, 44 skips | PASS 2533, 50 skips | PASS 2539, 44 skips |
| Backend/compiler | PASS 1006, 552 skips | PASS 1004, 554 skips | PASS 1129, 553 skips |
| ARM64 QEMU | **FAIL**, 74 pass / 1 fail | **FAIL**, 74 pass / 1 fail | **FAIL**, 74 pass / 1 fail |
| RV64 QEMU controls | PASS 48, 0 skips | PASS 48, 0 skips | PASS 48, 0 skips |
| TinyGo compatibility | PASS 5, 0 skips | PASS 5, 0 skips | PASS 5, 0 skips |
| Linux ARM64/RV64 builds | PASS | PASS | PASS |
| Darwin ARM64/Windows AMD64 builds | PASS | PASS | PASS |

Pass counts are top-level families; skip counts include logged subtests. Full
Wago/backend platform or mode exclusions remain raw and enumerated in the
summary. No required focused family is skipped. RV64 excludes the original
amd64/arm64 RR3/lifecycle files by their build constraints; it is relevant
controls only. TinyGo smoke does not qualify host opaque receipts. Darwin and
Windows are build-only here. Native Linux ARM64 execution is **NOT_RUN** and
is the open discriminator. No full-package race or full conformance campaign
is claimed.

All four immutable exports match all **4014 tracked src blobs** at the source
pin, with zero changed blobs, zero post-run export changes, and unchanged
drivers. Original RR1/RR2 and RR3 probes remain byte-identical. The target-only
fixture/architecture adapter is separately hashed and not a production edit.

`TIMER-DIAGNOSIS.md` details the ARM64 failure and bounded controls. This QEMU
rejects native timer creation with signal 64 (EINVAL) but accepts signal 35;
native AMD64 accepts both. No production signal-selection change or adapted
harness was made. Failed target commands and all earlier attempts are retained.

RR1/RR2 adversaries remain passing on the host all3/race and on ARM64 QEMU.
RR3 host return/cancellation/shared-memory and new lifecycle families pass
all3/race after the Slipstream audit; full platform qualification is incomplete.
Final review and any closure decision belong to the parent.

READY TO MEASURE (independent worker only): branch `experiment/rfwgc-port`,
source pin `5847df30f1af15d10b743d90a24b1429b290d358`. Sensitive paths are
conversion-only entry/exit, snapshot claims and domain retirement, nested-root
admission and callback suspension/resume. Proposed workloads: scalar and
storage-only calls with zero/nonzero domains, nested imports, callback-heavy
calls, and contended last-pin/Close churn. No performance result is asserted;
this designation does not waive the open ARM64 qualification or final review.
