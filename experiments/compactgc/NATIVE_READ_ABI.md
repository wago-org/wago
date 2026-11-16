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

## Integrated qualification and diagnostic

At runtime source `b7b6c3b2`, unfiltered default tests passed: Wago 40.004 s,
native collector 2.082 s, checked wrapper 0.003 s, AMD64 backend 6.368 s,
direct core 0.023 s, and compact core 0.012 s. Full native `wagodebug` passed
(2.092 s), and TinyGo 0.42 host native/checked tests passed (2.469/0.003 s).
These supplement the checked compact-native admitted-mode/regalloc, malformed
metadata, relocation, shared-instance, receipt and instruction-selection tests.
They do not imply full Wasm feature admission in the compact runtime.

The separate same-source bounded diagnostic found clear struct/tree improvement,
but an array regression and overlapping other workloads. See
`bench/experiments/gc-v8/direct-bridge/matched-compact-reads/evidence/summary.md`.
Its three warmup plus five timed calls per process are explicitly not the full
V8 gate. Real-GC audits run separately; short copy samples may legitimately have
zero collections. Allocation, stores, bulk operations and many reads still use
helpers. No broad speedup or V8 parity is established.

## Checked reference-valued struct getters

Final containing structs may also load four-byte collector-reference fields.
Supported storage includes typed struct/array references and nullable heap
references/i31; opaque extern and managed function references remain excluded.
The same checked receiver type, start, active-region, complete stride and null
validation applies. Child values are not revalidated at load, matching
StructGetTyped and StructGetFinalRef helper semantics. They are zero-extended
into 64-bit value slots and explicitly marked as exact GC references.

Compact reads + regalloc qualification passed: new reference-root/null/i31/
malformed-receiver cases0.042s; complete applicable compact GC/native matrix
3.201s. A no-helper getter sentinel proves selection, and tests discard the
parent before allocating internal callees, including local/select/branch/call/
return flows. Constructors are unchanged in this qualification. No performance
claim is made.
