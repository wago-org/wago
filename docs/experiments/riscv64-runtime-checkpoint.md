# Linux/RV64 runtime boundary checkpoint

Baseline remains #802's squash, `03a2db03ac06bcf20d5411272a7dce80a2138fae`.
This follows the accepted encoder checkpoint on `feat/rv64-encoder-checkpoint`.
The approved next-stage scope is the minimal runtime bridge, publication/cache
synchronization, stack/ABI qualification and independent review, with another
checkpoint before shared-scalar integration.

## Implemented boundary

`src/core/runtime/riscv64` is an isolated experimental package. The existing
AMD64/ARM64 core runtime and compiler architecture tags remain unchanged.
This package uses the merged `codeimage.Image` transfer interface, so staging,
owned transfer and copied-code publication converge on one sealing path.

The bounded `CodeBuffer` maps RW memory and grows by copying both the existing
logical image and the append source before retiring the old mapping. A source
may alias the old mapping, including its writable tail. `Take` transfers once;
producer `Close` cannot retire transferred memory. Failed growth retirement
poisons the builder and retains both mappings if cleanup also fails.

Publication validates fixed-width logical length and the transferred base,
changes the entire mapping to RX, then invokes Linux `riscv_flush_icache` with
flags zero over the logical code range. Only successful synchronization exposes
a callable `Program`. Local `FENCE.I` is insufficient for process-wide hart migration.
Entry pointers stay private. `Call` reserves the image until native return;
`Program.Close` waits for invocation quiescence. Failed unmaps retain ownership
for retry. A nonnil owner returned with a construction/publication error is
closed, rejects entry and must be closed again to retry cleanup.

The foreign stack uses current-main 4 MiB default, 512 KiB minimum, 1 GiB maximum
and 256 KiB fence margin. An AMD64-side contract test compares these values and
the 24-byte trap cell with the current core runtime. The bridge's 128-byte integer
save area is defined in Go; assembly offsets come from `go_asm.h`.

Both the Go wrapper and raw assembly check the full declared maximum guest
descent, including the bridge save area, before foreign-stack writes. The raw
entry also checks alignment and unsigned bounds. Guest SP is 16-byte aligned;
Go ABI0 stack arguments are marshaled explicitly, independently of ABIInternal.
The integer bridge preserves RA, original SP, x8/x9, x18..x25, Go CTXT/x26 and
g/x27, GP and TP. GP/TP/g remain valid while the guest executes.

The trusted snippet ABI takes a0/a1 inputs, returns a0, provides a2 as the stable
off-heap trap-cell pointer and a3 as the stack fence. Snippets must balance SP/RA
and respect their proven stack descent. Raw integer arguments do not establish
Go buffer ownership; callers retain any referenced buffer owners through return.
Calls on an Engine serialize; closing it waits for the active invocation. The
bridge parks Go through `entersyscall` before foreign entry and reacquires the
scheduler processor through `exitsyscall` on return, with no intervening Go
allocation or callback. Engine and Program owners remain live through return.

Primary contracts:
[Linux instruction-cache synchronization](https://docs.kernel.org/arch/riscv/cmodx.html),
[RISC-V ELF integer psABI](https://riscv-non-isa.github.io/riscv-elf-psabi-doc/),
[Go 1.27.1 ABIInternal](https://github.com/golang/go/blob/go1.27.1/src/cmd/compile/abi-internal.md).
Linux psABI, Go ABI and this guest convention are separate. This bridge does
not preserve or qualify floating-point callee registers.

## Executed evidence

Host: Linux/AMD64, Go 1.27.1, Linux 6.12.111. QEMU user-mode 10.0.13 was extracted
into the task workspace without installing system packages. RV64 test binaries
were freshly cross-built with `CGO_ENABLED=0`, `GORISCV64=rva20u64`. The Go host
profile can require instructions beyond the JIT's fixed-width integer subset.
QEMU used `rv64,zba=false,zbb=false,zbs=false,zicond=false,v=false`.

| Check | Result |
| --- | --- |
| Full new runtime tests under baseline QEMU, three repetitions | Passed. |
| ABI sentinels | Passed: integer saved registers and CTXT restoration; GP/TP/g live values; original SP, RA continuation and guest SP alignment. |
| Raw pre-write bounds | Rejected invalid alignment, undersized/overflowing descent and reversed bounds without changing any foreign-stack byte; exact 128+16 boundary executed correctly. |
| Arithmetic, traps and reuse | Raw i64 wrapping/upper-bit cases and repeated trap/normal reuse passed; successful steady-state calls allocated zero times. |
| Copied and grown/transferred publication | Both executed framed snippets successfully, including entry past a page-sized prefix after image growth. |
| Cross-thread publication | Eight images, four locked consumer OS threads per image, 32 calls per consumer, with concurrent GC; passed. |
| Active foreign-call GC | With one Go scheduler processor, guest announces entry, sleeps bounded 100 ms and itself reads completed-GC flag before return; passed five focused repetitions plus full-suite repetitions. |
| W^X and publication order | Actual `/proc/self/maps` permissions RW then RX; injected ordering check confirms RX then synchronization before exposure; passed. |
| Failure ownership | Injected protect, synchronization, old/new unmap and trap-map failures; retained closed cleanup owners and successful retries; passed. |
| Lifetime and serialization | Active image retirement waits; closed image rejects entry; concurrent calls on one Engine serialize; passed. |
| Portable Linux/AMD64 package race tests | Passed. |
| Existing core runtime, encoders, shared compiler and AMD64/ARM64 backend tests | Passed in ordinary and `wago_regalloccheck` builds on AMD64. |
| New package vet and staticcheck | Passed for Linux/AMD64 and Linux/RV64. |
| Documentation and whitespace checks | Passed. |

Commands run from the repository root with scoped writable caches, `GOWORK=off`
and `GOMAXPROCS=4` (the scheduler test temporarily sets one processor):

```sh
GOOS=linux GOARCH=riscv64 CGO_ENABLED=0 GORISCV64=rva20u64 \
  go test -c ./src/core/runtime/riscv64 -o /tmp/runtime-riscv64.test
qemu-riscv64 -cpu rv64,zba=false,zbb=false,zbs=false,zicond=false,v=false \
  /tmp/runtime-riscv64.test -test.v -test.count 3 -test.timeout 60s
go test -race -count=1 ./src/core/runtime/riscv64
WAGO_RV64_CLANG=/usr/bin/clang-19 go test -p 4 -count=1 \
  ./src/core/runtime/riscv64 ./src/core/runtime ./src/core/encoder/... \
  ./src/core/compiler/backend/railshot/shared \
  ./src/core/compiler/backend/railshot/amd64 \
  ./src/core/compiler/backend/railshot/arm64
WAGO_RV64_CLANG=/usr/bin/clang-19 go test -p 4 -tags=wago_regalloccheck -count=1 \
  ./src/core/runtime/riscv64 ./src/core/runtime ./src/core/encoder/... \
  ./src/core/compiler/backend/railshot/shared \
  ./src/core/compiler/backend/railshot/amd64 \
  ./src/core/compiler/backend/railshot/arm64
go vet ./src/core/runtime/riscv64
GOARCH=riscv64 CGO_ENABLED=0 go vet ./src/core/runtime/riscv64
staticcheck ./src/core/runtime/riscv64
GOARCH=riscv64 CGO_ENABLED=0 staticcheck ./src/core/runtime/riscv64
just docs
```

## Independent review and remaining gates

An independent reviewer inspected publication order, cleanup ownership,
invocation locking, assembly bounds/restoration and scheduler parking. They ran
portable race tests/vet and a separate fresh RV64 cross-build with baseline QEMU
full-suite count three. They identified that short-call concurrent GC did not
prove GC completed during an active foreign invocation. The bounded scheduler
fixture above was added; the reviewer inspected it and independently ran the
scheduler, ABI and bounds fixtures five times, then the grown-image fixture five
times. No blocking finding remains for
this narrow checkpoint. AI assistance was used for implementation and review.

This is not a production Wago target. The 19-hook adapter and strict whole-module
allowlist are the next review boundary. No Wasm modules are admitted here, and
no AMD64/ARM64 fallback code is emitted. Current core CodeBuffer/engine plumbing,
compiler selection, thunks, artifact loading, prepared/host-call layouts and
signal/cancellation paths still require deliberate integration. The current
65,535-slot host-frame layout was audited but is not wired into this integer ABI.
Memory, guest calls, FP/host, SWAR SIMD and broader qualification remain later
stages of the approved plan.

QEMU establishes correctness evidence only. Native multi-hart cache/lifetime
qualification, minimum Linux/Go versions and native performance remain untested.
No benchmarks or native performance claims were made. Full repository/corpus/
official Wasm conformance suites were not run for this isolated checkpoint; the
repository's full before-publication policy still applies. No PR was published,
no branch was pushed and no other checkout or PR was modified.
