# Experimental Linux/RV64 memory and globals checkpoint

Stage 4b1 extends the [integer/control checkpoint](riscv64-integer-checkpoint.md)
with genuine RV64 integer memory and global lowering. It remains isolated from
the production Wago compiler, CLI and artifact architecture. The original
post-#802 baseline and previous accepted commits are retained; no old #284 code
or active user worktree was restored or modified.
This document records Stage 4b1; the later
[bulk checkpoint](riscv64-bulk-checkpoint.md) extends passive-data admission and
the context layout while retaining the qualified single-memory scope.

Before this stage, a readonly refresh observed main at `5273dde1` after #799 and
open #803 at `694e9e8102fb4e4428468995863f05700fadcd29`. Their changes concern
AMD64 scratch/pinned-register handling. Their shared-contract effects were
checked; neither PR was modified or copied. This stage keeps fixed fallback
temporaries separate from the shared value/register allocator.

## Admission and guest semantics

Complete-module admission still finishes before emission or executable
publication. This substage allows one nonshared wasm32 memory, either defined
or explicitly named and bound by `InstantiateWithOptions`; integer globals;
and active data segments with literal i32 offsets. Only function, memory and
integer global exports are accepted. Globals use literal i32/i64 initializers;
mutable global set and immutable/mutable get are supported. Each instance has
private global cells, including when its memory is shared with other instances.

Memory/global instructions use the separate whole-function walker and canonical
stack slots. Context-free shared scalar functions continue using the existing
19-hook target. Every function, including hidden or unreachable unsupported
instructions, receives admission checks. Integer loads/stores, their signed and
unsigned narrow variants, memory.size and memory.grow are supported. These are
19 integer load/store opcodes. FP memory operations remain rejected.

Addresses discard raw incoming i32 upper bits. The uint32 immediate offset is
added in 64 bits, without address wraparound. Before loading the memory base or
touching bytes, emitted checks prove `length >= width` and
`effective_address <= length - width`. Bytewise LBU/SB sequences implement
little-endian accesses at every alignment without relying on misaligned host
loads. A failing store writes no partial bytes. Signed loads extend at their
declared width; i32 carriers and returns are zero-normalized.

The runtime reserves a stable RW anonymous mapping up to 256 pages (16 MiB)
per memory. Its descriptor holds the base, logical byte length and effective
maximum byte length. The reservation quota is distinct from the Wasm declared
maximum, which is preserved for import matching. Initial sizes above the quota
are rejected. Growth can fail below a declared maximum because of the quota;
the [Wasm memory.grow rules](https://webassembly.github.io/spec/core/exec/instructions.html#exec-memory-grow)
permit resource failures. Success returns the old page count, exposes zeroed
pages and updates the single authoritative descriptor length. Failure returns
normalized i32 -1 and leaves size unchanged; zero growth succeeds at the limit.
Guest growth uses no Go callback, mapping relocation or per-call allocation.

Named imports are matched atomically with lease acquisition using current page
count and original declared maximum. A quota does not manufacture a declared
maximum for a provider that lacks one. A previously grown provider can satisfy
a larger import minimum. Active data initializes in segment order: an entire
segment is checked before its copy, while earlier segment writes survive a
later failing segment on imported memory. This follows the established Wago
semantics in `src/wago/instantiate.go` and its imported-memory regression.
Executable publication happens only after successful initialization.

There are at most 256 globals and 256 active data segments. Prior module,
function, operand, control, instruction, code and frame limits remain. Bulk
memory, passive data, data-count metadata, multiple/shared/memory64 memories,
global/function imports, start functions, tables, calls, FP, SIMD and extended
constant expressions remain excluded. This is the first bounded Stage 4b
checkpoint; bulk memory is a separate implementation and review boundary.

## Runtime context and ownership

Serialized call storage keeps the existing argument and fixed result offsets,
then appends one hidden context cell. A framed stateful function loads that
cell into reserved X24 only after stack-capacity checks. A module with 256
parameters places it at byte 2056, exercising large-offset materialization.
The RW guest context contains a memory-descriptor cell followed by integer
globals; the last of 256 globals exercises byte offset 2048. Context-free
functions need no context load. The existing ABI0 bridge still preserves the
host's X24 and all other reviewed Go/guest ABI sentinels.

Instances own their context, call frame, engine and executable program. Defined
memories also have instance-owned creator references; imported memories retain
their external creator ownership. Memory leases keep the mapping and descriptor
alive through calls. Creator Close prevents new leases and creator access, but
existing leased instances remain callable until their leases close. This is an
explicit experimental owner policy, distinct from the public Wago memory API's
live-importer Close policy. No production API behavior changes.

Instance, context and memory locks serialize invocation, host memory/global
access, growth and retirement. Their order is instance, context, memory owner,
call frame, engine, program. A context with memory must have the matching live
lease before native entry; globals-only contexts use the nil-lease path. Copied
lease handles share a close token and cannot decrement ownership twice. No Go
heap pointer crosses the bridge. Failed unmaps retain their owners for explicit
Close retry, including constructor failure and either descriptor/data cleanup
failure order. Zero-value and closed owners cannot reach descriptor reads.

Guest bounds failures use experimental trap code 5, joining the existing stack,
unreachable, division and overflow trap islands and common framed epilogue.
This remains separate from the production TrapCode namespace. Executable W^X,
process-wide Linux icache synchronization and quiescent retirement are retained.
Memory/context mappings are RW and never executable.

## Qualification actually run

Host: Go 1.27.1 Linux/AMD64. Execution: workspace-local QEMU user 10.0.13,
fresh no-cgo Linux/RV64 binaries, GORISCV64=rva20u64, with optional guest
extensions disabled:
`rv64,zba=false,zbb=false,zbs=false,zicond=false,v=false`.

| Gate | Result |
| --- | --- |
| Full compiler under baseline QEMU | Passed three repetitions, ordinary and wago_regalloccheck builds. |
| Integer memory | All 19 opcodes, every mod-8 alignment, last valid access, dirty address upper bits, signed narrow edges and negative i32 loads passed independent Go expectations. Successful stores start with distinct bytes and verify neighboring sentinels. |
| Bounds and reuse | Zero-size memory, offset/address overflow, one-past-end and huge addresses trap; whole-store preflight and successful calls after traps passed. |
| Growth/import lifetime | Zero/success/failure/huge-delta growth, zeroed new pages, current size seen by two importers and host, exact name/limit binding, creator Close with live importers and concurrent calls passed. |
| Initializers/globals | Ordered imported initializer failure preserves earlier writes; globals remain private across instances; mutable i32/i64, immutable i32 and globals-only context calls passed. |
| Context/frame extremes | A 256-parameter sibling function fixes context offset 2056; global 255 uses offset 2048. Stateful invocation with reused argument storage measured zero steady-state allocations. |
| Runtime ownership | Full baseline QEMU runtime passed three repetitions; portable race checks and injected construction/unmap failures, copied leases, context/lease mismatch and Close synchronization passed. |
| Admission | Shared/memory64/multiple memory, FP load, bulk operations, oversized state and extended constants rejected before publication. Previous denials retained. |
| Encoder | Independent LBU/SB encodings and live Clang 19 assembly comparison passed. |
| Existing-target compatibility | Nine runtime/encoder/shared/AMD64/ARM64/RV64 packages passed ordinary and allocation-check suites on AMD64 with shared scalar enabled. |
| Vet/staticcheck | Compiler/encoder/runtime passed on Linux/AMD64 and Linux/RV64. |
| Independent review | Two readonly reviewers found no production blockers. Ownership guards and stronger successful-store sentinels were incorporated and focused qualification rerun. |

Focused reproduction:

```sh
export GOWORK=off GOMAXPROCS=4 GOCACHE=/tmp/wago-rv64-go-cache
GOOS=linux GOARCH=riscv64 CGO_ENABLED=0 GORISCV64=rva20u64 \
  go test -c ./src/core/compiler/backend/railshot/riscv64 -o /tmp/rv64-memory.test
qemu-riscv64 -cpu rv64,zba=false,zbb=false,zbs=false,zicond=false,v=false \
  /tmp/rv64-memory.test -test.count 3 -test.timeout 3m
# Repeat cross-build with -tags=wago_regalloccheck before executing that binary.
go test -race ./src/core/compiler/backend/railshot/riscv64 ./src/core/runtime/riscv64
WAGO_RV64_CLANG=/usr/bin/clang-19 go test ./src/core/encoder/riscv64
just docs
```

The local RISE smoke selector now includes zero-memory, high-capacity context
and globals-only execution before the supported suites. Bash, shellcheck and
actionlint were checked locally. Publication remains explicitly held: no push,
PR, remote CI, App installation or credentials/OAuth changes occurred.
RISE access and physical runner execution remain unverified. QEMU establishes
correctness only; native cache, performance and release qualification are pending.
