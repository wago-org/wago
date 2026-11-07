# PR 1041 expansion round — 2026-10-31

PR: https://github.com/wago-org/wago/pull/1041
Branch: `fix/rv64-typecap-rebased`.
Base: `ce5a4975d184a970648efb95f9019e7bddaa6beb`, clean on entry.
Implementation head: `2f9d1cb04fcdc92c44114df1376aab2961d98903`.

Scope authority: `/data/workspace/brief-pr1041-expansion.txt` and
`audit/rv64-function-cap:docs/audit-rv64-function-cap-2026-10-31.md`.
The audit was read before implementation. The existing rebased type fixes were
not rewritten, and no rebase, push, or PR comment was performed.

## Result and checkpoints

The two proven module blockers were addressed first. Before the change, valid
1,025-function probes failed with `SWAR function directory bound` and
`native table bridge requires bounded signature and source index`. Both pass.
Icepack now validates, passes source admission, and compiles through public RV64:
552,512 Wasm bytes, 2,411 definitions, 14 function imports, 2,411 defined entry
points, and 10,820,848 native code bytes. Compilation deferred executable mapping;
this does not establish application execution correctness.

Checkpoints, in order:

1. `ac67a41e` — table bridge caller ordinal and signature-key guards, SWAR
   function directory, initial module-route regressions and directory constants.
2. `5c71f975` — compiler and runtime population/import/table/export mirrors,
   metadata accounting, bounded admission-sized reference scratch, call-site
   accounting, and regression updates.
3. `69c26809` — keeps the native large-rooted execution probe numeric. Collector
   callable-mask proof is distinct from a numeric function's entry/frame proof.
4. `2f9d1cb0` — removes the additional 256-import restriction in plugin admission,
   with a 257-plugin-import regression.
5. The documentation commit containing this report is the final branch head.
   Its parent is the implementation head above. Resolve its exact hash using
   `git log -1 --format=%H -- docs/pr1041-expansion-round-2026-10-31.md`;
   the handoff also names that hash. A commit cannot embed its own hash.

## Bounds and allocation discipline

The public signed-32-bit byte extent stays **2,147,483,647 bytes**. New independent
minimum directory charges yield these population bounds; configured source,
compiled-metadata, native-code, instruction-work and private-profile byte budgets
can bind much earlier:

- Functions and function imports: `/16` = **134,217,727**.
- Tables: `/32` = **67,108,863**.
- Exports and exception tags: `/16` = **134,217,727**.
- Signature keys in a native table bridge: type declaration bound, additionally
  limited to `(MaxCodeBytes - 8192)/64` = **262,016** before assembler expansion.
  This reserves 8 KiB of fixed bridge code and at most 64 bytes per key.
- Function call records: `MaxModuleBytes / sizeof(directCall)` = **26,214** on
  the 64-bit targets. Existing 16,384 instruction/label work accounting remains;
  it can reject a body before that record extent is reached.

Function totals use the directory budget before public expansion; the public
check uses subtraction before adding imported and defined populations. Public
metadata preflight also charges table and export directories at 32 and 16 bytes
per entry respectively. Native bridge source ordinals stay well below the
`uint32` index+1 sentinel boundary. No slot, mask, table32 header, relocation,
public byte budget, or type encoding changed.

Closed-reference scratch is now `len(imports)+len(definitions)` booleans and
`len(function imports)` signature pointers, after namespace admission. A separate
1 MiB scratch budget checks the boolean and pointer charges by division before
allocation; the existing source/state byte budget remains independent. No raw
wire count directly sizes these slices. The previously landed malformed
14-byte rooted-wire regression still passes with zero allocations.

SWAR validates immutable function/type/global directory contents once per module
before reusing that proof during body lowering and emission. Integer reference
fallback does the same for its immutable function directory. Standalone walkers
still validate supplied directories. This removes the repeated whole-directory
signature validation identified by the audit; it is not a compile-time
performance measurement or a claim that all compiler work is linear.

## Per-site ceiling changes

Paths in the compiler table are under
`src/core/compiler/backend/railshot/riscv64/`. A new population ceiling below
always remains subject to the actual byte/work limits described above.

| Site / resource | Before | After |
| --- | --- | --- |
| `native_table_thunk.go`: caller source index | `<1,024` | `<134,217,727` |
| `native_table_thunk.go`: signature keys | 1,024 | type budget plus 262,016-key code projection |
| `swar_function.go`: unconditional function directory | 1,024 total | 134,217,727 total |
| `swar_function.go`: imported prefix | 256 | 134,217,727 |
| `module.go`: private defined-function admission | 1,024 | 134,217,727; public scalar remains byte-budgeted |
| `swar_module.go`: definitions and imported+defined directory | 1,024 in restricted profiles | 134,217,727; public directories also preflighted |
| `reference_function_module.go`: definitions / exports | 1,024 / 1,024 | 134,217,727 / 134,217,727 |
| `reference_function_module.go`: boolean / import signature scratch | fixed 1,024 / fixed 256 | admitted actual lengths; combined scratch charge <=1 MiB |
| `reference_function_state.go`: total functions / function imports / all-kind imports | 1,024 / 256 / 613 | 134,217,727 each; numeric global/memory restrictions retained |
| `gc_rooted_scalar.go`: defined and total functions / imports | 1,024 / 256 | 134,217,727 / 134,217,727 |
| `gc_rooted_scalar.go`: table count / exports | 1,024 / 1,024 | 67,108,863 / 134,217,727 |
| `gc_immediate_references.go`: definitions | 1,024 | 134,217,727 |
| `exception_source.go`: definitions / tags | 1,024 / 1,024 | 134,217,727 / 134,217,727 |
| `memory.go`: total functions / imports / table export aliases | 1,024 / 256 / 1,024 | 134,217,727 each |
| `integer.go`: reference function directory / imports | 1,024 / 256 | 134,217,727 each |
| `host_import.go`: public function imports | 256 | 134,217,727 |
| `native_import_thunk.go`: import ordinal | `<256` | `<134,217,727` |
| `plugin_machine.go`: custom instruction entries / function imports | 256 / 256 | 134,217,727 each, with import identity/carrier checks unchanged |
| `swar_host_state.go`: host signatures / total function population | 256 / 1,024 | 134,217,727 each; real thunk/image bytes still bind |
| `swar_table_state.go`: image functions | 1,024 | 134,217,727; private table storage/growth rules retained |
| `tail_import_source.go`: source definitions / imports / exports | 1,024 / 256 / 1,024 | 134,217,727 each; owner/factory bounds unchanged |
| `tail_dynamic_source.go`: source definitions / imports | 1,024 / 256 | 134,217,727 each; owner/factory bounds unchanged |
| `public_resource_budget.go`: private all-kind import count selection | 613 | public `/16` directory budget, 134,217,727 |
| `table.go`, `production_emission.go`: public table directories | 1,024 | 67,108,863 |
| `integer_initializers.go`: tables / reference-bearing export directories | 1,024 / 1,024 | 67,108,863 / 134,217,727 |
| `public_primitive_references.go`: aggregate tables | 1,024 | 67,108,863 |
| `local_mutable_reference.go`: exports | 1,024 | 134,217,727 |
| `swar_module.go`: public tables / export directories | 1,024 / 1,024 | 67,108,863 / 134,217,727 |
| `compile_production_module.go`, `swar_module.go`, `reference_function_module.go`: custom count | 1,024 | aggregate metadata bytes, including 16 bytes per custom record plus name/payload |
| `decoded_metadata_budget.go`: names and local/label group counts | 1,024 | existing per-row/group byte charges and string bytes; no arbitrary count ceiling |
| `branch_hints.go`: function groups | 1,024 | 32 bytes per group and 8 per hint against remaining metadata; single raw section, ordering, target validation and existing hint-byte bounds retained |
| `integer.go`, `swar_function.go`: all direct/indirect/ref/tail/helper call count guards | 1,024 per body | 26,214 call-record extent plus unchanged instruction/code/transport budgets |
| `tail_ra_owner.go`: sites per caller function | 1,024 | same call-record extent; owner, aggregate site and caller-group limits retained |

Runtime paths below are under `src/wago/`.

| Runtime mirror | Before | After |
| --- | --- | --- |
| `railshot_admission_riscv64.go`: functions, function-key directory, imports, function/global/memory exports | 1,024; imports 256 | function/import/export-specific 134,217,727 bounds |
| `gc_frame_roots_linux_riscv64.go`: total functions / imports / tags | 1,024 / 256 / 1,024 | 134,217,727 each |
| `rv64_execution_contract_riscv64.go`: rooted frame entry directory | 1,024 | 134,217,727 |
| `reference_functions_riscv64.go`: callable mask/cache population and closed function/import/export admission | 1,024; imports 256 | function/import/export-specific 134,217,727 bounds |
| `compiled_data.go`: rooted import receipt / aggregate global and memory export receipts | 256 / 1,024 | 134,217,727 each |
| `rv64_execution_contract.go`: table directory mirror | 1,024 | 67,108,863; compile-time equality assertions against compiler constants |
| `compile_table64_generic_riscv64.go`, `table_capacity_riscv64.go`: table count and capacity-directory lengths | 1,024 | 67,108,863 |
| `reference_table_directory_riscv64.go`, `railshot_tables_riscv64.go`: table export count | 1,024 | 134,217,727 |
| `native_tail_metadata_riscv64.go`, `native_table_targets_riscv64.go`: tables per compiled owner | 1,024 | 67,108,863; owner graph/lease limits unchanged |
| `reference_function_state_riscv64.go`: combined function/global/memory exports | 1,024 | 134,217,727 |
| `exception_handling_riscv64.go`: tags | 1,024 | 134,217,727 |
| `native_import_riscv64.go`, `native_table_entry_riscv64.go`: general import signature/key directories | 256 | 134,217,727; tail-owner-specific import limits unchanged |

## Regression evidence

- Scalar, SIMD/SWAR, late-caller indirect table call, rooted source, and public
  local-tail modules validate and compile at 1,024, 1,025 and 4,096 definitions.
  The 4,096-function public scalar case remains the passing control.
- Additional generic rooted scalar and SWAR rooted compiler routes accept 1,025
  functions; private scalar compilation accepts 1,025 as well.
- Scalar and SWAR public modules with 257 function imports and 1,025 definitions
  compile. Closed-reference 256/257 import fixtures now both pass. A 257-entry
  plugin import directory also passes exact instruction admission.
- Named/custom/hinted/exported 1,025-function decoded variants compile. Empty
  custom records exhaust their metadata-byte budget rather than bypassing it.
  Existing malformed metadata, ordering, raw/decoded and byte-bound tests remain.
- A valid one-function Wasm module with 1,025 direct call instructions first
  reproduced `direct call count/slot budget`, then passed after the byte-derived
  change. Dead calls still hit the preserved instruction/record work bounds.
- Configured native-code and compiled-metadata quotas reject enlarged modules
  before staging callbacks. Existing slot/result/frame/arity checks remain.
- The new native `TestRV64CeilingRootedInstance` compiles 1,025 functions,
  authenticates rooted entry/frame metadata, instantiates, and invokes the last
  numeric entry under GC stress **when executed on RV64**. On this amd64 host it
  was cross-compiled, not run. Native cold-artifact/collector-callable-mask and
  owner lease lifecycle execution therefore remain unverified in this round.

## Gates and environment

All final gates used **Go 1.27.3 linux/amd64**, selected explicitly with
`MISE_GO_VERSION=1.27.3` and mise shims on PATH (the repository mise config
otherwise selects 1.27.1). The initial two-failure reproduction used that repo
Go 1.27.1 selection; it is not counted as a final gate.
Dedicated `GOCACHE` and `GOTMPDIR` directories were placed on `/data` rather than
the tight root filesystem. Later gate commands used `GOMAXPROCS=2`, and build,
vet and cross-compilation used `-p=2` to limit concurrent host load.
Counts below are terminal test/subtest events, excluding package terminal events.

| Gate | Command / scope | Verdict |
| --- | --- | --- |
| Focused ceiling/type/P1 | `go test -json -count=1 ./src/core/compiler/backend/railshot/riscv64 -run 'Ceiling\|RV64Type\|RootedScalarTypeWireRejectsCountBeyondPayloadWithoutAllocation'` | PASS: 41 passed, 0 failed, 0 skipped; 3.363 s |
| Full RV64 package | `go test -json -count=1 ./src/core/compiler/backend/railshot/riscv64` | PASS: 4,084 passed, 0 failed, 117 skipped; 10.045 s |
| Runtime package | `go test -json -count=1 ./src/wago` | PASS after corpus initialization: 10,762 passed, 0 failed, 563 skipped; 58.089 s |
| Repository build | `go build -p=2 ./...` | PASS: exit 0 |
| Host vet | `go vet -p=2 ./src/core/compiler/backend/railshot/riscv64 ./src/wago` | PASS: exit 0, 0 diagnostics |
| Linux arm64 test cross-compilation | `GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go test -p=2 -c -o <artifact> <package>` for both packages | PASS: 2/2 test binaries, exit 0 |
| Linux riscv64 test cross-compilation | same command with `GOARCH=riscv64`, both packages | PASS: 2/2 test binaries, exit 0 |
| Linux arm64 vet | `GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go vet -p=2 <both packages>` | FAIL: 2 diagnostics in unchanged source, listed below |
| Linux riscv64 vet | same vet command with `GOARCH=riscv64` | FAIL: 13 diagnostics in unchanged source, listed below |

The first runtime run failed: 10,760 passed, 17 failed, 548 skipped. Every failed
case reported that `tests/conformance/spec-v3/test/core` was missing. The pinned
submodule was uninitialized in this worktree and the primary checkout. Running
`git submodule update --init --depth 1 -- tests/conformance/spec-v3` checked out
the existing gitlink `9d36019973201a19f9c9ebb0f10828b2fe2374aa`; the full retry
then passed. No tracked dependency revision changed.

Cross-vet failures are preserved, not suppressed. Every diagnostic-bearing file
was compared byte-for-byte with `git show ce5a4975d:<path>` and was identical.
This is unchanged-file evidence, not a claim that a complete baseline vet run
was performed. No unrelated warning was fixed or analyzer disabled.

Arm64:

```text
src/wago/gc_native_reservation_arm64_test.go:37:51: possible misuse of unsafe.Pointer
src/wago/gc_native_reservation_arm64_test.go:73:54: possible misuse of unsafe.Pointer
```

Riscv64:

```text
src/core/compiler/backend/railshot/riscv64/native_import_gc_linux_riscv64.go:83:30: possible misuse of unsafe.Pointer
src/wago/public_host_riscv64_test.go:57:4: self-assignment of c
src/wago/public_host_riscv64_test.go:349:5: self-assignment of c
src/wago/public_owned_host_riscv64_test.go:35:4: self-assignment of c
src/wago/gc_function_storage_fixtures_riscv64_test.go:198:19: assignment copies lock value to cache
src/wago/gc_function_storage_fixtures_riscv64_test.go:198:33: assignment copies lock value to memo
src/wago/gc_rooted_callable_riscv64_test.go:49:19: assignment copies lock value to cache
src/wago/gc_rooted_callable_riscv64_test.go:49:41: assignment copies lock value to memo
src/wago/gc_rooted_callable_riscv64_test.go:129:10: assignment copies lock value to memo
src/wago/gc_rooted_callable_riscv64_test.go:200:18: assignment copies lock value to cache
src/wago/gc_rooted_callable_riscv64_test.go:200:40: assignment copies lock value to memo
src/wago/exception_owned_riscv64.go:125:36: possible misuse of unsafe.Pointer
src/wago/exception_owned_riscv64.go:279:33: possible misuse of unsafe.Pointer
```

Full diagnostics (including copied lock types), final JSON, the missing-corpus
run, and before-fix probe logs are retained locally in
`/data/workspace/pr1041-expansion-evidence/`. Dedicated build cache, compiler
scratch, generated test executables and the standalone Icepack probe source
are removed after gate completion. The pinned corpus checkout is retained.

## Audit rows intentionally remaining

This round does not claim to lift every policy row in the broader audit:

- **Tail-owner/factory population, helper-inclusive entries, proxy imports,
  binding groups, target directories, caller-owner groups, 4,096 sites, retained
  ranges/continuations and generation scopes** remain under their independent
  policies. No module-to-owner regression in this round established that these
  were the actual failing owner-construction gate. The public local-tail module
  probes pass without proving factory construction. The private imported-tail
  source probe at 1,025 definitions instead reaches its real 1 MiB decoder
  metadata allocation bound, which is preserved. Widening owner counts requires
  the audit's exact 40-byte descriptor / 32-byte target-row extents, source
  receipts, sparse masks, lease retention, cancellation/park/resume and factory
  byte proofs together. `MaxFunctions=1024` and `MaxHostFunctionImports=256` are
  retained for these legacy ownership/profile policies, not general module
  admission. Per-caller call-record accounting was updated; aggregate owner
  counts were not.
- **Owned table capacity, aggregate element payload, private 256 element
  segments and exact one-table recognizers** are unchanged. The table changes
  here concern module table-directory counts and export aliases. Storage/growth
  reservation and initializer/ownership semantics are separate audit work;
  blanket expansion could select unsupported private growth or exact-product
  shapes. Public table32 header/storage format constraints stay intact.
- **SWAR/closed-reference `br_table` label limits** are unchanged: those are
  control-flow dispatch labels, not module tables or function populations. The
  scalar compact-dispatch fallback is preserved.
- **Adjacent memory/global/data pilots, invocation argument owners, lease
  graph edges and uint16 writer-owner storage** are unchanged. The audit calls
  for distinct dynamic scratch, storage/growth and owner-format proofs.
- **Type-only/runtime signature-directory checks** were not expanded here:
  the brief excludes redoing type work. Existing `len(c.Types)` and rooted
  flattened-type checks using the legacy count remain where they were in the
  base; this round's enlarged function fixtures share small type directories.
- **Arity arrays, transport slots/result masks, source packet widths, relocation
  reach and all real byte/work quotas** remain out of scope and unchanged.

**Compile-time measurement debt remains attached to PR 1041.** No benchmark or
valid-large-module allocation study was completed. The successful Icepack
compile is correctness/admission evidence only. Native RV64 execution and the
two cross-vet failures remain explicit review/handoff limitations.
