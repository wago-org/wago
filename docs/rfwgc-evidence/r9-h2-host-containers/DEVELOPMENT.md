# Slice A development provenance

Starting source `67ef1f39e61dd61ee891bdefa5f8218a74aba806`, production
`501e9ab46c4f68090c7bbeb17a6ad14f4fadda6d`. Parent authorized the exact A
allowlist plus `src/wago/function_environment_linux.go` for collector-local
host receipt slot metadata and conditional root filtering. B signatures and
helper ABI are unchanged.

Precommit tests use immutable git archives of the starting pin plus only A-owned
source/test overlays. Each export records `owned-overlay.json` with SHA256 of
all overlaid files. No B dirty source or tests enter these results. Initial
ordinary existing receipt/store-hook scope and six new host test families pass.
`scoped-precommit` applies to the earlier overlay, before canonical-directory
root revalidation and extra immutable/ordinary-global controls. Final candidate
qualification is recorded separately; these earlier passes do not qualify later
changes. No failed ownership probes have occurred as of this entry.

All host containers retain their existing explicit Close contract: Close refuses
live importer/native leases. Clearing a slot removes its external converted edge
without closing the container. A successful Close consumes all receipts before
storage unmap, then drains after releasing every topology/domain/native lease.
No automatic Go finalizer or new relocation allocator is introduced.

`final-overlay` is the later canonical-root/ordinary/immutable-control snapshot,
with all3 normal/race passing; `access-gate-overlay` adds the final importer
beginInvocation guard and passes all3 normal scope. Its 3989 source blobs match
production/test commit `4bf395915fa2532b99bfec8f3a8d565cb6e4b217` exactly.
`source-pin.json` records that proof. Final committed-pin driver/per-cell results
are in the four `*-4bf395915` directories. All 107 final test/build/driver commands
pass (18 integrated + 14 target + 3 TinyGo + 72 scoped); no failed/interrupted
attempt was discarded. Zero-match and required-test skips fail the census checks.

Parent and B committed later changes while these runs were in flight. Those
changes were never copied into A exports and no source pin was silently updated.
All qualification statements in this directory apply only to the named A pin.
