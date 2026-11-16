# Attested allocation guard experiment (policy 5)

`wago_gc_direct_trusted_alloc` selects private policy 5, with the existing direct
ABI. It includes scalar native array allocation and policies 1–3 access paths.
It does **not** enable reference-struct policy 4 implicitly: combine the separate
reference-struct tag explicitly when evaluating that pipeline. No allocation is
elided, and no shape or initialization budget is widened by policy 5.

## Inductive runtime contract

Fresh validated compilation, immutable owned type maps/layouts, owned Runtime
collector admission and serialized execution certify the view/arena/start-table
addresses. Every Go/native allocation, collection, error refresh and Close must
maintain the direct-view contract. The actual nursery bump is nonzero, aligned,
inside the nursery and at an unused object start. Closed/disabled allocation
capabilities cannot supply pointers for native publication.

Under this contract, policy 5 omits repeated instance/collector ABI and size
checks, immutable type-map bounds/null checks, admitted backing-pointer checks,
bump-start/alignment checks, arena/end consistency and unused-start checks. It
still loads the canonical descriptor identity from the owned map; local type IDs
are not substituted for canonical IDs.

The following checks remain on every native allocation:

- AllocationEnabled must equal 1 **before dereferencing capability-gated pointers**.
- Load the authoritative current bump and require equality with its read snapshot.
- Require bump <= end and sufficient remaining bytes for the aligned stride.
- For dynamic numeric arrays, use 64-bit length/size/stride arithmetic and retain
  the bounded native initialization cap and interruptible-mode fallback.
- Reject the native counter increment at MaxUint64; the existing rooted Go helper
  determines the fallback accounting behavior, which is not changed here.
- Preserve all existing eligibility, stress/CollectEveryAlloc and reference-layout
  conditions, and all original canonical initializer slots on fallback.

Header, payload and padding initialization remains complete. Publication still
writes start membership, authoritative/snapshot bumps and allocation count as one
serialized no-safepoint sequence. No failure branch follows the first mutation.
Snapshot equality is deliberately retained until all newer allocator/helper/error
transitions have a separate exhaustive audit.

## Qualification boundary

Checked allocation is unchanged without the separate tag. Its private-pointer and
start-table corruption tests remain checked-mode tests; those mutations violate
policy 5's admitted ownership invariant. Dynamic enabled/capacity/snapshot tests
remain in both modes. Dedicated policy 5 cases disable the capability while its
gated pointers are null, and exercise insufficient capacity and counter fallback.
Normal native sentinel, exact accounting, numeric bit/padding, mixed Go/native
collection and rooted initializer tests remain required.

No speedup, lower compilation cost or V8 parity follows merely from removing
redundant guards. Measure matching artifacts, heap policy, allocation counts and
real collections before promotion.
