# Round-9 feature, linking, and helper-protocol assessment

Owner: test worker; no production edits. This report covers H4-F1, H6-F1,
H6-F2, H1-F1 only. H2-F1/H6-F3 belong to the other lifecycle worker.

## Exact scope and provenance

- Immutable main: `90f2b57dafa7f8c3cc7e3aca38b91148f15e1d1e`.
- Immutable committed port: `ca30843801956bae67d5cd1219f66d191518c125`
  (mature-region, native, and foreign-stack prerequisites included).
- Proposed PR1020 foundation: `ce63d517a55f2981b7ccf6e150d1b9050fb5f953`
  (`refs/port-assessment/pr1020` at inspection).
- Local PR1020 tune: `504dcae5a626403407158e5aade6442de1563787`.
- Slipstream report: `dcb45b56386327a1f71fb039ed02e13562e1e2d7:hunt-findings.md`,
  reviewed source `82d546679bab4887acfab056f6ad773a969b1e03`.
- Starshine read-only audit: `starshine-master-base`,
  `1d6c0af47140299d8f3c01ed03060d279c43ee03`, remote
  `https://github.com/jtenner/starshine-mb.git`. All cited source was read with
  `git show`/`git grep HEAD`; unrelated dirty tests were not used or edited.

All executions use git archives plus the owned test fixture, not the live shared
worktree. Rooting 997/1026/1027 and collector 1030/1019 edits/commits continued
while this work ran (observed shared HEAD `b5f0f76d237e5bccbcbf7c41593b5b06a12399c4`).
None of those later changes are qualified here. No switches, production edits,
subdelegation, PR, push, closure, benchmarks, or performance measurements.
Tests/docs cannot alter production performance.

`source-manifest.json` identifies exact Git blobs and SHA-256 values.
`source-evidence.txt` preserves the pertinent immutable source with line numbers.
`results/` records full stdout, command, source pin, exit status, and skip status.
Normal Go test duration annotations are test-run output, not performance data.

## Disposition recommendations for the parent

| Finding | Main | Committed port | Recommendation |
| --- | --- | --- | --- |
| H4-F1 | Core3 shared memory and atomic-load module rejected; Threads override accepted | Same | PRESENT-OUT-OF-SCOPE: public standards feature-mask omission, unchanged by port |
| H6-F1 | Type-free `(ref null none)` global instantiates and reads null | Same | ABSENT for the exact reported module/setup failure |
| H6-F2 | Bottom matcher accepts struct/array, but both immutable imports fail later at GC-domain admission | Same | PRESENT-OUT-OF-SCOPE recommendation for the observable linker defect; source cause differs from Slipstream |
| H1-F1 | Serial collector, no helper publication/wait/STOP protocol | Same | ABSENT at these pins; proposed parallel foundations also avoid the reported alias, but final adaptation requires recheck |

No INCLUDED-FIX is claimed: this worker integrated no production fix. The parent
owns final dispositions and combined gates. If the parent treats the H6-F2
GC-domain admission failure as part of its active rooting territory, it must
remain pending integration and green gates, rather than be marked fixed/absent
or exported as a separate PR. The standalone H6-F1 success does not prove the
pre-existing-provider persistent-root invariant of H2-F1.

## H4-F1: confirmed standards profile omission

`src/wago/config.go:85-101` calls CoreFeaturesV3 the mandatory release set but
omits CoreFeatureThreads. The broader `coreFeaturesWago` adds it, masking the
problem in ordinary default configuration. `validatorFeatures` maps the bit to
`Threads`; this is a public Compile-path defect, not only an internal validator
hypothesis. The exact 14-byte fixture is
`0061736d01000000050401030101`. With CoreFeaturesV3 it fails with
`compile: unsupported memory shared (threads disabled) at memory 0`; explicitly
enabling Threads passes. The missing-maximum fixture
`0061736d010000000503010201` is rejected in both configurations. An independently
encoded shared-memory i32.atomic.load function likewise fails Core3 and compiles
with Threads enabled. These tests assert standards-correct success, so the
known defect remains a visible FAIL in the evidence.

Scope: config/profile membership is pre-existing and identical in main/port,
not a collector change. Proposed owner follow-up: add Threads to CoreFeaturesV3,
retain backend SupportedFeatures admission and missing-max checks, and revisit
the experimental feature label. No production change or separate PR here.

Starshine source corroboration: `src/validate/validate.mbt:1017-1031` admits shared
MemType with a maximum; tests at 11894/11908 cover valid max/missing max.
`src/validate/proposal_features.mbt` gates newer proposal slices, not ordinary
Threads. This is a source audit, not an assertion of executed Starshine gates.

## H6-F1: exact trigger absent

The fixture has only global/export sections, zero types, and explicit
`(ref null none)` / `ref.null none`. Compilation, Instantiate, export reading,
and a zero/null bit assertion pass on both snapshots in all modes. The test
also confirms `len(c.Types)==0`. No collector is allocated for this null-only
module; that is valid because there is no heap object to normalize or retain.

`src/wago/constexpr.go:158-202` explicitly decodes none and writes zero bits.
`instanceBuilder.prepareCollector` (`instantiate.go:287` onward) uses actual
execution/declaration/helper requirements, not a type-section decode callback;
its null-only early return succeeds. `gc_type_domain.go:gcRefMatchesValueType`
checks nullability before consulting a collector. Thus copying Slipstream's
unconditional setup shape would not be a justified fix for this exact Wago
trigger. This conclusion does not certify future managed-object storage or
linked providers. Parent owns any active registration/root integration.

Starshine is a module optimizer/validator library (README and `src/rume` module
transformations), not this native runtime. Its `GlobalType` validator validates
the value type directly (`validate.mbt:1010`); no type-section-triggered native
collector setup counterpart was established in that sibling audit.

## H6-F2: subtype correct, observable linking failure independently confirmed

The provider includes an unused `() -> ()` function type, isolating the report's
H6-F1 setup variable. It exports immutable `(ref null none)`. Separate consumers
import it as nullable defined empty struct and nullable defined i32 array; all
modules compile. Both public Runtime links fail with:

```
imported global "env"."g": collector-reference global requires producer and importer in the same Runtime GC domain
```

Mutable variants correctly fail earlier with `exact type is incompatible with
required structural type`. The independent direct bottom-matcher probe passes
both struct/array covariance and reverse-subtyping/invariance negatives.

`type_descriptor.go:497-539` already admits AbstractHeapNone below defined struct
and array. `globals.go:1717-1724` uses that predicate for immutable imports and
requires both directions for mutable ones. The actual failure comes from
`globals.go:1809-1833`: it requires nonnil identical collector domains **before**
it examines whether the compact value is null. A harmless function type does
not create a collector in Wago; the provider remains collector-free. The
feature, matcher, and globals files are byte-identical between the two pins.
This is not evidence that the missing Slipstream matcher exists in Wago, nor
permission to report the observable valid-module linking scenario ABSENT.

Recommended scope is existing linking/admission policy outside this collector
port. If accepted into the port's rooting integration, the proposed bounded fix
is to admit immutable, exact nullable-none null storage without requiring an
object domain, while retaining reference-store identity, owner liveness, exact
typing, and mutable invariance. A broad exemption for mutable null GC storage
would be unsafe: it may later hold objects. Alternatively ensure a shared domain
for every linked GC slot through the parent's lifecycle work. Re-run this exact
fixture, object-bearing imports, closed/cross-Runtime owner rejection, and all
three modes after the chosen integration. This worker made no such edit.

Starshine `src/validate/match.mbt:898-916` explicitly accepts none below defined
struct and array; `passes/local_subtyping.mbt:143-151` independently agrees.
It does not supply evidence about Wago's runtime collector-domain admission.

## H1-F1: protocol and teardown proof

Main/port `native/collector.go`, `collect.go`, `mark.go` implement collection
synchronously on the caller. CollectFull clears marks, marks roots, sweeps and
reclaims directly; Close discards reservations and storage directly. There is
no helper pool to notify or join, not merely an absence of the word futex.
The port's mature-region uint64 epoch is allocator bookkeeping, not a wait word.

PR1020 and tune `native/parallel.go` have three separate mechanisms:

1. Phase start uses unbuffered worker `wake chan struct{}`. Phase and work state
   are initialized before sends; each send synchronizes with the receive that
   starts runPhase. Sending before receive blocks, so an early notification
   cannot be lost. joined.Wait precedes coordinator postprocessing/reuse.
2. Trace-idle predicate is `len(queue)==0 && active!=0`, read under `p.mu`.
   Queue publication and active-count changes occur under the same mutex.
   Cond.Wait registers notification before releasing it. Broadcast occurs while
   holding it; waiters reacquire and recheck in a loop. `idle` is merely a sharing
   hint, not the predicate. No wider generation is truncated for waiting.
3. Teardown closes each wake channel, then waits on stopped. Close persists for
   a worker that has not begun receiving. loop's deferred stopped.Done joins
   helper exit before Collector.Close releases storage. This proof assumes the
   existing serialized collector use; it does not promise concurrent Close
   during a running phase. Normal p.run waits for all workers to return first.

`epoch` is uint32 for mark claims only, with clear(claims) on wrap; it is not a
phase or STOP token. Local tune changes capture/copy admission work only and
leaves the synchronization shape unchanged.

Protocol probes execute production trace(), loop(), run(), and closeParallel():
force the registered-Cond-wait boundary, force STOP before the helper starts,
and exercise phase delivery/join. The first two run with epoch values
0xfffffffe, 0xffffffff, 0 directly, requiring no billions of cycles. There is no
full-width generation/low32 wait pair to force into an alias. The tests are not
a new synthetic futex model and do not claim to exercise arbitrary simultaneous
Close/collection, mark-claim rollover, or the future Snap integration.

Required recheck on the **final integrated PR1020/Snap commit**: inspect every
phase publication/wait/completion/STOP path and type width; retain channel
send/receive and mutex-predicate ordering (or prove any replacement wake-word
protocol); force registered-wait publication, pre-receive STOP, and relevant
boundary values; verify join before reclamation and before epoch/claim reuse;
run collection correctness and the probe in all three modes. If futex tickets
are introduced, test both +2^32 alias and 0xfffffffe-to-STOP before kernel wait,
and inspect ticket exhaustion. Current serial absence is not final assurance.

Starshine's optimizer/library does not expose this native helper protocol;
Slipstream supplies the verified futex trigger. No Starshine runtime execution
claim is made.

## Reproduction and evidence handling

Run from the existing Wago worktree:

```
python3 docs/rfwgc-evidence/r9-features-protocol/run.py \
  --go /home/dev/.local/share/mise/installs/go/1.27.3/bin/go \
  --output /tmp/r9-feature-protocol-rerun
```

The runner creates disposable archives, copies the exact `.go.txt` probe to the
proper package, runs ordinary/wagodebug/wago_regalloccheck, and records real exits.
There are no tool/fixture skips. Go is explicit because mise refuses untrusted
archived mise.toml; no global trust/settings change was needed. The protocol
fixture is installed only in foundations containing its production types.
Keeping probes under the evidence directory preserves intentionally red,
out-of-scope regression evidence without silently changing the port root suite.

## Final focused matrix

| Immutable snapshot | ordinary | wagodebug | wago_regalloccheck |
| --- | --- | --- | --- |
| main 90f2b57 | Feature probes exit 1, four failing leaves | Same | Same |
| port ca308438 | Feature probes exit 1, four failing leaves | Same | Same |
| PR1020 ce63d517 | All three protocol tests PASS | PASS | PASS |
| tune 504dcae5a | All three protocol tests PASS | PASS | PASS |

The four failing feature leaves are Core3 shared-memory acceptance, Core3 atomic
load acceptance, immutable struct import, and immutable array import. All other
leaves pass: explicit Threads positives, missing-max negatives, no-type null
global, direct bottom matcher, and both mutable covariance negatives. Zero skips.
These are focused probes, not full-package or full-port qualification gates.

## Slipstream fix-wave check (read-only)

`slipstream-fix-wave.txt` records the final remote/local ref observation and exact
patches inspected. Remote master remained `82d546679bab4887acfab056f6ad773a969b1e03`;
none of these source fixes was established as landed at that observation:

- H4: branch fix/r9-core3-threads at
  `d733afed8e3a5a283794817f1e89ffa2d2bccd55`; production patch
  `eef4bd82d7fda98e11720f39a6f9544740786601` enables extended_threads in
  core3.goose, with harness/tests aligned. Wago's analogous feature-mask edit is
  proposed to owner as out-of-scope; it was not silently mirrored into production.
- H6-F2: `f80e06c76cac916c5b5dc719bb1efb6ec253505c` adds defined struct/array
  handling under HEAP_NONE. Wago already implements that exact semantic shape;
  its independently reproduced later domain rejection needs a different fix.
- Registration cluster: `21c7cd8557c8b0af70a1d2bc2af180764d23a721` registers
  before the first non-custom/non-type section and installs missing imported
  table/global root regions. Parent/rooting owner must assess this shape for
  H2-F1. It is not evidence that Wago's passing H6-F1 trigger needs a transplant.
- H1: `bea9f723fb9fc0126e57ef56fab9a37f714f7e53` caps NG_TICKET_MAX at
  0xfffffffd. Existing dispatch fails closed at the cap, preventing wrap or
  collision with STOP/DEAD low words. This is the unlanded fix-wave shape;
  measurement/landing is not assumed. Wago's channel/condition protocol does not
  need that numeric cap while its current synchronization shape is preserved.

The hunt report itself contained no production fixes. A later source landing
or a changed Wago integration pin requires a fresh mirror/gate decision.
