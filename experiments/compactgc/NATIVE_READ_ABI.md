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

## Native runtime qualification

The opt-in compact-read build retains a separately typed per-instance view and
accepts only its private native receipt (or the separate helper receipt), never
the direct native ABI. Default Instance state is empty; compact-read mode adds
one owning pointer. Both job basedata and captured instance context use that
same view. Runtime tests alternate two instances sharing a collector through
major collections and preserve independent globals and context pointers.

Go1.27.1, compact_reads + regalloccheck: applicable compact GC/native/private
receipt tests passed3.291s, including exact five fixtures, eight scalar bit
representations, typed array length, moving initializer/public/clone roots,
malformed bounds/type/start/version metadata, null trap PC and a no-helper
native-read sentinel. Backend normal/mixed-direct-tag receipt tests passed
0.004s/0.003s. Final diagnostic instruction/telemetry test passed0.011s after
supplying the same staged type layouts as the production frontend.

Initial fixture failures were corrected without weakening production checks:
same-Runtime clone requests are intentionally rejected (typed Value publication
is used instead); broad TestCompact matched unrelated ordinary-config import
tests, so the selected GC suite is explicitly TestCompactGC, TestCompactNative
and TestGCCompact; the standalone disassembly fixture initially omitted frontend
type metadata and therefore correctly fell back to a helper. These results are
not a full compact conformance or performance qualification.
