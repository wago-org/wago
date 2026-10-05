# RV64 imported memory32 checkpoint

The opt-in `wago_rv64_experimental` production/public compiler admits one ordinary
memory32 import, using genuine RV64 loads, stores, bulk operations and growth
against the imported JobMemory. The total memory count remains at most one.
Defined functions, literal scalar globals and data retain the current bounded
scalar policy. Functionless modules, function/global imports, starts, tables,
references, Wasm shared memories, memory64 and multiple memories remain gated.
Host sharing of a Memory object does not itself enable Wasm threads.

Source admission preserves the distinction between an absent import section and
a present unsupported empty section. A present section must contain exactly one
import; decoded admission requires its kind to be memory and its type to be
ordinary memory32. Hand-decoded modules and trusted architecture-3 artifacts
enforce the same resource boundary before image publication. Compilation retains
the shared deferred heap-image ownership contract. No other target supplies
fallback machine code, and no register-entry proof is advertised.

The shared runtime prerequisite serializes construction of an imported primary
JobMemory with the native execution lease. Shared collector construction acquires
the collector domain first, matching invocation lock order. Construction rechecks
the backing's current pages against the consumer quota after waiting, clears
per-instance pointer fields, and captures the complete new context before release.
Backing-owned size/max caches stay intact. Failed initialization restores prior
pointer fields and unlocks before arena cleanup or importer detachment; previously
applied active-data effects are retained. Invocation rebinding uses RebindTrapCell
so a queued interruption survives a foreign trap pointer.

Each consumer retains its own global cells, passive data/drop descriptors and
quota directory, although ordinary imported memory aliases the provider's
basedata. Generic entries restore the consumer context and invocation control.
Growth uses the backing's maximum and the active consumer's quota, with unchanged
query/overflow semantics and a stable mapping address. Import roots retain the
original producer after logical Close; re-export preserves its memory identity
and exact declared limits. The last borrower releases the producer mapping.
Host-owned exclusive memory rejects another live importer and rejects Close
until its borrower detaches.

Focused portable public regressions cover successful and failed construction
behind an execution lease, actual provider growth while a quota constructor
waits, absent-pointer cleanup, quota-to-no-quota alternation after the quota arena
closes, private global/data.drop state, source and artifact calls, producer Close,
re-export, final-root cleanup, host ownership and declared maximum matching.
Queued cancellation/Close wait for foreign completion and retain later provider
reuse. A deterministic prepublished interruption checks the case where the
watcher's bounded retries have already ended. RV64 entry polls reject that
interrupted entry before its first guest write. Warm ordinary imported-memory
calls allocate zero.

Independent encoder/admission and runtime/lifecycle reviews accepted the slice.
Qualification logs are `evidence/imported-memory-*.log` in the task workspace:
host encoder/compiler and full compiler QEMU tests, focused public QEMU tests in
ordinary and checked modes, AMD64 shared-compiler reference and race checks,
existing quota/table/exception/host-reentry/GC compatibility checks, ARM64 compile,
vet/static checks, diagnostics/DCE and documentation checks. These are focused
correctness results; full official suites and native cache/performance
qualification remain pending. The broader port remains tracked in the
[parity tracker](riscv64-parity-tracker.md).

The subsequent [scalar global import checkpoint](riscv64-imported-global-checkpoint.md)
adds four-type global imports and value-import re-export ownership. Its resource
import count supersedes the memory-only count policy above.
