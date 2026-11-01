# Host lifetime and subsequent port stages

## Close before guest process exit

`7822fc8c4` adds `proc_exit_status`: encoded statuses 256–511 map to 0–255;
other statuses return -1 for existing trap handling. `cli_invoke` returns the
process status without printing normal results. `run_store_session` handles
exit from each imported dependency's initialization, the main initialization,
and the selected export. `finish_run_store` releases the Store, closes its GC
runtime, reports close failure as exit 1, then uses the requested guest status.
It does not establish clean teardown for every earlier `exit(1)`/trap branch.

The source test `clean_exit` allocates a struct then calls Facet `proc_exit(0)`.
It requires process return 0, empty stderr and exactly one GC report with the
expanded reservation. This tests the exported-invocation path; imported-start
and main-start handling are source-inspected but not new test cases in that diff.
The same commit corrects reservation assertions to 512 MiB heap/64 MiB metadata
and adds the `too_large` fixture consumed by the previous diagnostic test.
Therefore assess the landed sequence together, not each intermediate test commit
as an independently executed passing gate.

Wago requirements: carry exit as an invocation outcome through cleanup; cancel
unpublished native reservations, unwind suspended activations/leases, release
owned roots and resources, and close collector backing only when domain ownership
permits it. Embedded execution must return its existing exit/error contract,
not terminate the embedding process. One instance exiting must not free another
instance's shared-domain roots or a retained public GC token.

Concrete later tests: exit 0 and nonzero after GC allocation; exit from main start,
imported start and exported call; exit during nested host reentry; normal return
and ordinary trap as controls; close refusal/failure and repeated close. Assert
no live activation/reservation leak, no duplicate close/report, and survivor
instances/tokens remain usable until their actual owners release them. Process
exit tests need bounded subprocesses; embedded tests should assert exact returned
status and subsequent domain use. Existing Stage 3 close/reentry coverage does
not already cover all of these exit paths.

## Externrefs and host array views are different ownership models

At the new source pin, MoonBit `mb_new` creates instance-owned resource records
with `GC_INTERNAL_EXTERN` tagging, owner and slot identity. `mb_object` validates
tag, owner/resource lookup and kind. Strings, bytes, lists, builders and readers
use separately allocated host backing; they are not movable WasmGC payload
addresses. Slots are bounded at 65536. `mb_reserve` bounds both individual and
aggregate backing at 256 MiB; `mb_dispose` releases backing and clears a consumed
response record. These are bounded host resources, not proof of tracing/recycling
every unreachable externref. Do not reinterpret source tag bits as Wago `Ref`.

Wago should retain its `reference_store.go` domain/token owner and generation
checks, host rooting and result transfer. Distinguish opaque host values from
WasmGC handles and filesystem resources. A host token's lifetime may exceed an
individual guest activation. Cross-domain rejection, owner close with retained
tokens and backing disposal must follow Wago's API contract, not source's
instance table indices. No new general externref collector is inferred here.

Facet adds `host_gc_array`/`host_gc_new` through native/legacy bridges. Array views
validate managed reference, array descriptor, element kind/width and write
mutability; checked ranges use subtraction after offset validation. Native ABI2
resolves heap words; legacy resolves handles. Nested I/O validates child arrays
and builds bounded iovecs. The explicit source lifetime rule is synchronous use:
no guest callback, collector safepoint or retained view while payload pointers
are live. Allocating calls allocate first, resolve afterward, then initialize
fresh arrays and publish results. Source's native `host_gc_new` enters
`gc2_allocate`, which publishes frames as needed; this is not a license to leave
Wago host-created results unrooted across a collecting transition.

Wago equivalents must use borrowed-storage exclusion for raw views, root the
outer/nested arrays for the entire operation, reload payload after allocation,
and stage newly allocated result references through `stageGCHostResult` and the
existing host invocation suspension/resume path. During a borrow, collection
must refuse without mutation, or use another already-proven Wago contract; never
retain a raw slice across reentry. Validate mutability/width/domain before writes,
preserve zero/error results and partial-output contracts, and separate fresh
immutable initialization from later writes. This is a lifetime adaptation, not
a requirement to implement every Facet/MoonBit filesystem import in the GC port.

Source `test_moonbit_host.py` covers UTF-16 surrogate arguments, file bytes,
confined-path denial, isolated environment, disabled-worker capacity, concrete
Facet result allocation, partial copies, wrong element width, opaque-token/fd
separation, bulk requests, immutable write rejection/fresh immutable allocation
and denied network authority. It runs default and `--region-scalar=off` for each
supplied runner. These are source tests, not Wago results or proof of repeated
collection/reentry during every host operation.

Required Wago adaptations: host creates numeric/packed array result while outer
caller GC locals remain live; force collection before result publication and
on immediate guest consumption; wrong-width/immutable/foreign-domain rejection;
borrowed outer/child arrays with attempted collection; reentry/trap/cancel cleanup;
opaque externrefs surviving collections without becoming GC handles; shared-domain
close with retained results. Pair enabled region policy with default policy and
assert actual region occupancy/collections, not only the numerical result.

`tools/moonbit_host_verify.py` distinguishes `loaded 1` from process success:
it requires return 0, empty stderr, exactly one load marker and no link/trap
markers. A deliberate missing import can return process 0 with `link 128` and
must fail qualification. It verifies module hashes and checks 24 load records
across its two modes. This is load evidence, not workload execution or GC coverage.
Apply the same distinction to the Wago corpus; matching binaries or successful
linking cannot close runtime/oracle gates.

## Guest stack and facts evidence

`--guest-stack-bytes` accepts decimal page-aligned sizes 1–256 MiB. Zero internal
selection retains old defaults: 4 MiB operand and local stacks, 1 MiB native stack.
An explicit value sizes each of the three separately, not one combined budget.
Wago should preserve its own stack configuration, overflow behavior, frame maps
and root admission. If a corpus workload needs more stack, record the exact
configuration and prove roots survive deep calls/spills and overflow cleanup.
A larger stack must not hide a GC failure or silently change matched controls.
No new stack-option tests were added in that source commit.

The facts refresh changes consumer source-line metadata only, as verified by
recursive JSON comparison in `facts-fingerprint.json`. It does not add scalar
consumers, mature lifetime proofs or native execution evidence. Rebind any future
proof to actual compiler/source/configuration fingerprints; do not infer validity
from a documentation update or reuse previous execution claims after code changes.

## Native reservation stage decisions

Stage 3 deliberately disables native allocation batches in region mode. Keep
that guard until the new writer has an explicit contract and tests. Source's
single-window Eden fast path is not Wago's region reservation implementation.

1. Reserve one bounded non-spanning region run plus handle capacity and physical
   bytes before native entry. Record region identity, bounds and invalidation
   epoch; no generated writer may cross the run or use a stale refreshed view.
2. Cursor advancement means a fully initialized, published object. On cancellation,
   reclaim only unused bytes/identities; preserve published handles and charge
   accounting. `CancelNativeAllocationBatch`/`discardNativeStructHandles` are
   Wago's existing boundary, not a replacement for region ownership tracking.
3. Refill may collect only after exact frame/initializer roots are admitted and
   prior reservations invalidated. Reentrant host calls, traps, fallback, backing
   relocation, close and partial batches all need cancellation/retry fixtures.
   Large arrays retain their physical extent and separate cancellation path.
4. Dead/handleless scalar charges must not silently regain a bypass into a region
   reservation without publication/charge tests. Maintain checked fallback and
   default-policy oracle. If ABI fields change, update generated offsets on all
   consumers and cached artifact validation together.

## Future parallel collector decisions

Full parallel behavioral parity remains open; this source assessment does not
waive it. Implement only after destination transactions, roots and serial region
semantics are qualified. Source prior landings retain per-helper tickets,
acknowledged completion and helper drain before release; a wake is not completion.
Late/duplicate completion cannot satisfy a new epoch, finite tickets cannot wrap
into STOP/DEAD, publication failure drains dispatched workers, and supervisor
failure poisons the Store and disables allocation. Close must wake/join live
helpers before unmapping. Go and non-x86 implementations need their own memory
ordering proof; source locked-instruction/compiler-barrier assumptions do not
transfer automatically.

Active work sizing and destination reservation counts remain independent.
Owner-only phases must not publish helper epochs; source thresholds are not Wago
calibration. Establish exclusive mutation ownership for each destination run,
handle publication, region metadata and work queue, with all root/domain owners
held through completion. Test serial/parallel semantic parity, duplicate/delayed
completion, partial dispatch failure, worker loss, ticket exhaustion and close
under pending work. Read the Stage 3 mirror's wake/join protocol before design.

Later implementation measurement handoff should name exact branch/head and changed
allocation, promotion, barrier, host and scheduling paths. Proposed workloads:
no-GC/scalar controls; escaping graphs across region/card boundaries; low/high
survival and fragmented mature churn; required refusal/retry; LOS arrays/range
writes; native partial/refill/cancel batches; nested host results/close; and
serial/parallel workload sizes around dispatch thresholds. Compile costs use the
same module bytes/options. Only an independent measurer with the global lease,
after tests drain, may produce timing evidence. This documentation-only worker
has no performance-sensitive change to hand off as READY TO MEASURE.
