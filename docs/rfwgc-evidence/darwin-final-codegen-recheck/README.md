# Darwin final codegen independent recheck

Review source: `d6f4e94fe671542d7e22fe84e8fe1f9358661c3a`, whose parent is the
target-local diagnostics commit `000e3e321b80d22642aae86ca18b373b9ae63c17`.
This archive is an independent, bounded Linux/QEMU review. It does not approve
the overall port and gives no native Darwin execution credit.

## Verdict

No review blocker was found in the scoped router repair or its tests.

- **Router repatch: CLOSED.** `PatchBranch19` ORs an imm19 into the existing
  instruction. The router first contains the valid `+4` fallback (`0xb5000031`).
  Clearing only bits 23:5 before the second patch produces the intended compact
  route (`0xb5002211`, target `0x440`) instead of the stale-bit route (`0x444`).
  Opcode, X17 register, condition bits and all non-immediate bits are preserved.
- **Sign/range/alignment and repeated patch behavior: CLOSED for the actual
  route contract.** `_probes/branch19_repatch.go` independently clears and
  repatches positive and negative aligned offsets, decodes the signed imm19,
  preserves CBNZ register/opcode and B.cond condition/opcode, and proves an
  out-of-range failure leaves bytes unchanged. Production targets are code
  offsets and therefore instruction-aligned; this review does not widen the
  encoder API contract to arbitrary unaligned callers.
- **Failure fallback: CLOSED.** The new failure branch restores the exact saved
  fallback word. The source diff changes no eligibility predicate. The returned
  `code` remains the ordinary body on failure, including when append capacity is
  shared, because restoration occurs before return.
- **Guard oracle and execution: CLOSED under Linux ARM64 QEMU.** All compact /
  noncompact, scalar / pair and normal / trap combinations pass. The exact word
  oracle requires CBNZ X17 and the decoded target's first word to be
  `0xa9bf7bfd` (`STP FP,LR`); execution covers Go-host fallback and native-leaf
  routing. The reverted production implementation fails the same oracle at
  `0x444`, which is the required bounded negative control.
- **Eligibility and public fixtures: CLOSED.** Candidate exact unwind plans
  retain four callsites and reject the second body. Public variants require a
  nonempty unwind-callsite map and byte-identical selector-on/off images.
  The positive guarded route lives at the valid backend no-plan boundary and
  requires a larger enabled image. Candidate controls and policy/fallback tests
  remain asserted; expectations were not weakened to admit production behavior.
- **Target-local diagnostics: CLOSED.** `establishedScalarRoute` is scoped with
  `t.Cleanup` to exact byte/count tests only. Shared-route pair tests remain in
  the diagnostics selector. The helper does not alter package initialization,
  production routing, or unrelated tests.
- **Prior join/helper/comparator regressions: CLOSED for this focused replay.**
  The focused selector retains the dynamic-import unwind join, native-leaf GC
  policy, table lowering/helper, bulk argument, worker comparator, const-fold
  execution and same-operand comparator cases.
- **Native Darwin runtime: OPEN and separate.** No native Darwin command was
  run. This review cannot close native cancellation, ownership, or runtime gates.

## Fresh execution

Each of ordinary, `wagodebug`, and `wago_regalloccheck` passed three lanes:

| Lane | Per-mode top-level result | Coverage |
| --- | ---: | --- |
| focused backend | 14 PASS, 0 FAIL, 0 SKIP | router execution plus prior ARM64 regressions |
| exact diagnostics | 12 PASS, 0 FAIL, 0 SKIP | established counts/bytes and shared-route pairs |
| public fixtures | 4 PASS, 0 FAIL, 0 SKIP | unwind exclusion, policies, fallback and trap |

Builds and executions all exited zero. `negative-build` exited zero and
`negative` exited one as required, reporting: `native guard target 0x444 does
not start with STP FP,LR`. The negative overlay uses the exact pre-fix compile.go
blob (`5c1706da3533e34199b27e9fee055da43db01380`) shared by `58fbb7d4`,
`000e3e321`, and `d6f4e94^` while retaining the new oracle.

Tool identities are recorded as commands and logs: Go 1.27.1 at the explicit
GOROOT, QEMU AArch64 10.0.13, Node 24.22.2 and WABT 1.0.42. Every test command
uses the explicit Node/WABT PATH. `source-integrity.json` records the immutable
Git archive/tree; binaries remain outside the repository and their hashes are in
`binary-hashes.json`.

The read-only Slipstream and Starshine audit found no equivalent guarded ARM64
router/imm19 repatch implementation. It is a scoped source search, not runtime
qualification. No benchmark, source/workflow edit, push, PR, dispatch, Claude
review, native Darwin run, or subagent was used.
