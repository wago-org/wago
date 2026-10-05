# RV64 experimental public integration checkpoint

Linux/RV64 now connects the genuine RV64 compiler and shared production runtime
to Wago's public Compile, Runtime.Compile, Instantiate, ordinary WasmFunc and
PreparedSession paths behind `wago_rv64_experimental`. This tag is required;
the bounded feature set is not production parity. No AMD64/ARM64 native fallback
is present. Baseline main is `068208990b37d413141126325a85c32483b93895`.

The decoded shared compiler entry point preserves CompileOptions and finalized
image ownership. Deferred output returns heap Code with no CodeImage; a nonnil
CodeImage owns an RW mapping transferable through Take. The public cache performs
W^X and process-wide Linux instruction-cache synchronization before callable
visibility. Every public source compilation forces cooperative polls. Artifact
architecture identity 3 denotes the framed, polling RV64G wrapper ABI; other
architecture images and encoded prepared-entry proofs are rejected.

Supported modules contain bounded integer functions, direct same-instance calls,
structured/indexed control and multivalue results, one defined nonshared memory32,
integer globals, literal active/passive data, and bulk memory operations. Raw i32
upper bits are handled by the backend and public boundary. Configured memory,
metadata, native-stack and code-size policies remain authoritative within the
backend's additional implementation bounds: 1 MiB source/instruction/data budget,
1024 functions/types, 256 signature/local/global/data limits and 16 MiB native code.
The public feature catalog exposes the corresponding feature families while
whole-module admission still restricts their resource shapes.

Source admission retains present-empty section and type-grammar provenance after
normal frontend validation. Decoded callers receive pre-validation instruction
and data-copy bounds. Trusted artifact and hand-built metadata admission also
bounds unused type signatures, inert function descriptors, active/passive
directories and their combined byte payload. Active entries can overlap nil
placeholders in the passive original-index directory; directory lengths are
bounded independently rather than incorrectly double-counted.

Imports, start functions, tables/references, noninteger types, extended
initializers, multiple/shared/memory64 memories, GC/EH, custom machine code and
signal-based bounds remain rejected before callable publication. Profiling and
source/unwind metadata requests also reject explicitly. No register-ABI or other
unimplemented optimization is advertised. Conservative worker/compaction hints
can use the serial fully checked path.

RV64 has no parked-host control-frame ABI. Frame sizing, initialization,
registration and all host-entry APIs reject; zero HostCtrlFrameBytes denotes its
absence. Forced synchronous-host options, including caller-resolver forcing,
are rejected with an error before mapping or infallible arena sizing. This fixes
a panic found by independent review. Failed requests leave ordinary subsequent
instantiation usable.

Two independent source reviewers accepted the final bounded integration after
artifact-limit and host-mode corrections. Focused public QEMU tests cover live
code owners after Compiled.Close, architecture identity and trusted round trips,
prepared function/session fallbacks, active/passive initializer ownership,
passive use after close and Go GC, aliased global cells/direct calls, memory quota
growth, native code limits and zero warm ordinary-call allocations. Cancellation
tests cover context deadlines, queued canceled invocation gates, active manual
cancellation, loaded-artifact cancellation and active Instance.Close with one Go P;
calls reuse clean trap cells afterward.

Host encoder/compiler suites, full RV64 compiler QEMU suites and focused public
QEMU suites passed three runs in each ordinary/checked-register mode. RV64 plugin
and host-rejection fixtures passed three runs. The AMD64 shared-scalar reference
and race checks passed, as did Linux/ARM64 public test compilation, experimental
RV64 repository-wide build, host/RV64 vet, emission fingerprints and diagnostic
dead-code checks. Strict compiler/encoder/frontend/plugin staticcheck passes;
broader public analysis retains pre-existing diagnostics in seven unchanged
files. Qualification logs are in the task workspace's `evidence/public-*.log`.
The complete public/full-feature suite, compiler-free
`wago_precompiled`, TinyGo and optional profile builds are outside this checkpoint's
accepted RV64 scope. QEMU establishes functional correctness; native multicore
cache, signal and performance qualification remains unrun. Remaining work is
tracked in [the parity tracker](riscv64-parity-tracker.md).
