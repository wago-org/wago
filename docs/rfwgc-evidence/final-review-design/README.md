# Final four-reviewer fan: design and cross-layer review

Reviewer scope: overall design and judgment across the complete combined port.
That covers boundedness, the lifetime and conditional-root model, target
exclusions, complexity and failure atomicity, the seven-topic rollup, full-port
parity, and cross-layer defects that the sliced reviews could miss.

- Production pin reviewed: `5345bd365c13963f1bf60e26bb80789ab9ead4cc` (immutable `git archive`).
- Evidence head read: `2c9f72744dd6f414fc8f236c2d45aa39ce6ced69`.
- Baseline: `9713878ab452848f7eb7549a90be94b938a898be` (immutable `git archive`).
- The dirty tree was not used. Its existing dirt, index and submodules were not touched.

This report is advisory input to the parent. It does not approve anything, and
it does not decide inclusion or PR readiness. The 195 passing gates in
`final-combined-5345` were taken as given. They were not treated as proof of
the design.

## Verdict summary

| ID | Class | Introduced or baseline | Status |
| --- | --- | --- | --- |
| DR-1 | Concurrency/isolation regression | Introduced by `1fb8f9423` | VERIFIED. Executed in all three modes. Baseline control passes. |
| DR-2 | Compatibility regression (import admission) | Introduced by `1fb8f9423` | VERIFIED. Executed in all three modes. Baseline control passes. |
| DR-3 | Target exclusion leaves H2-F1 baseline semantics without fail-closed rejection | Baseline retained, but not declared | VERIFIED in source only. Darwin and Windows execution NOT_RUN. |

DR-1 and DR-2 have the same root cause. One repair can fix both, but each
symptom needs its own regression test.

## DR-1: externref storage alone makes an instance lease every GC domain in its Runtime

**Source (pin 5345bd365):**
- `src/wago/gc_conversion_store_host.go:10-25`: `compiledConversionStoreHooks` returns true for *any* module with an externref global or table, including modules with no GC.
- `src/wago/reference_store.go:2126`: `registerInstance` sets `dynamicInvocationDomains` from that predicate. `:2285-2288` then sets `executionFlagImportedGCDomain|executionFlagDynamicGCDomain`.
- `src/wago/reference_store.go:1519-1526`: dynamic instances report the *whole* store topology as their invocation domain set.
- `src/wago/reference_store.go:1563-1576` and onward: `lockGCInvocationContext` read-locks the topology and acquires every domain's invocation gate for the full guest call.

Baseline `9713878ab:src/wago/reference_store.go:1894` sets dynamic only for
managed-function reachability or dynamic funcref reachability.

**Trigger.** Create one Runtime with three instances:
- A GC producer P, exporting a scalar `f`.
- An *unrelated* GC instance U, whose collector domain is distinct.
- A consumer C that imports `P.f` and merely declares `(global externref)` or `(table 1 externref)`.

**Observed behavior (`probes/design_review_probe_test.go`):**
- At the pin, C leases 2 domains, so U's domain is included. While C holds an invocation lease, `U.lockGCInvocationContext` blocks until a 2 s context deadline (`context deadline exceeded`).
- The control `none` case (no externref storage) leases 1 domain and does not block.
- At the baseline, all cases lease 1 domain and U's lease succeeds immediately.

**Consequence.** Every call into any module that declares externref storage now
serializes against *all* GC instances in the same Runtime. Externref tables and
globals are ubiquitous, for example in wasm-bindgen output and GC-language
toolchains. The lock is held for the whole call, including long-running guest
loops. Two consequences follow:
- Independent GC tenants lose concurrency, which used to be correct.
- Topology writers wait behind that call. This is the likely effect on instantiation and Close that take `topology.Lock()`; it was not separately executed.

This is a correctness-of-isolation and liveness regression, not just a
performance change. It must also be part of any measurement plan.

**Suggested repair.**
- Do not make "has externref storage" equivalent to "dynamic domain".
- Admission must follow actual conversion authority. Options:
  - Keep conversion-hook instances non-dynamic. Use the existing fast path in `publishConversionStore` (`!needed && owner != nil → publish()`) when the store has no conversion identities or containers.
  - When a conversion is possible, lease only the store's conversion-claimed domains plus the instance's own domain. `conversionAddClaimedDomainsLocked` already enumerates them.
  - Upgrade lazily, under the topology write lock, at the first identity or container creation.
- Add regression tests:
  - An unrelated GC instance lease or invocation must not block on a scalar invocation of an externref-storage module.
  - The DR-2 cross-Runtime import case.

## DR-2: producers with externref storage can no longer export functions outside their Runtime

**Source:** `src/wago/api.go:2236-2238` rejects an import whenever
`ex.inst.executionFlags & executionFlagDynamicGCDomain != 0 && ex.inst.refStore != store`.
After DR-1, every producer in a Runtime that declares externref storage carries
that flag, even a module with no GC.

**Trigger (`probes/design_review_cross_runtime_probe_test.go`):**
- P is a non-GC module with a scalar export `f` and one externref global or table. It is instantiated in Runtime R1.
- Consumer C imports `P.f` and is instantiated standalone, or in a second Runtime.

**Observed behavior.**
- At the pin, C's instantiation fails with `cross-instance import "p.f" from a dynamic funcref producer requires the same Runtime` for global/table × standalone/other-runtime (4 failures).
- The `none` control passes.
- At the baseline, all six cases pass.

**Consequence.** This is a public API compatibility regression, unrelated to
GC. Existing embedders that link plain reference-types modules across Runtimes,
or into standalone instances, now get an error. The error message also
misdescribes the cause.

**Suggested repair.** Same root fix as DR-1. In addition, restrict the
same-Runtime rule to producers that can actually transfer opaque converted or
managed references across that import. That means a signature containing
externref or anyref, or real managed-function reachability, not the presence of
storage.

## DR-3: the H2-F1 repair is Linux amd64/arm64 only; Darwin and Windows keep baseline semantics with no rejection

**Source:**
- The receipt adapters `gc_conversion_receipt_host.go:1`, `gc_conversion_store_host.go:1` and `gc_conversion_host_container_host.go` use the tag `linux && !riscv64 && !tinygo && !wago_target_tinygo`.
- `go list` with `GOOS=darwin GOARCH=arm64` and with `GOOS=windows GOARCH=amd64` selects these files instead: `gc_conversion_receipt_other.go`, `gc_conversion_store_other.go`, `gc_conversion_host_container_other.go` and `function_environment_other.go`.

On those targets:
- `gc_conversion_store_other.go:8-10`: the guest store helpers 53-58 still dispatch, through the target-independent `gc_conversion_store.go:15-72`, but they simply `publish()`.
- `gc_conversion_receipt_other.go:11-13`: local tables use the pre-fix `publishExternReplacement`.
- `releaseGCConversionOwner` and `closeGCConversionCoreLocked` are no-ops.

This is exactly the baseline chain that `r9-converted-provider/README.md`
reports for main: an imported-container store with `uses=0`, where publisher
Close drops the checked root while the provider survives.

**Reachability.** Darwin arm64 is a supported GC target. For example,
`gc_conversion_chain_test.go:1` runs `extern.convert_any` chains on
`darwin && arm64`, and the arm64 backend emits the same helper routing. All
H2/R9 regressions are tagged `linux && (amd64 || arm64)`, so no Darwin build
compiles them.

**Consequence.** On Darwin arm64, and on Windows where GC is supported, the
HIGH H2-F1 ownership loss remains. Nothing rejects it before publication. The
final README lists RV64 and TinyGo receipt exclusions but does not name
Darwin or Windows. A final "H2-F1 INCLUDED-FIX" would overstate the platform
scope. This is baseline behavior, not introduced, but the exclusion is
undeclared and fails open.

**Suggested repair**, either:
1. **Fail closed on targets without adapters.** Reject, by trap or helper failure, any publication of an opaque converted identity into a container whose physical owner is not the publisher, and any public receipt path. This is the "explicit admission rejection" alternative that the H2-F1 README allows.
2. **Port the adapters to Darwin.** This needs the managed-function graph currently in `function_environment_linux.go`.

In either case:
- Record a target-scoped disposition, such as PRESENT on Darwin/Windows or REJECTED.
- Add a darwin-arm64-tagged regression, even if it is build-only until a runner exists.
- Classify RV64's compact lane explicitly for H2-F1, with its own evidence, rather than through "build-excluded".

## Questions and coverage gaps (not asserted as defects)

- **Q1: region-mode complexity.** In `src/core/runtime/gc/native/alloc.go:141-146`, Eden region exhaustion runs `CollectFull` *before* `CollectMinor`. With an opt-in `NurseryRegionShift`, a large old heap makes each Eden fill cost O(live heap) marking, which can defeat generational behavior. This is intended per the config comment. Decide whether to add a bound or heuristic, for example full-first only under mature pressure or a recent-reclaim yield. The measurement plan needs a large-old-heap, high-allocation-rate workload in region mode.
- **Q2: stale contract comment.** `collector.go:95` says "Native allocation batches are disabled in this mode". Native struct allocation (`nativeAllocationAllowed`, `native_alloc.go:69`) is excluded, but `native_regions.go:5-36` implements region-native chunks. Reconcile the comment with actual behavior.
- **Q3: PR1020 parity.** Parallel tracing is disabled whenever an object-edge source is installed (`parallel.go:79`). Wago installs one for managed-function modules on Linux (`function_environment_linux.go:325`). For those instances, tracing is silently serial; only sweep and copy can be parallel. The rollup's "parallel collection retained" should state this.
- **Q4: externref store cost on every module.** amd64 and arm64 now route every externref `table.set/fill/copy/grow/init` and externref `global.set` through a Go helper (`amd64/table.go`, `globals.go`; `shared/extern_store.go:7-23`), including modules with no GC. Bulk helpers allocate an O(count) snapshot per operation (`gc_conversion_store.go:173`). Performance workloads must include non-GC externref-heavy modules. A guard that only takes the helper path when conversion is possible is a candidate optimization; it ties into the DR-1 repair.
- **Q5: new public API asymmetry.** `Table.SetValue` (`gc_conversion_store.go:236`) always errors on non-Linux, RV64 and TinyGo (`gc_conversion_store_other.go:27`). Document this or implement ordinary non-conversion semantics there.
- **Q6: threads and DR-1 (not executed).** A non-GC threaded module with externref storage is now dynamic. The atomic-wait rejection at `api.go:2163-2167` requires `needsRuntimeGCCollectorDomain`, so it does not cover this module. If `memory.atomic.wait` runs while all domain gates are held, a notifier that lives in another GC instance of the same Runtime could deadlock. Not reproduced; this needs a targeted check after the DR-1 repair.
- **Q7: poison surfacing.** An unexpected panic during mature evacuation or run promotion permanently poisons the collector (`mature_evacuation.go:342-350`, `:510-516`; `collector.go:379-382`). That is fail-closed by design. The Wago-level consequence, a permanently unusable instance or domain shared by several instances, should be documented, and tests should cover a shared domain.

Probed but not reproduced: I hypothesized that a consumer with externref
storage could be rejected for importing from a *private* GC domain
(`reference_store.go:2151-2152`). None of the alloc, convert or global
producer shapes produced a private registered domain, because conversion and
Runtime binding make it non-private. I record this as not established.

## Reviewed areas with no finding

- **Mature evacuation** (`mature_evacuation.go`):
  - Selection is bounded by `evacuationLimit`.
  - Planning is pure.
  - The checkpoint clones only allocator metadata.
  - `growBacking` copies into a fresh Go slice, so restoring the old header is valid.
  - The commit barrier comes after every fallible step.
  - Cards are keyed by handle and payload, so they are relocation-safe.
- **Eden region reset** after minor and full collection, and region chunk cancel ownership.
- **Mark high-water reset** across failed parallel phases (`mark.go:6-35`, `parallel.go:121-137`).
- **Staged active-segment initialization into imported extern tables.** Ordering and receipt staging (`instantiate.go:1711-1745`, `:2272-2278`) hold, and earlier segments keep their effects.
- **Lifetime consumer replay** (`amd64/gc_lifetime_consumer.go`). Parameter-provenance forwarding is safe given the analysis's parameter-write rejection, and allocation effects are preserved.

## Coverage

Units are files, and counts are kept separate. 237 non-doc files changed
between `9713878ab` and `5345bd365`.

Read in depth (about 25 files or hunks):
- collector: `collect.go`, `collector.go`, `config.go`, `eden_regions.go`, `native_regions.go`, `mature_evacuation.go`, `mature_regions.go` (header), `mark.go`, `parallel.go` (lines 1-140), `cards.go` (head), `host_view.go`, `alloc.go` (lines 100-200), `throughput_alloc.go` (`growBacking`)
- wago: `gc_conversion_receipt_host.go` (all), `gc_conversion_store.go` (all), `gc_conversion_store_host.go` (partial), all `*_other.go` stubs, `reference_store.go` (registration, domain view, lease), `api.go` (import validation), `instantiate.go` (staged init)
- compiler: `shared/extern_store.go`, `amd64/table.go` diff, `amd64/gc_lifetime_consumer.go`

**Not reviewed** (unreviewed items, so no coverage percentage is claimed):
- `tiny_collect.go`/`tiny_alloc.go` changes, `barrier.go`, `roots.go`, `object_*_linux.go`, `reclaim_linux.go`
- `function_environment_*` (H6 owner, retry and wakeup) beyond the stubs
- `gc_conversion_host_container_host.go`
- the RV64 compact lanes
- frame-root and EH root-map changes for PR997/1026/1027
- the arm64 backend beyond table routing, frontend `gcdesc.go`, and the TinyGo foreign-stack template
- per-file evidence logs, beyond the README files of `final-combined-5345`, `r9-converted-provider`, `gc-rollup-assessment.md`, the port log and the H2 docs

## Sibling cross-check (read-only)

- **Slipstream** `084be3d4bf70fb5337085ceddfab1bfa9db8b1c9`, `src/jit.goose:291-303`: imported-container root registration is gated on the *importer* having a native heap (`rt.native_heap != 0`) and a nonzero root kind. Registration happens per region at import time. It does not serialize the store, and it does not change the cross-store import policy. So DR-1 and DR-2 are Wago-specific, and Slipstream's narrower gating supports the suggested repair. Slipstream is Linux x86-64 only (`gc_linux_x86_64.S`), so DR-3 has no analogue there.
- **Starshine** `1d6c0af47140299d8f3c01ed03060d279c43ee03` (object present in `starshine-master-base`), `docs/wiki/wasm-gc-core-boundary.md`: this is a model, codec, validator and generator boundary with no native runtime collector or store. All three findings are not applicable.
- No sibling defect is asserted.

## Commands and skips

Work was done in immutable archives only:
- `/tmp/fr-design` came from `git archive 5345bd365 -- . ':!docs'`. `tests/conformance` was added from the same pin.
- `/tmp/fr-base` came from `git archive 9713878ab -- . ':!docs'`.
- Probe sources were copied into each archive's `src/wago/`.
- Toolchain: `go1.27.3 linux/amd64`. See `logs/toolchain.txt` for probe hashes.

```sh
for d in fr-design fr-base; do for tags in "" wagodebug wago_regalloccheck; do
  (cd /tmp/$d && GOFLAGS= go test -count=1 ${tags:+-tags=$tags} -run 'TestDesignReview' -v ./src/wago)
done; done
```

Results:
- `fr-design`: exit 1 in ordinary, wagodebug and wago_regalloccheck. There are 12 `--- FAIL` lines each: 2 parent tests, 6 lease-blocking subtests and 4 cross-Runtime subtests.
- `fr-base`: exit 0 in all three modes.
- Raw logs are in `logs/`.

Each blocking subtest waits on a 2 s context deadline. That is a correctness
bound for "blocked versus not blocked", not a timing measurement. No
benchmarks were run.

Skips:
- Darwin and Windows execution (no runner), so DR-3 is source-only.
- ARM64 and RV64 QEMU for the probes.
- Full suites, which are already covered by `final-combined-5345`.
- No known ownership failure was executed past.

Scratch archives are outside the repository. No production edits, pushes,
PRs, subagents or closures were made. The commit contains only this directory.

## Archive packaging correction

The archived probes now reside in `_probes/` so Go package discovery does not
compile them outside src/wago. Their bytes are unchanged; SHA256SUMS updates
only their relative paths. Historical logs and source references retain their
original names. Durable copies in src/wago remain executable regression tests.
This packaging repair does not alter runtime behavior.
