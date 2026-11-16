# Experimental policy 6: direct numeric array loop leases

`wago_gc_direct_trusted_loop` separately attests one receiver's object address and
immutable array length across a bounded noncollecting loop. It implies policy 3
numeric accesses, but does not enable reference-struct allocation (policy 4),
allocation guard reduction (policy 5), or numeric-array allocation. Those tags
must be selected explicitly. The private receipt is policy 6; direct ABI version
is unchanged. Runtime owned-domain, private snapshot and typed ingress admission
remain required.

The entry prelude sets both cached registers to zero for null. For non-null it
loads the current arena, computes the object address and loads Aux. Trusted
live-reference/layout invariants make this speculative read nontrapping. Each
original access still checks null, then unsigned index against the cached length,
then performs zero-extended 64-bit index scaling and the actual numeric access.
No trap or write is hoisted. An enclosing zero-iteration guard skips the prelude.

The scanner retains the existing fixed 256-byte / 128-op / 16-attempt budgets.
Only a straight-line integer body with final scalar array reads/writes, one
invariant direct-local receiver and terminal `br_if 0; end` is accepted. Calls,
allocations, nested/early control, division, unknown instructions, interruptible
execution, custom lowering and receiver assignments reject admission. This
initial subset excludes floating-point loop bodies and multiple receivers.
Unsupported loops use normal per-access lowering.

Two otherwise free registers are reserved without forced spills. A separate
16-byte compiler lease tracks local/type/end and the two registers; it is not a
root and never survives a safepoint. Stack canonicalization preserves the pins.
The original handle cache is not reused. Unexpected effects, receiver replacement
or mutation fail closed rather than retaining an invalid backedge address.

This adds fixed state to the compiler function object, not to runtime instances
or guest heaps. Actual compiler object-size, allocation-cost and execution
measurements remain pending. The implementation and tests must be compiled and
qualified before publication. No performance improvement is claimed here.
