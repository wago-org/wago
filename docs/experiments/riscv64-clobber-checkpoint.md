# Experimental RV64 typed clobber observation

This checkpoint follows the [direct-call slice](riscv64-direct-call-checkpoint.md)
at `3e76e09c`. Verified main `068208990b37d413141126325a85c32483b93895`, parent
`249501bc536cabe406b33007408afe40a9c55d16`, was integrated with local merge
`7f307da5`. It includes #800 and recent #799/#803/#805/#804/#806 fixes.
The merged [checker contract](../register-allocation-checker.md) governs this
diagnostic behavior. No old #284 branch or user worktree was restored or changed.

## Implemented coverage and current applicability

With `wago_regalloccheck`, the RV64 encoder provides a typed GP-write observer
independent of its transfer observer. Validated R/I/U instructions report their
destination, including word arithmetic, loads, SP updates and constant/normalization
sequences. Writes to hardwired x0 are discarded. Any nonzero-link JAL/JALR reports
all nonzero GP registers conservatively for physical-call effects. Noncalling
long transfers report their actual AUIPC write to X31. Stores, ordinary branches,
Ret and Nop do not invent GP destinations. Long-fixup patching adds no event.

All register, immediate, alignment and sticky-error checks precede notification;
the observer runs before the validated instruction is appended. A forbidden
write fails immediately, so write-then-restore cannot conceal it. Invalid emission
reports no physical or transfer effect. Existing emission helpers and guest
encodings remain unchanged.

The separate transfer-window API reports supported register moves, direct
SP-relative loads/stores, semantic destination kills and conservative calls.
It distinguishes GP and FP namespaces. Word and byte loads discard old upper-byte
identities; sign-extending W arithmetic kills its complete previous identity
without asserting zero normalization. Zero-register stores conservatively kill
overwritten slot identities. Replacing or nesting transfer observers never
disables GP lifetime observation.

Checked target state supplies preload-based GP reservations, outer-observer
chaining, idempotent cleanup and terminal-path mask save/restore. A reservation
starts only after its complete actual typed preload. Separately emitted terminal
paths must restore the body mask with a deferred scope after the last cache use.
Shared and fallback function attempts restore lifetime state on every exit while
preserving existing panic propagation; no recovery/debug policy was added.

**Neither current RV64 compiler path admits immutable register caches.** Shared
scalar values use reference-counted materialization/spill/reload; fallback values
use canonical homes and mutable fixed temporaries. Integer globals are loaded
afresh; X24 is a context ABI pin. Explicit real-preload controls exercise this
mechanism, including shared/fallback emission, rather than a new optimization.
Production function attempts currently have no active immutable reservation.
Current compilation also does not establish symbolic encoder transfer windows;
tests establish their input facts explicitly. Future integration must establish
real preload/input contracts before using this machinery.

Raw bytes, direct buffer writes, handwritten runtime bridge assembly and fixup
patches are outside typed observation. Compressed, atomic, FP/vector and
conditional GP-write instructions are not implemented by this encoder; future
emitters require explicit physical-effect coverage before admission. SP/frame
aliases after address materialization are outside the symbolic transfer API;
typed physical destination observation still covers those writes. There is no
whole-function dataflow, arithmetic/ISA or arbitrary scratch-liveness proof.

## Ordinary builds and focused qualification

Ordinary encoder and target states are leading empty placeholders; mask tokens
are zero-sized. Constant-false guards erase observer arguments and cleanup paths.
RV64 compiler files already exclude TinyGo, so no TinyGo lowering claim is made.
Standard-Go tests compare sizes, alignments and all nonchecker field offsets with
placeholder-free layouts. Ordinary observer APIs retain no callback.

`scripts/check-riscv64-diagnostics.sh` compares exact shared/fallback, recursive
and memory/growth guest fingerprints. It also force-links experimental compiler
and encoder in ordinary Linux/AMD64 and Linux/RV64 harnesses and rejects retained
diagnostic implementation symbols. Existing production DCE binaries do not
import this experimental compiler, so those alone cannot qualify its erasure.

Typed-emitter tests independently decode actual instruction destinations and
audit the current public emitter inventory. Negative controls cover word/byte/full
loads, arithmetic, same-register writes, normalization, alternate-link/long calls,
SP/RA/X31 writes and write-then-restore. Other controls cover invalid/sticky/alignment
failures, independent nested transfer windows, FP namespace facts, real preload
placement, terminal masks, outer-observer restoration on success/error/panic/rethrow
and assembler reuse. These preserve allocator plausibility while corrupting
physical effects. No production cache-admission coverage is implied.

Both independent encoder/compiler and runtime reviewers accepted the source
without blockers. The following gates were actually run, serially with two Go
workers after checking for existing test processes:

| Gate | Result |
| --- | --- |
| Full RV64 compiler QEMU suite | Three repetitions, ordinary and checked builds |
| Full RV64 encoder QEMU suite | Three repetitions, ordinary and checked builds |
| Full RV64 runtime QEMU suite | Three repetitions |
| Nine focused host runtime/compiler/encoder packages | Ordinary and checked passes |
| Checked encoder/compiler/runtime race checks | Pass |
| AMD64/RV64 vet, host staticcheck | Ordinary and checked passes |
| Standard layouts, ordinary inert observers, force-linked DCE | Pass |
| Ordinary/checked and pre-infrastructure guest fingerprints | Exact match for all recorded fixtures |
| Short matched ordinary compile sample against `7f307da5` | Same 98 allocations and 32864 bytes per operation |
| Ordinary R/I/U helper disassembly | No diagnostic observer calls |

The cost sample used 100 iterations, three repetitions, the same host/toolchain
and two Go workers. Observed time was roughly 35–43 microseconds across both
versions; this short check is not a performance qualification. Independent
reviewers also ran fresh focused checked QEMU controls and layout/DCE/fingerprint
checks. An initial checked-encoder QEMU source-audit run from the repository root
failed to find asm.go; the same fresh binary passed from its package directory.
The initial failure and corrected-run evidence were retained.

Focused commands from the repository root:

```sh
go test ./src/core/encoder/riscv64 ./src/core/compiler/backend/railshot/riscv64
go test -tags=wago_regalloccheck ./src/core/encoder/riscv64 \
  ./src/core/compiler/backend/railshot/riscv64
scripts/check-riscv64-diagnostics.sh
```

QEMU encoder binaries that run the source-inventory audit must execute from the
encoder package directory, matching `go test`'s working directory. QEMU results
are correctness evidence only. Native RISE access/cache/performance qualification
remains unverified; no native run, push, PR, remote CI or permission change was
performed. The staged plan remains in force; indirect/table/multivalue, FP/host
and later optimization boundaries remain separate review slices.
