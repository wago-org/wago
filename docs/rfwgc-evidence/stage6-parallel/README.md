# Stage 6 parallel collector integration

Production slice: `bc05eef3e`; portable callback/failure test slice:
`779fb3f70`, on `experiment/rfwgc-port`. This integrates the
native collector portion of published #1020
`ce63d517a55f2981b7ccf6e150d1b9050fb5f953`, relative to actual common base
`27abf4470e1137c18233d246e05dee6fb76ef266`, and the useful tuning at
`504dcae5a626403407158e5aade6442de1563787`. It preserves the committed
1030/1019/1003/1022 prerequisites and transactional mature/LOS/Eden behavior.
No final INCLUDED disposition, PR, push, closure, benchmark, timing acquisition,
measurement lease or review fan occurs here. Existing shared index/dirt, rooting,
backend, trampoline and H6 portable source ownership were preserved.

## Implemented contracts

- `Config.Workers` accepts zero/one (serial) through eight total participants.
  Tiny and TinyGo targets reject helpers. Pools are lazy, bounded by configured
  participants and processor availability at creation. Trace, sweep and copy
  independently choose active subsets from their phase work size; participation
  can shrink/grow without changing reserved destination capacity. Small work,
  one processor, and custom object-edge tracing retain serial paths.
- Unbuffered channel delivery publishes prepared phase state. Every dispatch
  allocates a fresh phase identity and records exactly one completion per
  participant. WaitGroup join seals helper writes before coordinator inspection,
  mark-bound merge, freeing, rollback, publication or scratch/claim reuse.
  Duplicate/stale dispatch poisons. Numeric sequence is diagnostic only, and
  uint64 wrap cannot alias a wait/completion identity. Claims have their separate
  uint32 epoch, cleared on wrap.
- The shared trace queue is bounded at 4096. Overflow remains on private work
  stacks, with no dropped task; idle wait predicates and publication share one
  mutex. Private storage remains graph-sized (claimed handles plus array chunks),
  as in #1020, not a fixed per-worker arena. Cancellation wakes idle workers;
  all dispatched peers drain before private/shared work is discarded. The next
  collection cannot reuse phase state before that join.
- Recoverable injected faults return an error with intact source handles and
  payloads. Partial trace marks remain covered by per-worker high-water maxima
  merged after join, including failed phases. Unexpected panic or goroutine exit
  poisons the collector: checked API admission rejects reuse and native fast
  admission fields are disabled on refresh. Close drains helper lifetime before
  freeing storage. Process-fatal Go runtime faults/OOM are not promised contained.
  Collector operations, including Close, retain the existing serialized-mutator
  contract; arbitrary concurrent Close versus collection is not an API promise.
- Copy workers write only preadmitted disjoint destinations. Every physical
  destination, including rounded size and absorbed tail, is claimed by the serial
  planner before dispatch. Helpers never allocate, resize backing, edit descriptor
  occupancy/free trees, publish handles, poison sources or invoke host callbacks.
  Mature reservations commit only after successful join. Abort rolls them back;
  successful coordinator publication poisons the old young source exactly once.
  Zero-promotion and survivor-only operations retain zero mature credit demand.
- Sweep workers discover only. The coordinator releases each dead identity after
  join using existing `releaseHandle`/`deferThroughputFree`, so mature occupancy,
  pending free debt, cards and reclamation callbacks retain one owner. Existing
  Eden/card cleanup precedes descriptor retirement. No page is independently
  freed as a second object allocation.
- Tuning keeps the single-worker dispatch guards, omits redundant captured lists
  for complete young survival and stops copy-threshold counting once sufficient.
  Current sorted physical-span plans and transactional boundaries remain intact.

## Joint host callback integration

H6-F3 owns `object_edges_*`, `object_trace_*`, `reclaim_*` and their portable
counterparts/tests. None was edited or committed here. Once its Linux portable
files appeared, the owner reported scalar shadow-edge failures. Read-only audit
confirmed that AMD64's two pointer-free minor drains and two payload-only direct
scanner dispatches bypassed external edges. All four guards in owned `mark.go`
now require zero external edges before taking those fast paths. The H6 tests
then passed in all three modes (slice logs). This is an integration repair,
not a claim that RV64's original callback path was broken.

Audit: `scanObjectRefs` visits external edges before `HasRefs` filtering;
`scanObjectRefsRange` includes them in bounded work. `scanRememberedCards` uses
complete external-edge scanning before physical-card shortcuts;
`finishMinorCardMetadata` and `handleContainsNurseryRef` preserve those children.
`WriteBarrierObject` remembers old external owners even when payload is empty.
`scanObjectMark`/`scanObjectNursery` are payload-only but are now selected only
when external-edge count is zero. Parallel trace declines any installed source;
root/conditional-slot callbacks and payload copy publication remain coordinator
operations. H6 owner independently confirmed sweep callbacks happen after join.

`callback_integration_test.go.txt` preserves the integration fixture installed
in the final snapshot before the portable API commit. After H6 committed
`6c499585c`, it was promoted into the owned
`parallel_callback_integration_linux_test.go`; the earlier empty-source test
was broadened from RV64 to Linux. No H6-owned source files were edited.
It exercises policies 0/6/12, actual parallel sweep, external edges on scalar
objects, serial source tracing, conditional persistent cycles, surviving minor
collections and exactly-once reclamation after join. Domain-wide host quiescence,
retention bounds and target-specific RV64 receipts remain the H6 worker's gates.
The final snapshot records the exact portable source overlay; its production
bytes match the later committed portable API. It does not claim those files as
this worker's commits.

## Qualification and provenance

`validate.py` archives a committed base and replaces only the disposable native
package with the recorded local snapshot, including H6 portable API files and
the owned callback tests. The recorded final run installed the same callback
fixture explicitly before its promotion into a normal Linux test.
`final/source.json` records the base, source
hashes and callback fixture hash. `production-drift.json` confirms unchanged
production bytes against the committed test-slice head. It runs full collector packages, focused AMD64
backend and guest GC suites, plus parallel/protocol race tests in ordinary,
wagodebug and wago_regalloccheck. Logs, actual commands and exits are retained.
Go test's printed durations are ordinary test output, not performance samples.

The initial root-level logs predate the final slice and portable callback repair;
they exposed a stale Config size assertion, now corrected. They are diagnostic
history, not the final gate. `slice-results.json` records all-green three-mode
focused source, H6 callback and layout tests preceding the production commit.
The subsequent `callback-final-results.json` and `failure-final-results.json`
exercise strengthened exactly-once/native-poison assertions and failed-full to
minor to full retry, under the race detector in all three modes.
`disabled-target-results.json` separately tests standard-Go build-tag fallback
admission; it does not certify execution under the TinyGo compiler.

New tests cover independent reachability oracles, policies 0/6/12, worker settings
1/2/4/8, graph death and high-handle reuse, full/minor/full transitions, interrupted
root enumeration, claim epoch wrap, diagnostic sequence boundaries including
2^32 and uint64 wrap, registered-condition publication, STOP before receive,
delayed participant join, duplicate dispatch, helper exit, sticky poison,
partial-copy source preservation, mature rollback/retry, queue overflow and
native reservation cancellation. The 64-byte-region fixture uses sufficiently
many small Eden objects to force actual copying; larger nodes would become
nonmoving LOS and make that fault test vacuous.


Final result: all twelve `final/results.json` commands exited zero.

| Mode | Collector top-level passes | Focused race passes | Backend passes | Guest passes |
| --- | ---: | ---: | ---: | ---: |
| ordinary | 462 | 19 | 40 | 410 |
| wagodebug | 495 | 20 | 38 | 404 |
| wago_regalloccheck | 462 | 19 | 41 | 410 |

Existing suite skips are listed in `summary.json`; no new parallel, protocol or
callback test is skipped. Backend counter-dependent tests retain their existing
diagnostic-tag skips. These scoped passes do not claim the parent full-root,
new host domain graph or actual TinyGo compiler gates.

## Source cross-check and R9 H1-F1

Read-only Slipstream `82d546679bab4887acfab056f6ad773a969b1e03:gc/native.goose`
was inspected for dispatch/ack/complete/join/supervisor and admission contracts;
prior exact excerpts remain in `../rollup-collector-assessment/slipstream-contracts.txt`.
Starshine `e075c47e61b01ab9e4fd0ae4a7ca47b755e3fca1`'s
`docs/wiki/wasm-gc-core-boundary.md` was read before integration triage: it supplies
model/validation context, not a native helper or host shadow-edge collector.
The source's raw forwarding/futex representation was not transplanted.

R9 H1-F1 low32 alias is absent from this implementation's waits: worker starts use
channel delivery, trace waits use a mutex-protected predicate, phase completion
uses fresh identity plus join, and STOP uses persistent channel closure plus
lifetime join. Neither sequence nor claim epoch is a truncated notification
word. Tests exercise these actual paths at boundary values. This scoped result
must be rechecked if a later run/evacuation integration changes the protocol;
it is not a final whole-port review verdict.

## Remaining integration gates and handoff

- Parent owns public runtime/CLI worker plumbing and guest helper-participation
  fixtures outside the assigned native directory. Native `Config.Workers` is
  implemented; this slice does not silently edit those other owners' paths.
- Phase sizing is a Go work-count policy, not a transplant of Slipstream's
  history/pressure heuristics. Its correctness independence from reservations is
  implemented; tuning/measurement and collection-pressure integration remain.
- Exact-span preadmission is preserved. Helpers gain no destination allocator.
  The explicit bounded run planner, independent reservation count/slack/credit,
  source exclusions and run ownership required by the full-port design remain
  follow-on work, not declared superseded by this exact-span stage.
- Optional mature evacuation is reserved for its separate implementation owner.
  Read `../stage6-evacuation-design/{README.md,tests.md}` at `0b6cceb86` for the
  contract. Reuse copy/drain ordering, but do not reuse young source poisoning or
  promotion accounting for old-to-old moves; preserve card/identity ownership.
- Parent combined final-head tests, H6 bounded host graph tests, TinyGo actual
  compiler/foreign-stack tests, other platforms, corpus/oracles, public config,
  run admission/evacuation, independent measurement and the separate mandated
  final review fan remain open. No final INCLUDED until those combined gates.

READY TO MEASURE after correctness qualification and worker drain, by an
independent holder of the global lease. Branch `experiment/rfwgc-port`, production
commit `bc05eef3e`; final evidence/test commit is named in the handoff. Sensitive
paths: native Config/closed checks, serial callback scan guards, mark/highwater,
helper dispatch/trace/sweep/copy, promotion admission/publication, native view
refresh and Close. Proposed workloads: matched workers 1/2/4/8 under policies
0/6/12; dense/sparse cyclic graphs, large ref arrays/private chains, fully surviving
and sparse survivor copies, immediate promotion, fragmented mixed old/LOS,
zero-credit cycles, cancellation/retry, source callbacks and serial/no-GC controls.
No performance direction is asserted.
