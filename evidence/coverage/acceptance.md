# #815 acceptance and remaining scope

Checked against the live issue body retained in issue-815.md on 2026-10-09.
The issue asks for an explicit initial record subset, not a complete ISA decoder.
All five written criteria have bounded evidence; this is not a universal performance
or semantic-equivalence claim. The GitHub issue checkboxes were not edited.

| Literal issue acceptance criterion | Status | Supporting evidence |
|---|---|---|
| Identical opcodes with different constants, false destination dependencies, changed spill widths and harmless relocation changes | Completed for controlled initial subset | Paired distinct bytes; opcode-only baseline misses constants/XOR dependencies/stack-store widths/displacements; admitted matching-target relocation compares equal; targets/conditions/widths remain distinct |
| Both AMD64 and ARM64 records for explicit initial subset; unknown semantics remain unknown | Completed for portable record subset | AMD64 facts and 16 Jcc controls; ARM64 MOVZ/ADD/LDR/STR/B word controls; checked/profile ARM64 cross-build; identical unknowns and invalid relocations reject qualification |
| At least one current performance fixture agrees with human-readable disassembly and source attribution | Completed on native AMD64 | Existing 88-byte fib.wasm emits 105 native bytes; objdump/source ranges; independent i32.add attribution accounting for local-declaration bytes; selected XOR attribution and six public API oracles |
| Bound time/memory and report incomplete mappings/truncated regions | Completed within diagnostic budgets | Linear mapped/raw partition; 64 mapped / 128 raw regions; 4096 instructions;1 MiB JSON; 64 KiB native; 512 reports; unknowns/anchor/count changes incomplete; early limit rejection; allocations measured; caps are not compiler peak-memory limits |
| No additional state/codegen/allocation/dispatch in ordinary builds | Completed for production paths | No experiment production import/source modification against verified main e4bcc524; identical actual cli/wago ordinary runtime bytes/hash; diagnostic main is a separate opt-in executable; tests run in test builds |

## Completed practical integration

- Capture/compare/focused-test just recipes and documented standalone exit codes.
- Existing compiler profile/source/disassembly contracts reused; no profiler added.
- Captured actual binary/input/native identity and configured compiler path/features.
- All fib bytes retained: 51 mapped + 54 raw; verified nonoverlapping complete partition.
- Three raw ranges preserve real profile ownership without fabricated Wasm locations.
- Mapped operand changes and opaque byte changes separated in output.
- Unknowns, invalid metadata, mismatched anchors and omitted/truncated coverage explicit.
- Identical pairs decode once; normalized direct branches use fixed strings; record
  layout packs flags, reducing change-report bytes without retained caches.
- Independent source/measurement review, focused tests, vet and native public results.

## Deliberately unsupported initial scope

- Whole-program equivalence, automatic root cause, sampled hotness or speed inference.
- CALL/RET/PUSH/POP implicit operand semantics or relocation normalization in opaque
  wrappers/data. Those bytes are now compared, but remain semantically unqualified.
- SIMD/FP, partial GP writes, indexed memory, broader implicit effects, register
  renaming and broad constants normalization.
- Insertion-tolerant instruction alignment, renamed/reordered functions, changed
  owner/anchor matching or guessed nearest Wasm PCs.
- Independently authenticated source/profile ownership in imported JSON, saved
  compiled-artifact ingestion, full shared-driver/environment reconstruction.
- Arbitrary/untrusted large modules; peak/RSS/retained-memory qualification.

These are scope boundaries, not unfinished requirements for the issue's explicitly
permitted initial subset. A larger tool should revisit representation and budgets
against realistic high-volume workloads before widening them.

## Hardware-blocked qualification

Native ARM64 capture, emitted source/operand attribution and execution/timing remain
unavailable on this AMD64 host. ARM64 records are tested with word controls and
cross-built only. Native ARM64 is **not** silently counted as completed evidence.
The literal record-support criterion is satisfied; hardware qualification is an
additional guard before expanding architecture support beyond record comparison.

## Exact architectural decision for broader operand coverage

To classify the 54 raw fib bytes as operands, decide who supplies authoritative
code-versus-literal boundaries, stable entry/internal-call target identities and
call/return/stack implicit-effect rules. ProfileRegions alone is byte ownership,
not that contract. Options are extending established emitter/artifact observers or
adopting a verified target disassembler representation behind the same source/owner
schema. That requires review with the established profiling/artifact interfaces.
This change makes no such runtime/host-boundary decision: raw comparison is useful
without it, conservatively reports byte changes, and never normalizes opaque data.

## Qualification limitations

See results.md for full presubmit status, including failed/interrupted attempts.
Draft PR smoke is not full CI or native ARM64 qualification. Keep the PR draft and
retain the independent review/measurement records; no merge is authorized here.
