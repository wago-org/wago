# Dragline ARM64 SHA-256 hardware research

Research snapshot: 2026-09-01. Sources are Arm, Apple, the Linux kernel, and
the Go project. This note concerns native AArch64 emission; compatibility
targets must continue to use baseline instructions.

## Conclusion

ARM's SHA-256 extension is a practical high-leverage target for the current
scalar SHA corpus. `SHA256H` and `SHA256H2` advance the two halves of the
eight-word hash state by four rounds, while `SHA256SU0` and `SHA256SU1` form
the corresponding four-word message-schedule group. Go's production ARM64
SHA-256 block routine demonstrates the required ordering: preserve old
`abcd`, execute `SHA256H` for new `abcd`, execute `SHA256H2` with old `abcd`
for new `efgh`, then carry new `abcd` into the next group. It interleaves
`SU0`/`SU1` with round-constant vector adds and processes one 64-byte block
without a scalar 64-round loop ([Arm A64 ISA overview, cryptographic
instructions](https://developer.arm.com/-/media/Files/pdf/graphics-and-multimedia/ARMv8_InstructionSetOverview.pdf),
[Go's ARM64 SHA-256 block routine](https://go.googlesource.com/go/+/refs/tags/go1.26.5/src/crypto/internal/fips140/sha256/sha256block_arm64.s)).

The feature gate is **FEAT_SHA256**. It is optional from Armv8.0, implements
the `SHA256*` instructions, is identified by `ID_AA64ISAR0_EL1.SHA2`, and
implies FEAT_SHA1 and FEAT_Crypto ([Arm Architecture Reference Manual,
FEAT_SHA256](https://developer.arm.com/documentation/ddi0487/maa/-Part-A-Arm-Architecture-Introduction-and-Overview/-Chapter-A2-A-profile-Architecture-Extensions/-A2-2-Armv8-A-architecture-extensions/-A2-2-1-The-Armv8-0-architecture-extension)).
It is not part of baseline AArch64 and must never be emitted merely because
`GOARCH=arm64`.

## Instruction contract and encodings

All four instructions operate on 128-bit Advanced SIMD registers with four
32-bit lanes. Arm's instruction pages gate each form on FEAT_SHA256 and require
FP/Advanced SIMD access to be enabled. The useful encoder forms are:

| Instruction | Practical contract | A64 word |
| --- | --- | --- |
| `SHA256H Vd.4S, Vn.4S, Vm.4S` | Four rounds producing the updated `abcd` half | `0x5e004000 \| Vm<<16 \| Vn<<5 \| Vd` |
| `SHA256H2 Vd.4S, Vn.4S, Vm.4S` | The same four rounds producing the updated `efgh` half | `0x5e005000 \| Vm<<16 \| Vn<<5 \| Vd` |
| `SHA256SU0 Vd.4S, Vn.4S` | First half of the next four schedule words | `0x5e282800 \| Vn<<5 \| Vd` |
| `SHA256SU1 Vd.4S, Vn.4S, Vm.4S` | Completes the next four schedule words | `0x5e006000 \| Vm<<16 \| Vn<<5 \| Vd` |

These fixed words and register fields come directly from Arm's encoding
diagrams and match the Go assembler's AArch64 encoding table
([Arm A-profile A64 ISA manual, SHA256H-H2-SU0-SU1 pp. 1735-1739](https://documentation-service.arm.com/static/67e40f3398aa3c3b6eea6a85),
[Go ARM64 assembler encoding source](https://go.googlesource.com/go/+/refs/tags/go1.26.5/src/cmd/internal/obj/arm64/asm7.go#6142)).
Useful independent check words are `SHA256H q0,q1,v2 = 0x5e024020`,
`SHA256H2 q3,q4,v5 = 0x5e055083`, `SHA256SU0 v6,v7 = 0x5e2828e6`,
and `SHA256SU1 v8,v9,v10 = 0x5e0a6128`.
The operand aliasing detail matters more than the raw encoding: `SHA256H2`
must see the pre-`SHA256H` `abcd` state. Go explicitly saves that state in a
third vector before the pair, which is the safest template for a specialized
Dragline emitter.

More precisely, Arm defines H as `Vd = SHA256hash(old Vd, Vn, Vm, true)` and
H2 as `Vd = SHA256hash(Vn, old Vd, Vm, false)`; the shared operation iterates
over four lanes, hence four rounds. SU0 applies the schedule's small sigma-zero
(`ROR 7 XOR ROR 18 XOR LSR 3`) contribution, and SU1 applies small sigma-one
(`ROR 17 XOR ROR 19 XOR LSR 10`) while completing four new words
([Arm A-profile A64 ISA manual, instruction and shared pseudocode pp. 1735-1739,
5828](https://documentation-service.arm.com/static/67e40f3398aa3c3b6eea6a85)).

The first 16 schedule words are big-endian message words. Go loads four
16-byte vectors and applies `REV32` to each before the SHA operations; it adds
four round constants to a schedule vector before each H/H2 pair. A correct
specialization must retain both transformations and add the original state
back after all 64 rounds ([Go's ARM64 SHA-256 block routine](https://go.googlesource.com/go/+/refs/tags/go1.26.5/src/crypto/internal/fips140/sha256/sha256block_arm64.s)).

## Reliable feature detection

### Linux/arm64

Use the kernel's `AT_HWCAP` contract, specifically `HWCAP_SHA2`; do not probe
by executing an instruction and catching `SIGILL`. The kernel documentation
says userspace should test the HWCAP before using a feature and that other
probing methods are not reliable. `HWCAP_SHA2` represents
`ID_AA64ISAR0_EL1.SHA2 == 0b0001` functionality ([Linux ARM64 ELF
hwcaps](https://www.kernel.org/doc/html/latest/arch/arm64/elf_hwcaps.html)).

`golang.org/x/sys/cpu.ARM64.HasSHA2` is the appropriate Go-level API. The
repo-pinned `x/sys v0.30.0` maps Linux HWCAP bit 6 to `HasSHA2` and has
fallbacks for an unavailable auxiliary vector ([x/sys v0.30.0 Linux ARM64
detector](https://go.googlesource.com/sys/+/refs/tags/v0.30.0/cpu/cpu_linux_arm64.go),
[x/sys ARM64 feature API](https://pkg.go.dev/golang.org/x/sys/cpu#pkg-variables)).

### Darwin/arm64

Apple documents `sysctlbyname` as the API for interrogating instruction-set
characteristics ([Apple, Determining Instruction Set
Characteristics](https://developer.apple.com/documentation/kernel/1387446-sysctlbyname/determining_instruction_set_characteristics)).
On the current test host (Apple ARM64, macOS 26.5.1),
`hw.optional.arm.FEAT_SHA256` exists and returns `1`.

That sysctl alone is not a complete deployment policy. The Go standard
library notes that macOS 11 did not expose sysctl keys for SHA2 and therefore
assumes AES, PMULL, SHA1, and SHA2 on every Apple Silicon Mac because M1 had
them and future Apple Silicon retains that floor ([Go Darwin ARM64 CPU
detection](https://go.googlesource.com/go/+/refs/tags/go1.26.5/src/internal/cpu/cpu_arm64_darwin.go)).
Current x/sys follows the same policy and also records the modern
`hw.optional.arm.FEAT_SHA256` name ([current x/sys Darwin ARM64
detector](https://go.googlesource.com/sys/+/refs/heads/master/cpu/cpu_darwin_arm64.go)).

The important repo-specific caveat is that Wago pins `x/sys v0.30.0`. That
version's ARM64 initialization performs real detection on Linux, NetBSD, and
OpenBSD, but Darwin falls through to minimal FP/ASIMD features; consequently
`cpu.ARM64.HasSHA2` is false on this Mac despite hardware support
([x/sys v0.30.0 ARM64 initialization](https://go.googlesource.com/sys/+/refs/tags/v0.30.0/cpu/cpu_arm64.go)).
Therefore a Wago implementation must either:

1. add a Darwin/arm64 helper that returns true, following Go's Apple Silicon
   minimum-feature policy; or
2. upgrade x/sys to a release containing Darwin ARM64 feature detection, after
   checking the repo's Go 1.22 compatibility requirement.

The first option is the smaller compatible change. TinyGo should remain
conservative unless it gains an equally reliable platform-specific check.

## Dragline integration seam

The existing MOPS path is the right model:

1. Append `TargetFeatureARM64SHA2` after the current final feature bit in
   `src/core/compiler/compiler.go`; never renumber existing serialized target
   bits.
2. Set it in `applyHostTargetFeatures` from the platform-safe SHA2 helper.
   Compatibility targets and TinyGo remain clear.
3. Gate the specialized emission from `input.Target.HasFeature(...)`. The
   target fingerprint already participates in function-artifact identity, so
   native and compatibility artifacts cannot collide.
4. Record the SHA2 bit in each specialized function artifact's `RequiredISA`,
   just as MOPS does. Also extend the compiled-code codec requirement and load
   check; target fingerprinting protects the function cache, but serialized
   compiled code still needs explicit cross-host rejection before execution.
5. Add the four encoder methods beside the existing NEON methods in
   `src/core/encoder/arm64/asm2.go`, with exact-word tests against known
   assembler output.
6. Keep the optimization behind a verifier for the exact SHA-256 dataflow.
   The safe first slice is the known corpus block routine: prove the 64-byte
   block loop, state initialization/add-back, endian conversion, constants,
   schedule recurrence, bounds/trap behavior, and output stores before
   replacing it.

`emitARM64` already receives the full compiler target and dispatches both the
RailMach and structured paths. Passing a SHA2 capability alongside the current
MOPS decision is the narrowest emission seam. A fixed-register specialized
block is feasible because the encoder already exposes V0-V31 through `Reg`
and has the required Q loads/stores, `REV32`, vector moves, and vector adds;
the SHA methods are the missing primitives.

## Recommended implementation order

1. Land detection, target identity, codec rejection, encoder methods, and unit
   tests without changing generated corpus code.
2. Implement an exact SHA block recognizer and differential tests over empty,
   partial, one-block, multi-block, and boundary-length inputs.
3. Verify native versus compatibility artifact separation and reject a
   SHA2-tagged serialized artifact when the host feature is absent.
4. Benchmark native SHA execution and code size against the retained scalar
   Dragline binary and Cranelift; keep the specialization only with a stable
   paired win.
