# Checked native scalar-array experiment

`wago_gc_direct_scalar_arrays` includes the checked direct reference-store,
allocation and scalar-read modes. It uses the existing separately admitted direct
ABI and does not authorize a handle collector. Default and Tiny lowering are
unchanged.

The new slice covers `array.get`, packed signed/unsigned gets and `array.set` on
final numeric arrays with 1, 2, 4 or 8-byte elements. SIMD, reference elements and
nonfinal types retain helpers. Before any load/store, the emitter validates the
parent with the checked direct resolver, then checks the unsigned logical index
against Aux and the selected element's 64-bit end against header Size. The resolver
also verifies header extent against active allocation and arena ranges. This is
selected-element physical protection, not complete Aux-declared payload validation.

The array operations remain fully checked even when combined with a trusted-read
or trusted-store tag. They retain no raw address across a helper or safepoint.
Numeric stores need no GC write barrier. Allocation, helper fallback, roots,
subtyping and runtime admission are unchanged.

Runtime tests prove native execution without a helper, packed sign extension,
integer/float bit preservation including NaN and negative zero, i32 index upper-bit
normalization, contents surviving major relocation, null instruction provenance,
logical bounds, and failed stores leaving contents unchanged after malformed
selected-element extents. Existing exact retained-workload and moving-root suites
are included in this mode's qualification.

No performance conclusion follows from implementation or passing tests. Compare
matched artifacts, policy, allocation counts and real collection before claiming a
gain. In particular, this does not implement native array allocation yet.

Integrated source `89013de` passed the combined scalar-array/trusted-store
`TestDirectGC*` and `TestTrustedDirect*` runtime suite with `wago_regalloccheck`.
The combined run excludes only `TestDirectGCReadValidation`, whose deliberate
private struct-metadata corruption is outside trusted struct reads; the new
scalar-array physical-extent checks still run. Scalar-only worker qualification
also passed that checked struct-validation test. Unfiltered default Wago,
native/checked GC, AMD64 backend and direct substrate tests all pass. These are
correctness checks, not measured array throughput or full experimental feature
conformance. Numeric array allocation still uses rooted Go helpers.
## Separately attested numeric access

`wago_gc_direct_trusted_arrays` selects private policy 3, including the existing
trusted scalar-read/reference-store modes and scalar-array operations. Policy 2
combined with `wago_gc_direct_scalar_arrays` continues to use checked numeric
array operations. The ABI version remains 3; the policy receipt is separate,
private, compiler-issued and nonserialized.

Only policy 3 removes repeated parent metadata resolution and the physical
selected-element check. A validated final array operand type, checked public
reference ingress, exact moving roots, and allocator-preserved header Aux/layout
establish a live object with a physically valid payload. Numeric host byte borrows
cannot change its header or introduce references. Parent null, unsigned logical
index bounds, zero extension and 64-bit scaling remain explicit. Numeric stores
still have no references requiring a write barrier. No pointer crosses a call.

Intentional private-header corruption tests remain in checked mode, separate from
null/bounds/relocation tests shared by both modes. This is an opt-in conventional
validated-Wasm access experiment, not allocation elision or a performance claim.
