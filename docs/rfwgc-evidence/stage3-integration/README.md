# Stage 3 region Eden public-runtime integration qualification

This is bounded integration qualification of runtime commit
`5b31c81ec0c74695a90bc8d36754b92845b59f14`, not completion of Stage 3 or the
full Snap GC port. Mirror evidence is commit
`69a6d9ec5b09a2e8fb6086763e1c112f0719411b`. Work stayed in the existing
`experiment/rfwgc-port` worktree. Concurrent documentation/review commits advanced
the branch to `72e478f6c8c5067691277fd38cb681878f05bd8f` before the final runs;
this worker made no production changes. The commit containing these tests and
logs is named in the handoff rather than embedded self-referentially here.

Only `src/wago/gc_eden_region_integration_amd64_test.go` and this evidence directory
are owned. Existing dirty corpus scripts, port log and spec submodules, plus other
workers' measurement fixtures, were neither staged nor changed. No branch switch,
worktree creation, delegation, PR, push, benchmark or intentional timing occurred.

## Configuration and oracle

Eleven scenarios each run with `NurseryRegionShift=0` and `6`, otherwise identical:
1024-byte fixed Eden, 1024-byte survivor spaces, throughput profile,
CollectEveryAlloc, ForceMajorEveryMinor and VerifyAfterCollect. No stress-size
override, Tiny fallback, disabled collection, or changed admission rule is used.
Both configurations must match independently specified numeric results or exact
existing error contracts. This is a same-head policy oracle, not a pre-port
performance comparison.

Each scenario asserts positive allocation and full-collection counters. Enabled
cases also inspect the collector's private region directory with read-only Go
reflection: exactly 16 regions and a positive sum of their committed used extents.
Baseline cases require an empty directory. This proves actual region-backed
payload allocation, rather than merely passing an option or counting a scalar
charge. Reflection is test-only, with no unsafe pointer access or diagnostic API
added to production. The assertions run while instances remain open and before
an explicit full collection can reclaim their last transient object.

Existing binary fixture builders are reused. The metadata-growth fixture uses
wasmtest's existing section/type encoders. Shared-domain construction uses the
existing package-private store/instantiate fixture API; calls, globals/tables,
collection, close and token release exercise the runtime surfaces. Borrow exclusion
uses the existing reserved host-scope fixture rather than creating a real host
callback; this limit is deliberate and is not counted as callback-return coverage.

## Coverage added

| Test suffix | Observable qualification |
| --- | --- |
| ImmutableGlobal | 20 reads return 42 under collection; shared collector ownership; importer reads 42 after provider close. |
| MutableGlobal | Importer replaces the aliased root with value 77; both instances read it over 20 rounds; provider still reads 77 after importer close. |
| RetainedToken | A public struct token keeps its exact producer alive after close; another shared-domain instance reads 42 under 1000 allocating operations; wrong-producer release rejected; a second array token allocates and releases; original producer releases its retained token. |
| BorrowExclusion | Guest roundtrip returns 7; collection during WithGuestStorage returns ErrPermissionDenied without advancing full collections; collection succeeds after borrow ends. |
| IndirectTail | Existing tail-indirect fixture retains no caller callsite; 1000 allocations execute and return scalar 66. |
| Exceptions | Exact native root admission; direct caught payload, catch_ref held payload, and mixed catch_all_ref payload produce 42, 7 and 105 under collection. |
| ForeignToken | Two private domains allocate struct/array tokens; foreign token ingress is rejected as stale; both original owners can release their own tokens. |
| Reentry | Three guest/host levels retain different live outer values; deepest entry performs 1000 allocations while callers are suspended; success/trap/success sequence returns 91 on successful outer calls, propagates the named host trap, and leaves zero active host activations/reentry depths. |
| Tables | Two heterogeneous imported tables share a collector; mutation aliases are visible in both instances, growth preserves values 22/33, repeated replacement is collected, and reads survive provider close plus explicit collection. |
| TableRollback | Second table import from a foreign domain rejects with the existing same-Runtime-domain diagnostic; both importer counts roll back to zero; both providers subsequently allocate and read 59. |
| MetadataGrowth | After the provider publishes native views, 15 linked instances register distinct structs of widths 2–16; collector type count increases each time, canonical identity of the imported original type stays equal, and all old/new instances read 42 after the final growth. |

This closes concrete portions of matrix O/D/F/M, with supporting C/H checks.
It is not universal closure of any matrix row. Forced full collection was chosen
to keep all runtime-boundary tests bounded and deterministic; these tests do not
qualify mixed-generation boundary behavior or region-exhaustion policy by themselves.

## Validation

Linux amd64, Go `go1.27.1`. Exact final commands, run from the existing worktree:

```
GOMAXPROCS=4 go test -p=4 ./src/wago -run '^TestGCEdenRegionIntegration' -count=1 -v -timeout=5m
GOMAXPROCS=4 go test -p=4 -tags=wagodebug ./src/wago -run '^TestGCEdenRegionIntegration' -count=1 -v -timeout=5m
GOMAXPROCS=4 go test -p=4 -tags=wago_regalloccheck ./src/wago -run '^TestGCEdenRegionIntegration' -count=1 -v -timeout=5m
```

All three commands exited zero: 11 scenarios / 22 paired cases per mode, no skips.
Machine-readable counts are in results.json. Raw final logs are ordinary.txt,
wagodebug.txt and wago_regalloccheck.txt. Standard
Go test durations are incidental runner output, not performance measurements.
An initial harness assertion inspected occupancy after explicitly reclaiming its
only object; it was moved before collection. No production defect was reproduced
and no runtime fix or admission relaxation was made. Early setup attempts failed
because `python` was unavailable (used `python3`), then because the temporary
fixture-extraction script over-trimmed a function and an import was unused; these
were corrected before the recorded runs. A no-tests run during setup is not
qualification evidence.

## Read-only sibling cross-check

Slipstream local origin/master still resolves to
`506b08d3d3fa3c8e8b1ce9ac0cb4f30a2463cdc3`; no fetch or sibling edit was made.
Read `gc/native.goose` at that exact commit: `ng_collect_owned` refuses collection
while borrowed/poisoned, preserves the distinction between reservation and active
worker counts, and checks persistent roots before destructive work. `ng_region`
rejects overlapping root-slot ownership, `ng_region_owner_release` releases only
that owner's regions, and `ng_region_resize` prepares backing before publishing.
These are corresponding contracts, not a claim that Wago payload regions are
Slipstream root regions. Old-space source landing remains absent at this cursor;
parked type-cap code and tabled Wago PRs remain excluded.

Starshine origin/master is `e075c47e61b01ab9e4fd0ae4a7ca47b755e3fca1`.
Read its pinned `docs/wiki/wasm-gc-core-boundary.md`: model, binary, validator and
text-authoring support are separate layers, not a native runtime-region collector.
No sibling tests or performance runs were performed. With no verified runtime
finding, there is no defect-specific triage or minimal failing runtime repro.

## Uncovered rows and limits

- O/F: host callback GC result creation/transfer, full-depth overflow, concurrent
  callbacks, SIMD/spill/operand-stack pressure, GC-reference tail arguments and
  results, direct/call_ref/foreign-host tails, cancellation and close during reentry.
- D/C: third-party import chains, overlapping slot fault injection, table/global
  borrowed payload access, cross-domain globals specifically, and failed root/card
  admission after partially consumed dirty words.
- M: high canonical IDs with few layouts, thousands of managed layouts, subtype
  hierarchies under growth, registration allocation failure/rollback/retry, and
  stale/fresh canonicalization API behavior. Widths 2–16 qualify small successful
  runtime registration growth only; they do not qualify parked source C1/C2 limits.
- A/G/R/B/I/S: region metadata/backing failure, long promotion churn, old/large
  policy, initializer graphs and scalar coexistence are owned by other suites;
  no new closure claim is made here.
- N: native region allocation batches remain disabled. This suite exercises the
  checked allocation fallback with native execution/access, not region-aware refill.
- X/T/E/P/Q: cached artifacts, non-amd64 platforms, Tiny/disabled configuration
  matrix, full conformance, race/sanitizer, parallel collector work and the final
  four-reviewer gate were not run here. Other workers own external-gate status.

Tests and Markdown evidence cannot change shipped runtime performance: they are
excluded from production builds. No measurement of this test-only change is
needed. Independent measurement of runtime commit 5b31c81ec and the eventual full
port still requires the global lease; no performance conclusion is inferred here.
