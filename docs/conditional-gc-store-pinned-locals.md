# Conditional GC stores and pinned locals

The amd64 backend can pin integer and floating-point locals at the same time.
Their combined count can exceed 16. Conditional native struct and array reference
stores must preserve every pinned local across their helper fallback.

Small snapshots still use the inline 16-byte buffer. Wider snapshots use the
existing function-local state buffer pool and return the buffer after emission.
The pooled slice is a separate variable so Go escape analysis keeps the inline
buffer on the stack. The generated fast path and helper reload rules are unchanged.

`TestGCConditionalStorePreservesWidePinnedLocals` builds both store shapes with
19 live integer/float pins. Before the repair, both fail compilation at the
16-local bound. After the repair, both must return the stored value plus every
live parameter. Related GC tests cover old-object barriers and collection.
