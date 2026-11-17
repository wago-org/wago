# Owned RW staging diagnostic

Linux/AMD64 tag `wago_experimental_owned_rw_staging`, startup selector
`WAGO_AMD64_OWNED_RW_STAGING=0|1` (default0). Invalid values reject only in tagged
supported builds. Only an explicitly resolved one-worker public compilation is
changed; parallel and unresolved worker policies retain heap staging.

Mode1 selects the existing backend NewCodeBuffer/CodeImage path. Compilation emits
into demand-paged RW storage, transfers exclusive ownership through Take into the
Compiled cache, and leaves RX sealing to first activation. It does not alter
managed scratch roots, native code, host calls, validation or runtime contracts.
Existing AppendTail/CommitTail detached-growth fallback, relocation/adapter
compaction, error cleanup and cache lock/instance lifetime rules remain in force.

Potential benefit: avoid zeroing a large heap staging capacity and copying it into
a separate mapping on activation. Costs include mapping syscalls/page faults,
reserved address space and retained mapping capacity. No measured gain or RSS
bound is claimed. No global buffer/scratch pool is introduced.

Focused qualification: TestOwnedRWStaging*, backend
TestLargeDeferredCapacityPreservesNativeCode, runtime CodeBuffer alias/growth/
Take/Close/fault tests, and public mapping cleanup/retry/concurrent lifecycle
checks. Default-mode existing tests require selector0. The new tests check exact
heap/mapped bytes and offsets, RW publication, no-copy activation, repeated reuse,
Close and rejection after close. Source-only checkpoint, not yet executed.
