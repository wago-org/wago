# Experimental direct-offset GC bridge

This is **not a V8 performance result or the default collector**. The existing
handle-based Throughput and Tiny collectors remain the defaults. The experiment
runs a conventional moving collector end-to-end through checked Go helpers before
adding a new native object-layout ABI.

## Opt-in and boundary

Build with `wago_gc_helper_only`, compile the module in that process, and instantiate
through an explicit owned `Runtime` with `GCConfig.ExperimentalDirectOffsets=true`.
Fresh compilation issues a private helper-only receipt. Serialized artifacts do
not retain it. Standalone/private instances, imports, tables, exceptions, managed
external/function references, reference-valued exported arguments, custom
instructions and plugin collectors are rejected. Numeric arguments, reference
results, internal calls and owned mutable GC globals remain admitted. No benchmark
exports are stripped or modified to achieve admission.

The collector does not expose the handle-based `NativeView`. The compiler bridge
disables native object-layout operations, constructor proofs and loop reductions.
Go helper safepoints spill and reload exact mutable frame roots. Persistent roots,
initializer scratch and native global cells are rewritten after a successful
collection. Public tokens resolve their authoritative registered root rather than
caching an object's original address.

## Collector and costs

`experiments/directgc` uses one bounded pure-Go arena containing Eden and two old
semispaces. Minors tenure reachable nursery objects; full collections evacuate live
objects into the alternate old semispace. Oversized objects allocate directly in
old space. Reference stores remember old parents. Preflight validates capacity and
references before mutating objects or roots, so an exhausted collection is atomic.
Offsets retain the low-bit i31/null encoding, and descriptors cover scalar, packed,
SIMD and reference fields. Native-compatible headers retain exact array lengths.

This first implementation is deliberately not a low-memory final design:
- Objects have a 16-byte physical stride alignment, including 24→32-byte padding.
- Two old semispaces reserve twice the configured old capacity.
- Mark/start/dirty maps and a bounded traversal queue consume additional memory.
  Header forwarding reduces a 9 MiB arena's fixed backing from about 18 to 13.57 MiB;
  this is allocated backing, not measured RSS.
- The native adapter snapshots mutable roots into Go slices at collections.
- The explicit config flag and backend pointer add 24 bytes to the 64-bit collector;
  the config itself grows 64→68 bytes. Native handle ABI layouts are unchanged.
- Allocation/collection/live-object counters are implemented; existing detailed
  promotion/pause metrics are not yet populated for this backend.

`ForcePromote`, handle generations and the handle collector's `Verify` entry point
are intentionally unsupported. Collection reentry and mutation during borrowed
payload access are prohibited by the serialized adapter contract. This is not yet
a complete replacement for all WasmGC, host, cross-instance or platform modes.

## Current validation

- Direct collector graph/random oracle, cycles, sharing, duplicate roots, i31,
  packed/SIMD layouts, oversized allocation and transactional OOM tests, including
  race-enabled tests.
- Native adapter tests for constructor scratch, alias roots, persistent slots,
  old-to-young edges, type append, forced major and old-only OOM retry.
- Helper-only AMD64 execution of the unmodified retained struct/array/fill/copy
  and independent tree artifacts with exact checksums and real collections.
- Allocation-at-every-constructor tests for structs, uniform/fixed reference arrays
  and caller operands surviving an internal allocating call.

The helper-only qualification exclusions are listed separately in
[`../gc-helper-only`](../gc-helper-only/README.md). Passing this admitted subset is
not a claim that every existing native-optimization assertion applies to this mode
or that the full public Wasm feature space is supported. No new timing claim is
made here; the previous default collector still misses the requested V8 target.

## Native read extension

The separate `wago_gc_direct_reads` build tag keeps allocating and mutating
operations on rooted checked helpers, but emits checked direct-offset scalar
loads for final structs and supported array-length sites. It issues a distinct
versioned receipt; these artifacts are rejected with handle/Tiny collectors.
Native read views are typed separately from handle views. Every read validates
null/tag/alignment, active allocation range, object-start membership, extent and
exact type; no resolved pointer survives a helper call. The instance retains the
view and collector, and every collection/allocation refreshes mutable bounds.
The optional instance-view pointer adds 8 bytes per instance. Collector views are
allocated lazily, with another pointer included in the 24-byte collector delta
above. Scalar relocation, malformed backed metadata, and trap-PC regressions are
required before promotion beyond this experimental branch. No native-read speedup
is claimed without controlled measurement.

## Native allocation and reference-store extensions

`wago_gc_direct_alloc` includes direct reads and adds checked nursery bump
allocation for pointer-free numeric structs of at most eight fields and 128 bytes.
The allocator validates capacity and its capability before any mutation, initializes
header/payload/padding, then publishes the start bit, actual collector bump and
allocation counter without an intervening safepoint. All unsupported shapes,
exhaustion and forced-collection configurations retain the rooted helper path.

`wago_gc_direct_refstore` additionally handles final arrays of nullable final
struct references. Parent, child, index, type and extent checks precede the store.
Young parents need no remembered entry. An already dirty old parent is a read-only
certificate that the bounded remembered queue or its overflow fallback covers it;
a clean old parent falls back to the complete helper before mutation. Native code
never sets a dirty byte without registering the parent. ABI version 3 uses a
separate 96-byte optional collector view, not the handle ABI.

The collector now scans a bounded deduplicated remembered-parent queue during
minor collection, falling back to the full old region on overflow. Its additional
backing is at most 4 KiB. Failed collections preserve queue state.

Matched exploratory measurements and exact allocation/collection assertions are
in [`bench/experiments/gc-v8/direct-bridge`](../../../bench/experiments/gc-v8/direct-bridge/).
Queue-based scanning and scalar native allocation substantially improve the
helper-heavy prototype, but these are not comparisons with the fully native
handle backend or V8. The direct reference-store extension still needs its own
matched timing. No V8 parity claim is made.
