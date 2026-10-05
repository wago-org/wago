# Experimental Linux/RV64 direct-call checkpoint

Stage 4c1 extends the accepted [bulk/passive-data checkpoint](riscv64-bulk-checkpoint.md)
at `246d4e8c`. It adds genuine RV64 direct calls between defined functions in the
same instance, including self and mutual recursion. Signatures still require
exactly one i32 or i64 result and integer parameters. Void/multivalue signatures,
indirect/tail/imported/cross-instance/host calls, tables/elements, reference types,
FP and SIMD remain excluded. No production Wago target or artifact is enabled.

## Admission and linking

Normal Wasm validation checks call indices and argument/result types. The
whole-module allowlist then checks every function and copies its scalar signature
before emission. Hidden or unreachable unsupported shapes still fail admission.
Each function permits at most 1024 call instructions; argument processing is
charged against the existing 16384-operation budget, including dead code.
The existing 256-parameter, 512-operand, 128 KiB frame and 16 MiB image limits
remain in force. Function metadata exposes actual emitted DirectCalls and
FrameHeaderBytes for qualification.

Calls use fixed-size AUIPC/JALR envelopes with RA linkage. Relocation sites are
rebased by the actual framed prologue and function image offset, then patched
against the callee's full entry after all function layouts are known. Forward,
backward, self and mutual calls target the callee's guarded prologue. All fixups
finish before executable publication; no absolute executable addresses, alternate
machine backend or Go helper perform guest calls.

## Guest transport and frame ownership

Every live caller value has an authoritative canonical SP home. Arguments are
loaded from those homes into the existing instance-owned serialized arena;
i32 arguments fill an entire eight-byte cell after LWU normalization, while i64
arguments retain all bits. Each callee consumes all incoming parameters into its
own frame before nesting, so the same bounded arena can serve recursive calls.
Successful results are copied immediately from the fixed result cell into the
caller's canonical home. This also preserves operands below the call arguments
and live locals across shared or fallback callees.

Shared callees may allocate A0 through A3, so callers must preserve the transport
pointers and stack fence independently of those registers. Calling functions
use this 48-byte header; noncalling functions retain the 16-byte header.

| Offset | Saved value |
| --- | --- |
| 0 | RA |
| 8 | A1, result-cell pointer |
| 16 | A0, argument-arena pointer |
| 24 | A2, trap-cell pointer |
| 32 | A3, stack fence |
| 40 | Alignment padding |
| 48 onward | Calling-function locals, then canonical operand homes |

The caller reloads A0 through A3 before marshalling and immediately after the
callee returns. It checks the restored trap cell before reading the result. A
nonzero trap bypasses later guest effects and unwinds through the normal framed
epilogue, preserving the originating trap code. A successful i32 result uses
LWU, including when the producer left dirty upper bits. X24 remains reserved for
the same-instance context. Guest allocator registers are volatile across guest
calls; this is distinct from the bridge's Go and Linux register obligations.

Every callee verifies SP against its restored fence and proves capacity for its
complete 16-byte-aligned frame before adjusting SP or writing any frame cell.
The engine's entry-frame proof is not a claim about recursive depth: each nested
entry independently checks its frame. Exhaustion produces stack trap 1, unwinds,
and permits later reuse of the same instance. Calls allocate no additional
outgoing stack arena, per-call heap object or Go callback. The maximum serialized
arena remains 256 argument cells, a result cell and the hidden context cell at
byte 2056.

Existing invocation locks, context/memory ownership, cleanup ordering, W^X and
process-wide Linux icache synchronization before callable visibility are
unchanged. No Go heap pointer crosses the native boundary.

## Qualification performed

Both independent encoder/compiler and runtime reviewers accepted the final
source without remaining blockers. They ran fresh RV64 binaries under QEMU and
host checks independently; neither modified the implementation.

| Evidence | Result |
| --- | --- |
| Exact call-admission regression on a temporary archive of `246d4e8c` | Fails on unsupported opcode 0x10; passes on this source |
| Decoded AUIPC/JALR targets and RA linkage | Forward/backward/self entries verified |
| Call count and argument-work budgets | Exact accepted/rejected boundaries, including dead calls |
| Ordinary and forced-fallback execution | Leaf, nested, self/mutual recursion, early return and i32/i64 results pass |
| Shared callee allocating A0 through A3 | Actual writes to all four transport registers verified; caller locals and stack prefix preserved |
| Shared callee followed by deep recursion | Stack exhaustion and subsequent instance reuse pass |
| 256 alternating i32/i64 arguments | Large frame homes, hidden context, integer global and later low-arity call pass |
| Nested traps 2/3/4/5 and memory ordering | Original code retained; earlier writes remain, later writes are skipped |
| Dirty-upper i32 inputs and unsigned arithmetic | Correct low-word results across calls |
| Warm recursive invocation allocation check | Zero Go allocations in the focused regression |
| Full experimental compiler QEMU suite | Three repetitions, ordinary and wago_regalloccheck builds |
| Full runtime QEMU suite | Three repetitions, including ABI/alignment and prewrite stack bounds |
| Nine focused existing-target/runtime/compiler/encoder packages | Host ordinary and wago_regalloccheck passes |
| Identical direct-call fixtures on established AMD64 | Expected recursion, parity and pressure results pass |
| Compiler/runtime race, AMD64/RV64 vet and staticcheck | Pass |

Builds use `CGO_ENABLED=0`, `GORISCV64=rva20u64` and baseline fixed-width RV64G
guest emission. QEMU runs disable zba/zbb/zbs/zicond/vector extensions. A typical
focused reproduction from the repository root is:

```sh
GOOS=linux GOARCH=riscv64 CGO_ENABLED=0 GORISCV64=rva20u64 \
  go test -c ./src/core/compiler/backend/railshot/riscv64 -o /tmp/rv64-calls.test
qemu-riscv64 -cpu rv64,zba=false,zbb=false,zbs=false,zicond=false,v=false \
  /tmp/rv64-calls.test -test.run 'Test(NativeDirectCall|DirectCall)' -test.count=3
WAGO_SHARED_SCALAR=1 go test ./src/core/compiler/backend/railshot/riscv64
```

The local RISE smoke script now includes leaf/early-return calls and a shared
callee followed by recursion/fence exhaustion. Native RISE access remains
unverified and those native gates have not run. QEMU establishes these correctness
results only; native cache behavior and performance qualification remain open.
No remote CI, publication or permissions change was performed. No #800 checker
integration is assumed; it awaits a verified merged contract. Indirect/cross-instance
calls, reference tables and multivalue belong to subsequent reviewed slices.
