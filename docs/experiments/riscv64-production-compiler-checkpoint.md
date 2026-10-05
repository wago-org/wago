# RV64 production compiler envelope checkpoint

`CompileProduction` emits genuine RV64G integer/control/direct-call modules
using the shared Engine/JobMemory wrapper ABI. The public Wago backend remains
disabled. Whole-module admission currently permits only integer function types,
defined functions, function exports, and inert name/producers metadata. Present
state-bearing sections are rejected before a native image exists, including
empty sections. Unexported functions must also be supported.

The envelope returns the shared CodeImage ownership interface and one bounded
allocation for Entry/InternalEntry tables. Function entries are 16-byte aligned
with fixed-width RV64 NOPs. Internal entries conservatively alias framed wrapper
entries; no prepared/light/bounded proof or optional ISA requirement is claimed.
Both RW staging and deferred heap staging transfer through the reviewed shared
publication path. Failed staging cleanup retains a retryable image owner.

Each function checks the complete aligned frame against the authoritative
basedata stack fence before its first stack write. Direct calls restore the
argument, result, home and trap transport registers and use LWU for i32 result
slots. Cold traps write the production trap code and function/PC source location,
then restore the bridge stack and continuation without unwinding each guest
frame. Source PCs include local-declaration bytes, matching existing targets.

The existing multivalue/indexed-control fixtures now also execute through the
production bridge, with both shared lowering and the genuine RV64 fallback.
Additional fixtures cover dirty incoming i32 carriers, nested divide/overflow/
unreachable traps, a four-byte declaration prefix, trap reuse, recursive stack
exhaustion, zero warm-call allocation, exact frame capacity, and both executable
publication ownership paths.

Two independent source reviewers accepted this stateless envelope after fixing
the local-declaration PC offset. Full compiler suites passed three ordinary and
three checked-register QEMU runs; host compiler/encoder/ABI suites passed three
runs in each mode. Shared runtime tests passed three AMD64 and three RV64 QEMU
runs. Focused host race checks, host/RV64 vet, runtime staticcheck with the existing
unused-test-field exclusion, and the documentation checker passed. Diagnostic
guest fingerprints match in ordinary/checked builds and diagnostic implementation
symbols remain absent from force-linked ordinary AMD64/RV64 binaries.
Evidence is in the task workspace's `evidence/production-envelope-*.log` files.

QEMU runs disabled Zba/Zbb/Zbs/Zicond/V and establish emulated correctness only.
Native cache/signal/performance qualification remains unrun. Stateful production
lowering, public backend/configuration/artifact binding, and broader feature
parity remain tracked in [the parity tracker](riscv64-parity-tracker.md).
