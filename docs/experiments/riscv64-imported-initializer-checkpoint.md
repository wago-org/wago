# RV64 immutable imported-global initializer checkpoint

The production and opt-in public compiler admit exact `global.get index end`
initializers referring to immutable imported scalar globals. Defined I32, I64,
F32 and F64 globals may copy only an import of the same type. Active memory32
data offsets may use an immutable imported I32. The imported-global prefix is
frozen before local definitions; memory imports do not consume global indices.
References to defined globals, extended expressions, starts and unsupported
resource shapes remain gated.

Production backend placeholder zeros are typing metadata only. The public
frontend retains `HasInitGlobal` and `Offset.HasGlobal`; normal instantiation
reads attached cells and copies into separate local cells. Existing attachment
owners keep producer storage alive, including after logical producer Close.
Immutable scalar cells have no supported writer, so global-only construction
needs no additional execution lease. Combined imported-memory construction uses
the existing domain/native lease. Raw FP bits survive, and narrow carriers are
zero-normalized through the normal cell readers and writes.

Architecture-3 artifact admission applies the same import-only, immutable and
exact-type restrictions, rejects mixed initializer forms and imported
destinations, and requires exactly one ordinary memory for active data. This
also closes the previous literal-active-data/no-memory metadata gap. Generic
validation permits broader shapes; the RV64 policy enforces its own boundary
before publication.

Portable public regressions cover all four scalar types, dirty upper words and
raw signaling NaNs, separate mutable derived cells, artifact round trips,
Compiled.Close/GC, retained producer ownership, memory imports first/middle/last,
zero warm call allocations, exact-end empty segments, last-byte writes, unsigned
negative offsets and failed attachment cleanup. Ordered active initialization
preserves the first segment's write when a later segment fails, restores a usable
producer context and releases memory/global roots. Seventeen direct artifact
controls reject negative/out-of-range/defined/mutable/wrong-type sources, mixed
forms and absent memory. Host-independent tests check exact expression parsing
and keep isolated GuestContext and earlier-defined-global references gated.

Both independent source reviewers accepted the slice. Qualification evidence is
`evidence/initializer-*.log` in the task workspace: focused AMD64 shared reference
and race checks, ordinary/checked host core tests, full compiler and focused
public tests under baseline RV64 QEMU, ARM64 compilation and vet/static/build/
diagnostic/documentation checks. QEMU establishes correctness only. Full-feature
official suites and native cache/performance qualification remain pending in the
[parity tracker](riscv64-parity-tracker.md).

PR #807 remains a publication dependency requested by Josh. Its observed draft
contract is optional checked graph instrumentation and does not block independent
local initializer work. RV64 target-side observer ordering, normalization/frame
transfer provenance and independent long-branch decoding need integration after
the shared contract is complete and merged. Shared graph/core/API work stays with
that PR's owner; no RV64 push, PR or remote CI is authorized.
