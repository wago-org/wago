# Experimental Linux/RV64 bulk memory checkpoint

Stage 4b2 extends the accepted [memory/globals checkpoint](riscv64-memory-checkpoint.md)
at `3cc68aca`. It adds genuine RV64 memory.init, data.drop, memory.copy and
memory.fill, passive data and consistent data-count metadata. The scope remains
one nonshared wasm32 memory, with the existing integer/control and globals
allowlists. Passive-data-only modules can execute data.drop without a memory.
Calls, tables/elements, FP, SIMD, memory64, shared/multiple memories, starts,
extended initializers and other unsupported features remain excluded.
No production Wago target, CLI or artifact architecture is enabled.

The later [direct-call checkpoint](riscv64-direct-call-checkpoint.md) extends
this historical scope with defined same-instance direct and recursive calls.

## Admission and owned segment state

Normal decoding/validation establishes data-count consistency and requires it
for memory.init/data.drop. Missing or mismatched counts fail normal validation,
including zero counts when an index is used. Empty consistent metadata is now
accepted. Whole-module state and every function are admitted before emission;
the prefixed-instruction walker accepts only these four additional operations.
Unsupported prefixes still fail in hidden or unreachable functions.

The compiled module owns its input and data bytes. It retains original data
index order, including interleaved active/passive/empty segments. Active bytes
still initialize in segment order before publication, with prior imported-memory
writes preserved if a later segment fails. Active segment records start dropped
in any successfully published instance. Passive bytes are private per instance,
even when instances share an imported memory; they never initialize memory
implicitly.

The guest context preserves the old memory/global cell offsets, appends one
16-byte base/length record per data index, then owns copied passive bytes in the
same anonymous RW mapping. The constructor checks at most 256 globals, 256
segments and 1 MiB of passive payload before its sole mmap. Metadata adds at most
6152 bytes, plus page rounding. The module's existing 1 MiB input bound also
bounds data before emission. Empty or active records have zero base and length.
At maximum state size, the last record is at byte 6136 and its length at 6144;
both require reserved-scratch address materialization.

data.drop sets the record length to zero and is idempotent. Its payload stays
owned until context/instance Close, keeping retirement bounded and avoiding Go
callbacks or unmaps during guest entry. Later memory.init observes length zero.
The existing context lock covers data mutation and calls, including nil-lease
passive-only calls. Context Close waits for active entry and retains its sole
mapping on failed cleanup for explicit retry. Memory import leases, invocation
lock order, executable W^X and process-wide Linux icache synchronization are
unchanged; no Go heap pointer crosses the native boundary.

## Bounds, copying and trap order

All three bulk operands come from canonical low-word i32 homes. Fixed temporary
registers X5/X6/X7 hold destination, source/value and count; X28/X29 provide
descriptor/length/byte temporaries. X30/X31 remain permanently reserved, X24
holds the framed context, and A2 retains the trap cell. Long branches clobber
X31, so data-record addresses are recomputed when needed after transfers.

Unsigned 64-bit subtraction guards prove `count <= length` and
`offset <= length - count`. Destination is checked first; copy/init then check
their complete memory/data source range. Only then can zero count exit or byte
access begin. An end offset with zero count is valid; beyond-end offsets trap
even for zero count. Active, empty or dropped segments allow source zero/count
zero, while source nonzero/count zero still traps. Failed preflight leaves the
entire destination unchanged.

These rules follow the official [Wasm bulk-memory semantics](https://webassembly.github.io/spec/core/exec/instructions.html#exec-memory-init).
Copy implements memmove by choosing forward traversal for destination <= source
and backward traversal otherwise; self-copy still performs full bounds checks.
Fill uses the low byte of its i32 value. Byte LBU/SB loops impose no alignment
requirement, use no alternate ISA backend/helper, and are bounded by current
logical memory (at most 16 MiB), or by the smaller passive source range for init.
Growth changes the authoritative descriptor length without moving the base.

Bounds failures retain experimental trap code 5 and the common framed epilogue.
Earlier successful instructions keep their effects; later instructions after a
trap do not execute. A reached drop remains effective if a following instruction
traps. Subsequent invocations can reuse the same stack/trap/result cells.
Cancelable contexts and production trap-namespace integration remain excluded.

## Qualification actually run

Host: Go 1.27.1 Linux/AMD64. Execution: fresh no-cgo Linux/RV64 test binaries,
GORISCV64=rva20u64, workspace-local QEMU user 10.0.13 with optional guest
extensions disabled:
`rv64,zba=false,zbb=false,zbs=false,zicond=false,v=false`.

| Gate | Result |
| --- | --- |
| Full baseline QEMU | Compiler and runtime passed three repetitions; compiler also passed three repetitions with wago_regalloccheck. |
| Copy | Go copy/memmove expectations passed for overlap in both directions, self-copy, disjoint/end/full-page copies and 64 seeded cases, including dirty i32 upper bits. Entire-memory comparisons check neighboring bytes. |
| Fill | Low-byte truncation, negative/raw carriers, unaligned/end/full-page fill and zero-length cases passed independent expected bytes. |
| Preflight | Source-invalid/destination-valid, destination-invalid/source-valid, one-past-end, self-copy OOB and huge counts trap without partial destination changes. Zero-count offsets retain bounds checks. |
| Init/drop lifecycle | Interleaved active/passive/empty indices, active initialization and immediate drop state, source ownership after original module mutation, repeated drop and per-instance drop isolation on shared imported memory passed. |
| Trap sequencing | Fill before a failing init persists; later fill/drop does not execute. Drop before a failing init persists. Trap followed by success reuses the runtime correctly. |
| Zero memory/growth | Bulk zero-count and source checks on zero logical memory passed; init/copy/fill after growth use the new length, with correct bytes and end/OOB checks. |
| Context extremes | 256 globals and 256 data records exercised record offsets 6136/6144. Init/copy/fill with reused argument storage measured zero steady-state allocations; globals remain intact. |
| Runtime ownership | Copied-input layout, caps before mmap, exact payload quota, map failure, unmap retry and quiescent Close during a passive-only data-mutating call passed. |
| Admission | Missing/mismatched data-count and hidden unsupported prefixes rejected before publication; previous unrelated denials retained. |
| Compatibility | Nine runtime/encoder/shared/AMD64/ARM64/RV64 host packages passed ordinary and allocation-check builds with shared scalar enabled. |
| Portable analysis | Compiler/runtime race, vet and staticcheck passed; vet/staticcheck also passed Linux/RV64. |
| Independent review | Two readonly reviewers assessed admission, bounds, register use, segment ownership/lifecycle and focused fresh QEMU/race evidence. |

Focused reproduction:

```sh
export GOWORK=off GOMAXPROCS=4 GOCACHE=/tmp/wago-rv64-go-cache
GOOS=linux GOARCH=riscv64 CGO_ENABLED=0 GORISCV64=rva20u64 \
  go test -c ./src/core/compiler/backend/railshot/riscv64 -o /tmp/rv64-bulk.test
qemu-riscv64 -cpu rv64,zba=false,zbb=false,zbs=false,zicond=false,v=false \
  /tmp/rv64-bulk.test -test.count 3 -test.timeout 3m
# Repeat cross-build with -tags=wago_regalloccheck before executing that binary.
go test -race ./src/core/compiler/backend/railshot/riscv64 ./src/core/runtime/riscv64
just docs
```

Local RISE smoke now includes bulk zero-memory/growth and passive-only context
execution before the supported suites. Bash syntax, shellcheck and actionlint
were checked locally. Publication remains explicitly held: no push, PR, remote
CI, App/OAuth, credential or security changes occurred. RISE access and physical
qualification remain unverified. QEMU establishes correctness only; native
cache/performance/release qualification is pending. The requested RV64 clobber
detection dependency on #800 has not been assumed merged or integrated here.
