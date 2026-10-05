# RV64 private table and indirect-call checkpoint

The production compiler and experimental public binding execute numeric
`call_indirect` through one instance-owned, nullable funcref table32. The table
starts null and accepts active legacy function-index segments with literal I32
offsets. Capacity and aggregate initializer entries are bounded at 1024, with
at most 256 active segments. Table imports, exports, mutation, guest reference
values, passive/typed elements and foreign calls remain gated.

RV64 reads the existing eight-byte table header and 32-byte entries. It
zero-normalizes the index, checks bounds, then null code, then the complete
structural signature key. Bounds/null traps use authoritative code5; wrong
signature uses code6. Distinct backend tags avoid confusing these with the
existing private memory-bounds tag. Cold traps retain the call instruction's
exact source PC and unwind through the established production bridge.

The target must have the exact LocalWrapper-tagged home and a nonzero canonical
descriptor whose context equals the current instance's descriptor-zero context.
Memory home equality alone is insufficient when two consumers share imported
backing. Every dynamic guard precedes argument arena writes, including unused
call-site signatures larger than all defined functions. X28 retains the target
across guard veneers, large frame-offset materialization and transport reloads.
The call uses JALR/RA and the existing 48-byte caller header and canonical slots.
Indirect sites count toward frame/slot budgets even in dead code, and never enter
the direct-call relocation list.

Normal instance construction owns table entries, canonical descriptors and
context in its arena, with existing code/resource roots. Imported-memory
construction uses the established lease. Artifact admission requires exact
table-export metadata and zero exports, preventing legacy ExportedTable fallback
from exposing mutation. Register ABI must be disabled so equal framed
wrapper/internal offsets still publish valid LocalWrapper descriptors. Persisted
and effective capacities are checked before narrowing, declared maxima must
cover minima, and scalar signature keys are recomputed from metadata. Source,
decoded and artifact shapes enforce the same bounded private stage.

Focused tests cover duplicate equivalent types, I32/F32 and I64/F64 signature
mismatches, dirty index/argument upper bits, null/OOB priority, exact trap PCs,
targets used only through elements, mixed 256-slot raw FP/integer transport and
zero warm allocations. An unused 256-argument type has only one allocated
argument/result slot; all failing dispatch guards preserve adjacent results.
Indirect recursion tests stack overflow and reuse. Shared-memory consumers keep
private table contexts; fault controls reject another consumer's canonical/whole
entry, corrupted tags and null canonical handles before callee result writes.
Wrong-signature trapping precedes bad ownership. Loaded artifacts survive
Compiled.Close/GC and release final owners; invalid nonempty and empty element
offsets fail construction and detach borrowed memory.

Thirty-seven artifact controls reject unsupported shape/type/limit/key/element
metadata. Host-independent source tests cover aggregate boundaries, dead-site
frame budgets, unsupported dead reference instructions, table exports and early
decoded rejection before staging. Active indirect cancellation observes a real
callee memory write, runs with one Go P, waits foreign completion, verifies
subsequent reuse and checks Close releases descriptors.

Both independent source reviewers accepted the slice. Qualification evidence is
`evidence/table-*.log`: host core tests, full compiler and focused public tests
under baseline RV64 QEMU in ordinary/checked builds, AMD64 shared reference/race,
ARM64 compilation, vet/static/build, fingerprints/DCE and documentation. Native
hardware/cache/performance qualification and broad official suites are unrun.
The first AMD64 reference configuration forced register ABI off and exposed an
existing monomorphic indirect-path failure; its failed log is retained. Reference
qualification uses AMD64's normal supported configuration. RV64 remains framed.

This is a staged private table boundary, with broader table/reference/foreign
dispatch work still pending in the [parity tracker](riscv64-parity-tracker.md).
PR #807 remains the requested publication dependency; shared graph/API work stays
with its owner, and no RV64 push/PR/remote CI is authorized.
