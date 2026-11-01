# R9 H2-F1 converted managed values in pre-collector providers

2026-11-01; bounded Linux/amd64 ownership-state qualification, Go 1.27.3.

**Repair required on both immutable pins. Do not recommend ABSENT.** A managed
struct converted by the real `extern.convert_any` instruction is admitted into
an older module-owned externref table/global. Its checked collector root exists
before retirement but belongs to the publishing instance's conversion state.
Closing that publisher closes and empties the conversion state while the
provider remains registered. The harness stops there.
This is a missing persistent conversion-owner obligation, not a crash claim.
H2-F1 remains an in-scope repair gate under the owner's full-port directive.

## Inputs and results

| Source | Immutable commit | Default: global/table, absent/harmless type section | Requested region shift 6 |
| --- | --- | --- | --- |
| main | `90f2b57dafa7f8c3cc7e3aca38b91148f15e1d1e` | Four ownership assertions fail in each of three modes | Four explicit admission skips per mode: field unavailable |
| updated port | `45c83820fbc10ee31eaaa8437e12464955b2fb9d` | Four ownership assertions fail in each of three modes | Four explicit admission skips per mode: collector configuration rejected before publication |

Modes: ordinary, `wagodebug`, `wago_regalloccheck`. Six Go commands return 1;
these are expected failing audit assertions, **not green validation**. There
are 24 admitted failing cases and 24 admission skips. `results.json` records
commands, exact source pins and probe SHA-256; the six `.log` files are raw
output. No timing or benchmark is taken; Go's incidental test durations are not
performance evidence. This opt-in evidence/testdata change cannot affect shipped
runtime performance.

Only Git archives of the named commits were executed. The shared checkout's
concurrent native, rooting, lifetime and foreign-stack edits were excluded.
Earlier bounded evidence `d7e6f5dfc1d04fee003a2492b515482d9453300c` was read for
context, not mistaken for a production fix. No shared parent log was edited.

## What the state probe establishes

The provider is instantiated first and exports only one mutable externref global
or one one-element externref table, initially null. The optional type section
contains one unused `() -> ()` function type. It has no functions, allocator,
or collector. Both provider and publisher are ordinary live members of one
reference store before execution. The publisher imports that exact container,
allocates one scalar-only struct, executes `extern.convert_any`, and stores it.
A null-only check of that imported slot in the same live invocation confirms
non-null publication; no object payload is read and no converted result token
crosses into the host.

Before retirement, the probe checks ordinary Go conversion metadata and the
collector's checked root API:

- Exactly one `gcExternConversionData` entry with `ref.IsObj()` and `hasRoot`.
- The state names the expected store and collector; `CheckedTableSlot(rootSlot)`
  equals that entry's `ref`.
- The provider still has no collector; the publisher collector is private
  (`storeOwned=false`). Domain persistent-root enumeration reports zero slots.
  Zero alone is **not** the failure: the conversion's checked root is present.
- Public GC/extern token storage is empty before publication; this function has
  only an i32 null-check result, so no host-token lease substitutes for ownership.
- No minor/full collection occurs during the bounded one-object invocation.
- The conversion entry has `uses=0` in these imported-container paths; there is
  no transferred per-container receipt recorded by `replaceExtern`.

After ordinary publisher `Close`, only Go ownership metadata is inspected:
`conversionClosed=true`, `entries=0`, `attached=false`, `publisherLive=false`,
`providerLive=true`. No further collector API, container read, clear, teardown,
collection, escaped call, or arbitrary/freed memory inspection follows. Stopped
stores stay Go-reachable until the process exits. The probe intentionally cannot
reach an eventual-release test: the prerequisite retained lease already failed.
It also fails closed if future source changes retain the state, requiring a
provider-backed identity and clear/release oracle before a pass is possible.

This proves loss of the conversion ownership receipt after successful non-null
publication. It does not claim a post-close payload read, exact stale bytes,
last code mapping unmap, collector crash, or behavior on unexecuted platforms.
Ordinary host-token success and compact-anyref pre-publication rejection from
the earlier evidence remain distinct controls, not absence proofs here.

## Concrete source chain

Paths/line numbers below are for the updated port archive, not moving HEAD.
The conversion files and relevant teardown implementation are unchanged from
main; `reference_store.go` has separate frame-root integration changes, which do
not supply a conversion-owner transfer.

1. `src/wago/gc_struct_helper.go:439-465` dispatches `gcExternConvertAny` to
   `state.Conversion.externFromAny`. `gc_extern_conversion.go:114-177` validates
   the compact reference, creates an internal opaque extern word and distinct
   public identities, and allocates `NewCheckedTableSlot(ref)` for an object.
   `gcExternConversionEntry` at lines 17-27 holds `ref`, `rootSlot`, `uses`,
   `hasRoot`; it is the actual receipt in this host representation.
2. `instantiate.go:519-535,2101-2105,2213-2218` creates the conversion state with
   the publisher collector and attaches it to that instance's table-state
   holder, including a holder for modules without local tables.
   `gc_ref_test_table.go:144-159` describes local-table classification;
   `setTable:211-219` uses `replaceExtern` for the mirrored extern-table path.
   `gc_extern_conversion.go:304-345` increments/decrements those uses and clears
   a checked root when the last recorded slot is overwritten. The admitted
   imported cases above do not acquire that receipt (`uses=0`).
3. `reference_store.go:1335-1358` defines public rooted extern token receipts
   and direct domain receipts. These are **different representations**, not a
   guarantee that an opaque conversion word stored in a module container owns
   one. `gc_compact_extern_riscv64.go` and
   `gc_compact_extern_host_token_riscv64.go` implement those compact-token paths;
   they do not execute in this Linux/amd64 probe.
4. `reference_store.go:1697` (`rangeGCDomainPersistentRoots`) filters instances
   by exact collector identity, and globals by compact GC-reference type.
   `rangeLocalGCTableRoots:879` also filters representation-aware roots. The
   pre-collector provider's opaque extern word is not a compact ref to scan.
   `native/roots.go:698-744` provides checked collector table slots;
   `native/mark.go:83,134` enumerates those slots as collector roots. Thus the
   entry's root is real, but its lifetime is the decisive missing property.
5. `instance_lifecycle.go:648-689` detaches imports and drops publisher table
   state, then closes a private collector. `gc_ref_test_table.go:272-296` calls
   `Conversion.close`; `gc_extern_conversion.go:405-430` nulls the root slots,
   zeros entries, and drops store/collector pointers. It has no surviving-provider
   receipt check. The observed metadata follows this source chain.
6. `instantiate.go:287-315` forces `DisableCollection` for products outside its
   validated native root-map slice. Port `native/config.go:21-29` rejects region
   Eden with that flag. The supplied shift 6, fixed 1 MiB nursery is otherwise
   valid. These region cases are unsupported admission, not actual-region
   execution and not a retention pass. The probe has an actual collector config
   assertion if a future pin admits region execution; it does not bypass the
   production gate.

## Repair requirement and deterministic acceptance gates

Bind the opaque converted identity and its checked root/domain lifetime to the
surviving module-owned container before publishing the word. Retire the
publisher without invalidating that identity or its collector. Clearing or
replacing the last containing slot must drop the matching receipt exactly once.
Merely registering the nil-collector provider, scanning an opaque word as a
compact ref, retaining a host token, or retaining the publisher indefinitely is
not a sufficient repair. An explicit admission rejection before publication
could be a bounded safety alternative, but is not implementation of the requested
supported path. Production ownership stays with the assigned lifetime worker.

Required qualification after repair:

1. Keep both container kinds and both type-section shapes; verify the provider
   precedes collector creation and the actual `extern.convert_any` entry is used.
2. Prove exact store/domain/receipt membership and enumerated root coverage before
   retirement; prove the same container-backed receipt remains live afterward,
   **before** permitting any collection or converted access. Stop on failure.
3. Only after retention succeeds, clear through the supported container API and
   assert receipt removal, checked-root nulling and eventual domain/owner release.
   Include replacement and two-slot shared-identity balance controls so a leak or
   premature final release cannot satisfy the test.
4. Rerun immutable main/completed integration in all three modes. Actual region
   execution is required where supported; retain explicit skips if the product
   remains inadmissible. RV64 compact extern and ARM64 require their own supported
   execution gates; this report does not infer their outcomes.

## Read-only sibling cross-check

Read the actual Slipstream registration fix
`084be3d4bf70fb5337085ceddfab1bfa9db8b1c9` (merged by `1a4759e5c`),
`src/jit.goose:272-280,296-303,350-367`. It installs missing imported table/global
root regions using the descriptor as key and registers the runtime before the
first semantic section even when a type section is absent. This addresses both
late root-region installation and no-type registration; copying only the latter
would miss the ownership obligation. Wago instead registers instances during
construction and uses distinct opaque conversion receipts, so the Goose patch
is not a literal transplant.

Also read Slipstream's actual H6-F3 fix
`ef06c3710a47b99e69ba981b5b9370aa4167e3a0:src/jit_store.goose`: it conservatively
retains failed ABI2 runtimes when other live runtimes have managed root regions,
pending an exact nested trace. No source crash fixtures were read or executed.
This is source-wave context only; H6 production repair belongs to the other
worker and is not requalified here.

Starshine `1d6c0af47140299d8f3c01ed03060d279c43ee03`,
`docs/wiki/wasm-gc-core-boundary.md:39-65`, distinguishes model, codec, validation,
and authoring coverage. It provides no analogous native runtime conversion-root
lease contract. No Starshine runtime defect or runtime absence is inferred.

## Repeating the bounded gate

Extract `git archive <exact commit>` into this directory's ignored
`snapshots/main` and `snapshots/port-updated` (ordinary archives, not worktrees).
Run `python3 run.py`. The runner injects only the uniquely named testdata probe
and selects only `^TestR9ConvertedProviderState$`. It performs no benchmarks,
source fixture execution, branch changes, production edits, PR/push, or closure.
The final logs supersede preliminary compilation/probe iterations; only the
probe hash in `results.json` identifies this reported matrix.
