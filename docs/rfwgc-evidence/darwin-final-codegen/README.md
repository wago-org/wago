# Remaining Darwin ARM64 codegen gate repair

Qualified source: **d6f4e94fe671542d7e22fe84e8fe1f9358661c3a**.
Slices: `000e3e321` selects explicit established diagnostic routes;
`d6f4e94fe` repairs guarded-router repatching and its test gates.
All results below use an immutable Git archive, not the concurrently edited
worktree. **Actual native Darwin rerun remains with the parent.**

## Precise causes and changes

The original native run 41431926958 at da19, audited at `58fbb7d4f`, failed all
four public compact/pair specialization assertions and the public trap assertion.
`native-original-excerpts.json` contains selected raw events read directly from
the retained ordinary artifact, with archive/member hashes. The original archives,
prior darwin-arm64-repairs/recheck reports and failed attempts are unchanged.

The decisive public-route predicate is `api.go:nativeUnwind`: numeric imported
callers on Linux/Darwin ARM64 receive candidate unwind-only GC frame plans even
when the module has no collector objects. Feature configuration does not remove
this obligation. `call.go:nativeImportVariantEligible` rejects every candidate
plan. For the fixture, `used=3` and `remainingPins=6`; pin pressure is not the
rejection. The other checks (bounded import-only loop, dynamic bindings, X24/X25
availability, identical scalar imports, reg ABI, environment selectors, no memory,
tables, tags, globals or custom instructions) remain intact.

The two exact public tests retain result arithmetic, all six input sizes, Go
callback counts, native admission, four compact/pair combinations and repeated
TrapDivZero checks. They additionally require return maps and selector-invariant
code images. Positive second-body coverage is preserved at the backend's valid
no-candidate-plan boundary in `TestNativeImportVariantRoutesARM64`: both selector
values, compact on/off, one/two-argument aliases, ordinary/trapping bodies,
native-tagged register calls and Go-host fallback execute. Candidate zero-root
plans are an explicit negative case and must retain four exact return maps.
The same compiled body executes each tag alternative, with arithmetic, callback
count, trap and byte oracles. No public API or production eligibility was relaxed.

That positive backend test exposed a **separate production bug**. The router is
first patched to its ordinary +4 fallback. `Asm.PatchBranch19` ORs an immediate
into the instruction; a second patch retained the fallback's low displacement
bit. In the compact single-argument fixture, router `0x28` should target `0x440`
(STP FP,LR), but instead targeted `0x444` (MOV FP,SP), skipping the frame record
and crashing on return. Noncompact displacement parity hid the bug. The seven-line
repair clears imm19 before the second patch and restores the fallback word if
range checking fails. The new test checks the CBNZ X17 encoding and its exact
STP landing instruction before native execution. Restoring only baseline
`compile.go` makes this committed oracle fail at `0x444`, without a crash.
The default public route's exclusion remains in force; this does not claim that
the old public assertion failure was itself an executed miscompile.

Default diagnostic failures were route-specific expectations: shared-scalar
admission bypasses the established allocator/folder, changes wrapper homes and
zero initialization, and does not emit the old peephole counters. Ten named
tests now select the established route with a test-scoped helper and restore the
prior switch through cleanup. All numeric counter, pin-mask, byte-delta and
execution expectations are unchanged. There is no package-wide disabling or
production fallback. Shared/established pair tests and the shared leaf byte/frame
oracle run alongside these diagnostics, including the independent V8 comparison.

## Qualification census

Counts include parent and subtest events, not independent workloads. All final
execution rows have **zero failures and zero skips**, in each mode.

| Exact bounded lane | ordinary | wagodebug | wago_regalloccheck |
| --- | ---: | ---: | ---: |
| Focused backend execution | 53 PASS | 53 PASS | 53 PASS |
| Target diagnostics + shared/established pairs | 62 PASS | 62 PASS | 62 PASS |
| Four public native-variant tests | 12 PASS | 12 PASS | 12 PASS |
| Prior synthetic worker comparisons | 8 PASS | 8 PASS | 8 PASS |
| Darwin backend/public/diagnostic cross-builds | 3 PASS | 3 PASS | 3 PASS |

Focused includes the new variant matrix, prior dynamic-import join and shortcut
policy, table lowering/helper ABI, comparator, and const-fold/same-operand
execution tests. Worker comparisons cover the same synthetic slice as the prior
repair: tiny, fib_rec, dispatch, many_funcs, memory_tree, plus shared adapters.
It does not include AssemblyScript. Public selectors are exactly SourceArtifactAndCalls,
Trap, RespectsPolicies and ForeignFallback; no cancellation, conversion, H2,
runtime, Core 3 or broad conformance gate is claimed here.

`run.py`, `retry.py`, `supplement.py` and their command receipts preserve argv,
cwd, build tags, explicit environments and process exits. The three modes use
Go 1.27.1, Linux/ARM64 QEMU 10.0.13 and CGO disabled. `tools.json` hashes those
executables plus the explicit Node and wabt tools. `source-manifest.json.gz`
records all **18,604** exported blobs verified against their Git object IDs;
submodule contents were not exported or required. The nine Darwin builds are
compile-only, never represented as native execution. Negative control is ordinary
mode only; its build succeeds and test exits 1 with the exact guard-target error.

The first matrix's diagnostics/public executions failed only at untrusted mise
shims for Node/wat2wasm. Their original logs/commands remain. `retry.py` reruns
those six lanes using the same hashed binaries and explicit tool paths; these
are the final qualifying logs. An initial archive command used a nonexistent cwd
and was rejected before execution; it was retried from the worktree. Prototype
logs are retained under `attempts/`: first harness masks were sized incorrectly,
the first native stub lacked its wrapper entry, and compact execution revealed
the repatching crash. Probe 2 had corrected masks and both native ABI entries,
and still crashed. Probe 4 logged the intended offsets; `compact.bin` is that
pre-fix code image. Probe 5 passes after the repair. These dirty-source probes
are investigative evidence, not immutable qualification. Baseline diagnostics
at 58fbb7d4 reproduce the original route failures; no failed attempt was erased. The first offline verifier needed `git ls-tree
--full-tree` because its cwd is the evidence subdirectory; that failed check is
also recorded under `attempts/`.

## Cross-check and handoff

Read-only sibling audit inspected Slipstream host resource/dispatch code and
Starshine's external optimizer runtime executor, recording heads, statuses,
excerpts and file hashes in `sibling-audit.json`. Neither contains Wago's guarded
variant/router/PatchBranch19 implementation; no equivalent sibling defect was
verified. This is a scoped source audit, not sibling execution qualification.

The owned src/wago changes are confined to the two notified exact tests.
Concurrent CI, conversion/runtime, evidence, index and gitlink changes were
preserved. Source commits used explicit `git commit --only` path lists.
No push, PR, dispatch, subagent, Claude review, benchmark or timing comparison
was performed. Existing test logs include incidental elapsed fields.

Ready for a fresh independent review and parent-owned native Darwin rerun of the
exact selectors. This closes the bounded QEMU/codegen work, not native Darwin,
full backend/CI/conformance, performance, or Nov 4 release qualification.

**READY TO MEASURE:** branch `experiment/rfwgc-port`, source
`d6f4e94fe671542d7e22fe84e8fe1f9358661c3a`; performance-sensitive path is
`arm64/compile.go:compileFunc` guarded native-import router. Proposed workloads:
valid no-plan import-only scalar loops, compact on/off, one/two arguments, native
and Go fallback, plus public unwind-mapped callers as unaffected controls.
Diagnostic/public fixture changes cannot affect shipped runtime performance;
the production repatching fix is handed to the independent measurement owner.
