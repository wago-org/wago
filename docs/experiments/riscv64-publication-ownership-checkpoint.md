# Executable publication and cleanup ownership

The shared production publication path now preserves cleanup ownership across
failed RX protection, instruction-cache synchronization, registration and unmap.
The change applies to existing AMD64/ARM64 targets as well as Linux/RV64.

`MapCode` returns entry zero on every failure. When rollback succeeds its mapping
is nil. When rollback also fails, it returns the original full mapping and both
errors; callers retain that mapping and retry `Unmap`. Platform protection choices
are unchanged, including Darwin MAP_JIT and Windows instruction-cache flushing.
The process-lifetime host-stub owner stores the returned mapping before checking
the initialization error. Command-line diagnostic consumers likewise establish
cleanup before checking errors.

Public Compiled owners retain failed main and shared-thunk releases. A cold,
per-module list retains failed private thunk releases and publication rollbacks.
The shared code-cache lock orders mapping, failed ownership transfer and cleanup.
One gate checks both shared call modes and private failures immediately before
publication. A persistent failure prevents another mapping from accumulating.
Successful mappings remain callable while owned; failed mappings carry no callable
base. Close, deferred last-instance release and decoded replacement clear ownership
only after successful unmap. Close retries retained failures without double release.

A main-image sealing failure closes the module because protection may already
have made its pages RX. Instantiation uses the public mapping owner for sealing,
while execution metadata remains its frozen snapshot; both code views are cleared
on failure. Cleanup remains attached to the code cache. The fixed Compiled/cache
owner layout and its allocation count are unchanged; fault hooks and failure lists
live only in the cold index sidecar.

Focused regressions inject publication and rollback errors, exercise the real
Runtime/Module instantiation path for private/shared thunks and main-image sealing,
retry each ownership category, replace decoded state, mix synchronous/asynchronous
shared call modes and coordinate two concurrent private builders. They establish
zero entry, bounded failure retention, cleanup retry and absence of double release.

Two independent reviewers accepted the implementation after the common gate and
public-owner routing fixes. Host runtime tests, focused public/race/profile tests,
fixed-owner allocation/layout tests and RV64 QEMU runtime tests passed. ARM64
runtime/public, Darwin ARM64 runtime and Windows AMD64 runtime tests cross-compiled.
Host/RV64 vet passed. Runtime staticcheck passed with the repository's existing
unused-test-layout diagnostics excluded; the broader public-package staticcheck
run still reports pre-existing diagnostics in unchanged files. No native RV64
hardware qualification was run. Evidence is in the task workspace's
`evidence/publication-ownership-*.log` files.

This closes the ownership prerequisite from
[the production runtime checkpoint](riscv64-production-runtime-checkpoint.md).
Compiler integration and full feature parity remain tracked in
[the parity tracker](riscv64-parity-tracker.md).
