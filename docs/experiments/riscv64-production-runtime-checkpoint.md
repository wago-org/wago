# RV64 production runtime checkpoint

This checkpoint adds Linux/RV64 support to the shared `src/core/runtime`
Engine, JobMemory, Arena, CodeBuffer and executable publication path. It does
not enable the public Wago backend. The subsequent
[compiler envelope checkpoint](riscv64-production-compiler-checkpoint.md) uses
this bridge for supported stateless integer modules; the earlier experimental
runtime remains separate.

The production wrapper passes A0=serialized arguments, A1=linear-memory home,
A2=trap buffer, A3=result slots, and pins the home in X25. The guest stack begins
with a 144-byte wrapper save area preserving the Go stack pointer, RA, GP, TP,
callee-saved GPRs, CTXT, g and FCSR. The initial integer checkpoint used 128 bytes;
the subsequent FP-state boundary added FCSR and alignment padding.
Alignment and available stack capacity are checked
before any foreign-stack write. The compiler must separately check its complete
guest frame before writing it. Normal returns and cold traps restore the same
wrapper state; cold traps use basedata offsets 24/32, avoiding the maximum-page
cache at offset 16. These offsets are also exposed in the shared ABI package.

The ordinary wrapper releases the Go P through the existing syscall scheduler
transition. The bounded entry remains reserved for a compiler proof. Production
host callbacks, host control frames, guarded memory and asynchronous signal
interruption are unsupported and fail closed. No successful resume or prepared
host registration is fabricated.

Central Linux publication changes RW pages to RX, then issues the process-wide
`riscv_flush_icache` syscall with flags zero before returning or registering an
entry. Both fresh MapCode and grown CodeBuffer transfer/sealing use this path.
The previously reviewed CodeBuffer failure ownership fix remains in force.
Raw MapCode rollback and the existing public Compiled/thunk cleanup lifecycles
required a separate ownership fix at this checkpoint. The subsequent
[ownership checkpoint](riscv64-publication-ownership-checkpoint.md) addresses it.

Focused fixtures cover dirty i32 carriers, multiple serialized result slots,
zero-page JobMemory, saved-register sentinels, cold traps through three guest
frames, source locations, reuse after traps, and pre-write stack rejection.
A bounded one-P nanosleep fixture proves Go GC completes while production
Engine.Call is active. Grown CodeBuffer.Seal and Take→SealCode images execute on
consumer OS threads while the publisher thread remains reserved. Injected cache
syscalls verify range, flags, failure propagation and invalid-range rejection.

The full runtime suite passed three ordinary QEMU runs after final fixture edits.
Existing AMD64 runtime tests passed three runs; focused CodeBuffer/cache tests
passed with the race detector. Host and RV64 vet passed, and ARM64 runtime tests
cross-compiled. Two independent source reviewers accepted the bridge; the runtime
reviewer additionally accepted the active-GC and cross-thread fixtures after fixes.
Logs are in the task workspace's `evidence/production-runtime-*.log` files.

The subsequent FCSR boundary preserves the complete rounding-mode/exception
state before switching stacks and restores it on the shared normal/cold-return
path. The sizeof-driven guard includes the complete 144-byte save area plus the
minimum guest header. An ABI0 probe seeds RUP and all accrued flags, runs actual
FP arithmetic that changes both fields, checks normal and cold return, then
restores its original state before Go resumes. Rejected entry retains FCSR and
all foreign-stack bytes. Exact compiler-frame boundary fixtures use the expanded
area. Independent source review accepted this change. Runtime sentinels, full
compiler and focused public QEMU regressions passed three runs in ordinary and
checked modes; RV64 vet and staticcheck (excluding existing unused diagnostics)
passed. Logs are `evidence/float-runtime-*.log`. FP Wasm admission and the full
guest FP-register ABI remain separate work; this bridge change enables neither.

QEMU used RV64G with Zba/Zbb/Zbs/Zicond/V disabled. These results establish
emulated correctness only. Native multicore cache and performance qualification
remain unrun. Full production parity remains tracked in
[the parity tracker](riscv64-parity-tracker.md).
