# Checked compact native reads

Native ABI1 is a distinct Layout16-only capability, not DirectABI4 and not the
compact helper compiler receipt. It exposes a read-only56-byte collector view
and32-byte instance prefix plus an owned canonical-type slice. Native allocation
and dirty-map publication are not provided. Layout8 cannot reuse this contract.

The runtime must retain the typed instance view and collector; uintptr addresses
do not keep either alive. Collector backing is fixed. Its descriptor retains
identity and refreshes occupied ranges at every allocation/collection boundary,
including errors. Close clears addresses/ranges while retaining version/size.
All access is serialized with mutation; no object pointer survives a helper call.

Build selection embeds one extra compact-only pointer into Collector; default
state is empty. No added default collector pointer. Compact instance metadata is
56 bytes on64-bit hosts plus the canonical map, and the collector descriptor is
56 bytes; these are genuine experimental-mode costs.

Qualification Go1.27.1, source a1bca690:
- Default empty-state/footprint selection tests: pass0.006s.
- Compact helper-tag view lifecycle/map/error tests: pass0.002s.
- Compact reads-tag view lifecycle/map/error tests: pass0.002s.
- Full standalone compactgc package: pass0.016s.
These are ABI/core correctness tests, not native emitter or performance results.
