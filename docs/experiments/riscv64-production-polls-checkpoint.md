# RV64 cooperative cancellation checkpoint

Production compilation can now emit cooperative interruption polls at function
entries, integer loop headers and each bulk-loop iteration. Public binding is
still disabled. That binding must always select Interruptible while asynchronous
host interruption is unavailable, including the callable-artifact contract.
Experimental compilation rejects an Interruptible request because its trap ABI
does not implement the production cancellation protocol.

Polls read the aligned invocation trap word with LWU and an acquire FENCE R,RW.
The typed fixed-width fence encoder validates its four-bit ordering sets and has
independent GP-write decoder coverage. Its live LLVM 19 assembler comparison
passed. The ordering follows the baseline [RISC-V memory-ordering specification](https://docs.riscv.org/reference/isa/v20260120/unpriv/rv32.html#_memory_ordering_instructions).
This data fence is distinct from executable publication's process-wide Linux
instruction-cache synchronization.

Entry guards precede polls and all guest-frame writes. The clear entry path
preserves the frame-size register and incoming transport. Loop backedges target
the poll, including indexed loops carrying parameter vectors. Bulk polls preserve
absolute source/destination cursors and count; an interruption can follow earlier
in-bounds writes. Cold interruption records standard trap code 12 and the precise
entry/opcode source, then restores the production bridge in one jump.

Two independent source reviewers accepted poll ordering, register preservation,
source metadata and lifetime handling. Suggested clear-poll regressions were
added: all multivalue/indexed-control fixtures, small memory.init, both overlapping
copy directions and descriptor drop paths. Active QEMU fixtures use one Go P,
observe native progress, publish RequestInterruptAsync, wait for foreign-call
completion before cleanup, then verify nested cold unwind, source locations,
reuse and zero warm-call allocation. Another fixture interrupts a long bulk fill.

Full compiler suites passed three ordinary and three checked-register QEMU runs.
Host encoder/compiler suites passed three runs in each mode. Host/RV64 vet,
strict encoder/compiler staticcheck, ordinary/checked emission fingerprints and
ordinary diagnostic dead-code checks passed. Documentation checks passed.
Logs are in the task workspace's `evidence/production-polls-*.log` files.

The public envelope must next extend nativeCancellationSupported and qualify
actual context deadlines, manual cancellation, invocation-gate waits and
Instance.Close. The runtime deadline stub does not implement asynchronous signals;
the existing high-level context watcher must drive cooperative requests. QEMU
correctness remains separate from unrun native cache/signal/performance work.
Full parity remains in [the parity tracker](riscv64-parity-tracker.md).
