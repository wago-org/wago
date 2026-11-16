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
or guest heaps. The AMD64 compiler function state is 2,840 bytes versus 2,824 bytes with the
lease field removed (same-field reflection layout comparison); the lease itself
is 16 bytes, including a compile-time remaining-access counter in former padding.
There are no new per-loop heap allocations or maps in the source. Compilation
allocation-cost and execution measurements remain pending. No performance
improvement is claimed here.


Qualification uses register-allocation checking and code-generation telemetry.
Telemetry requires one emitted hoist and reuse, so a silent admission rejection
cannot masquerade as a successful optimization test. The prelude uses the standard
current-local getter, including dirty pinned-local recovery, rather than assuming
a canonical frame slot is current. A dedicated fixture reassigns the receiver
immediately before the loop and reads it after a subsequent moving collection.

Machine canonicalization can erase receiver provenance. An unknown provenance
is accepted only under the bounded bytecode proof, which verifies every array
access uses the same local/type. Known mismatches still fail closed. The lease
counts compiled accesses, rejects access outside its PC range or beyond its
budget, and requires the count be exhausted at exit. This does not remove any
runtime null or index check.
