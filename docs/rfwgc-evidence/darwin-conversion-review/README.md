# Independent Darwin conversion production review

## Verdict

Reviewed immutable source `3242c18a19620aa77f39dd63cb5cd7544d4ca70a`,
including Darwin enablement slice
`3f616188315096d7ce1b5fe96b747516f99ffff0` and unsupported-adapter slice
`3242c18a19620aa77f39dd63cb5cd7544d4ca70a`.

* **Darwin ARM64 host-adapter slice (`3f6161883`): no production blocker found
  by this static/Linux/cross-build review.** The enabled receipt, physical
  container, conditional owner graph, scheduler, root, sidecar tracing and
  reclaim bodies are the unchanged Linux implementations. Their dependencies
  select coherently on Darwin ARM64, and no Linux syscall, mmap, signal ABI,
  raw offset, or Linux-only synchronization assumption was found in the moved
  files. This is not overall port approval and is not native Darwin execution
  credit.
* **Unsupported-adapter repair (`3242c18a1`): BLOCKED by one verified ingress
  namespace omission.** `setConversionGlobalPublic(access, g, value)` selects
  `g.owner.instance` whenever it exists and therefore does not also inspect a
  distinct accessing Instance. For `Instance.SetGlobalValue` on an imported
  physical global, the opaque public word can belong to the accessing/publishing
  Instance rather than the global owner's conversion namespace. The new lookup
  can miss, the ordinary directory also misses by design, and legacy internal
  conversion handling can publish the word without an unsupported-adapter
  receipt. Minimal repair: check both distinct conversion namespaces before the
  ordinary-directory fallback. Require direct and imported-global controls plus
  forged, cross-store, null, immutable and closed sequencing. Review that repair
  only at a new committed SHA; no result in this report transfers to it.

No production or workflow file was changed. No benchmark, push, PR, dispatch,
subagent, QEMU run, actual TinyGo run, or native Darwin run was performed.

## Darwin ARM64 platform audit

Darwin ARM64 and Linux select the same `function_environment_*_native.go`,
`gc_conversion_{receipt,store,host_container}_host.go`, and
`gc/native/{object_edges,object_trace,reclaim}_native.go` bodies. Darwin selects
the complementary `*_other.go` files out. `go list -json` receipts are retained
for Darwin ARM64, Linux AMD64, Linux RV64 and `wago_target_tinygo`.

The native bodies use Go mutexes/atomics, typed collector handles and existing
instance native-state locking. Static inspection found no syscall, mmap,
signal-handler, OS-page or hard-coded ABI dependency in the ported bodies.
Darwin ARM64 already has its architecture-specific frame-root/runtime-frame and
host-activation files. The conversion store routes physical global/table,
initializer, range/bulk, grow, clear and Close operations through transactional
receipt acquisition/retirement; publication follows successful acquisition,
and replacement/rollback retires the correct receipt. The conditional owner
graph retains domain/identity claims rather than permanently retaining the
publisher Instance, and existing publisher-Close, cycle, wakeup, finalizer and
eventual-release tests ran in the focused and race selectors.

The sidecar object-edge and mark callbacks remain serialized by the collector's
existing mutator/topology protocol. Parallel tracing is declined while an edge
source is installed; reclaim observers are detached on Close. This review found
no blind Darwin tag expansion across an architecture-dependent body: the port is
limited to Darwin ARM64, whose frame and activation implementations already
exist. It does not establish native callback/stack behavior without execution.

Linux RV64 continues selecting its compact conversion receipt/store files and
Linux native graph/collector sidecars. `wago_target_tinygo` continues selecting
the fallback graph, conversion and collector sidecars. No RV64 compact protocol
or actual TinyGo support claim is changed.

## Source bug and positive oracles

The native failure source is coherent with the code. A public result from
`make(42)` is an opaque conversion word, deliberately absent from the ordinary
externref directory. Legacy `internalExternForGlobal` nevertheless recognizes
it, explaining why the old fallback returned unhandled and `Global.SetValue`
returned nil. `TestConvertedMake42ClassificationAndReceipt` positively checks
that classification, receipt/claim installation before collection, value 42
after collection, clear retirement and final domain release.

`TestConvertedInitializer55PersistentRoot` checks a real initializer receipt,
drives the production receipt-owner graph collection boundary, and observes 55
after collection. `TestExternrefGlobalHoldingConvertedGCReference` restores the
positive initializer/global behavior and observes 42 after collection. These
are materially stronger than accepting or weakening the old Darwin rejection.

The unsupported test correctly checks owner-namespace rejection before ordinary
directory fallback and retains null, ordinary-token and invalid-token paths, but
it constructs only a global whose physical owner and conversion producer are the
same Instance. It therefore does not cover the blocked imported-global shape.

## Independent checks

The runner exported the exact commit with `git archive`; archive SHA-256 is
`223cd056e0be0f620a98810c351a5500fc3df498223d4f42fc9bf0401db6d4dd`,
matching the implementer's immutable source record. Go is
`/home/dev/.local/share/mise/installs/go/1.27.1/bin/go`, SHA-256
`30969f97169d7f43fe6a085873d75613adc21e30818a8c61d95bd27275df4624`,
with GOROOT `/home/dev/.local/share/mise/installs/go/1.27.1`. The temporary
source was removed after execution.

All independently executed commands passed:

| Gate | ordinary | wagodebug | wago_regalloccheck |
| --- | --- | --- | --- |
| Focused H2/H6/conversion/shape | PASS | PASS | PASS |
| Same focused selector under Go race | PASS | PASS | PASS |
| Unsupported adapter metadata test (`wago_target_tinygo`) | PASS | PASS | PASS |
| Darwin ARM64 Wago test-binary cross-compile | PASS | PASS | PASS |

The focused runs preserve the existing assertions and include receipt/store/
host-container/import transactions, public global behavior, conditional roots,
nested ownership, publisher/store Close, cycle/eventual release, wakeup and
shape/admission controls. Cross-compilation proves build and selection only.

The owner reported, after this review began, that a separate Darwin worker had
immutable-3242 ordinary focused/full-Wago/collector/backend/race PASS, Darwin
ARM64 cross-build PASS in all modes, and Linux ARM64/RV64 ownership QEMU six-cell
PASS while debug/checked cells were still running. Those are external status,
not independently inspected receipts here, and they do not repair the fallback
namespace blocker. No native Darwin result was reported in that update.

## Evidence and prior boundaries

The original native evidence pin resolves to
`58fbb7d4f6f7bf22b515c80e8913f2777ac72028`. Its README SHA-256 is
`a37605754c1d0d59393f50566079233a90d9c7867e864e9c06bfd9c7fb19a4c2`.
It remains authoritative for its failed 10/18 Core 3 and actual-global negative
fixture; no old failure was overwritten. Core 3 parent `215/690`, child `10/18`,
and spec-v3 expectations are unchanged by these commits.

The original Linux H2/H6 qualification and RR3 review were read as prior
contract evidence, not rerun wholesale while the implementer's full suite was
running. The review runner, raw logs, results, provenance and source-selection
JSON are all retained in this directory. Test durations are incidental
correctness output, not performance measurements.

Read-only sibling audit: local Slipstream
`dcb45b56386327a1f71fb039ed02e13562e1e2d7` has the corresponding physical
ownership shape: imported table/global regions are registered against their
physical descriptors, and owned table/global/element regions are installed
before initialized values become persistent. That supports the requirement to
root physical containers before exposure, but its raw region ABI is not proof
for Wago's opaque-word/receipt implementation. No Starshine checkout was present
at the expected local project paths for an additional live-tree audit; the
previous pinned Starshine boundary evidence was read and contains no analogous
native physical-container owner implementation. No sibling was modified and no
sibling defect is claimed.

## Coverage ceiling

Per-slice state:

* Darwin ARM64 host receipts/owner graph/sidecars: **static and Linux regression
  review closed; native Darwin execution open**.
* Unsupported adapter owner-global rejection: **closed for the tested same-owner
  shape; imported/access-instance shape open and blocking at immutable 3242**.
* Actual TinyGo: **open**; only the Go `wago_target_tinygo` adapter path ran.
* Linux/RV64 selection: **unchanged by source selection audit**; no new native
  execution claim is made here.
* Overall Darwin port approval: **not granted**.
