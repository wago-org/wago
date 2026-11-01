# Round 9 bounded lifecycle ownership probes

Date: 2026-11-01. Linux/amd64, Go 1.27.3. This is a state-based test report,
not crash reproduction, a broad audit, or a waiver of the earlier automatic
rejection. No source crash fixtures were copied or run.

## Immutable inputs and ownership

| Label | Archived source |
| --- | --- |
| main | `90f2b57dafa7f8c3cc7e3aca38b91148f15e1d1e` |
| port | `ca30843801956bae67d5cd1219f66d191518c125` |
| rooting (earlier committed checkpoint) | `4ad440e864a16424a3544f7c643948968e4b7fd2` |
| port-updated (user-directed current port pin) | `45c83820fbc10ee31eaaa8437e12464955b2fb9d` |

Each source tree was extracted with `git archive <commit> | tar -x -C
snapshots/<label>` inside this evidence directory. These are ordinary untracked,
ignored archives, not additional Git worktrees. The user subsequently updated
the immutable port pin to `45c83820f`; older results are retained distinctly. The rooting worker's full final matrix is a
separate in-flight gate, not supplied by these focused probes. Only the uniquely
named probe was added to each archive's `src/wago/`. The later rooting commit was assessed
separately; concurrent dirty production edits were never used for execution.

Read the project AGENTS.md, latest port-log owner orders, and
`r9-priority-source-assessment/README.md` before testing. No production edits,
subdelegation, branch change, PR, push, closure, debugger, arbitrary/freed-memory
inspection, collection pressure, or benchmarks. This directory's testdata and
runner do not affect shipped performance. Test command elapsed times printed by
Go are not performance measurements.

## Assertion matrix and results

| Finding / case | Main | Original port | Earlier rooting | Updated port |
| --- | --- | --- | --- | --- |
| H2-F1, anyref table/global provider, no type section | Consumer import rejected before publication | Same | Same | Same |
| H2-F1, anyref table/global provider, harmless function type | Consumer import rejected before publication | Same | Same | Same |
| H2-F1, extern-token table/global, both type variants | Known retained token survives consumer close | Same | Same | Same |
| H6-F3, nested function in struct, failed start | Ownership assertion fails; test stops | Same | Same | Same |
| H6-F3, nested function in one-element array, failed start | Ownership assertion fails; test stops | Same | Same | Same |

The final raw logs and `*-results.json` record ordinary, `wagodebug`, and
`wago_regalloccheck` runs for each pin. A failing exit is evidence of the H6
assertion, **not a green suite**. Admission skips are not lifecycle passes.
Only `^TestR9OwnershipState` was selected; no broad existing regression or source
reproducer was executed. The probe lives in `testdata/` so known failing audit
assertions are opt-in and do not change the repository's default test suite.

### H2-F1: bounded outcomes, overall unresolved

Every provider initially has a nil collector and live physical resources.
Each provider exports only a one-slot table or a mutable global, initialized to
null. The harmless-type variant adds only an unused `() -> ()` type. These
modules have no functions or managed allocations.

A consumer with an unused allocator requests a collector. Importing an anyref
container fails with `collector-reference table/global requires producer and
importer in the same Runtime GC domain`. Therefore these four cases never
publish a non-null managed reference. This is a concrete admission boundary,
not evidence that persistent root migration is implemented.

Externref cases are admitted. The global consumer uses a store-owned collector;
the table consumer uses a private collector in the observed host configurations.
Both instances have live registration/physical state before enumeration.
`rangeGCDomainPersistentRoots` reports zero compact slots before and after
consumer close. **Zero compact roots is not a failure for an extern token.**
The probe creates a normal store-issued externref containing the exact Go string
`r9-known-retained-value-42`, publishes it through the consumer setter, verifies
store resolution before and after consumer close, then attaches a fresh reader
and verifies the identical token in the provider's container. It clears the
slot before explicitly releasing the token. The provider remains physically
live throughout. All four token variants pass.

This proves host-token storage lifetime, not converted-managed-object lifetime.
The remaining mechanism is a representation-specific ownership oracle for a
managed object converted through `extern.convert_any` into an older provider's
extern container: identify the conversion entry/root lease, connect its lifetime
to the provider after the publishing consumer retires, and prove that lease
before any collection or access. The current probe never performs that
conversion. `gc_extern_conversion.go` explicitly distinguishes internal opaque
words, public tokens and compact collector refs; counting compact slots alone
cannot settle this gate. RV64 compact extern bindings are a separate path.
No blanket ABSENT or INCLUDED-FIX disposition is justified for H2-F1.

Relevant main source: `instantiate.go:287` collector selection;
`globals.go:1812` and `cross_instance.go:460` domain admission;
`reference_store.go:1588` registered-instance enumeration;
`gc_extern_conversion.go:29` representation contract. These source mechanisms
explain the observed boundaries, rather than assuming Slipstream's decoder
registration explanation applies to Wago.

### H6-F3: confirmed host ownership-state failure

The fixture is newly built with the repository's ordinary Wasm binary helpers.
A live provider has a mutable anyref global and a collector. A second module
imports that global and a host checkpoint, defines an otherwise empty function,
declares its `ref.func`, and has start:

1. Put the declared function in a newly allocated struct / one-element array.
2. Publish the aggregate in the provider's imported anyref global.
3. Call the ownership-state checkpoint.
4. Execute the ordinary Wasm `unreachable` instruction to fail initialization.

Automatic collection is disabled for this single-allocation probe. The escaped
function is never invoked. There is no public function token and no direct
funcref table/global slot. The imported anyref global links the producer to the
provider; it does not itself supply a reverse owner-retention count.

At the checkpoint, ordinary Go store membership identifies the only instance
other than the provider. Both are physically live in the same collector domain.
Actual persistent-root enumeration observes **two nonzero root slots** (the
owned global and imported alias). No object payload is inspected. The producer
has `resourceRefs=0` and the public funcref token map is empty. A normal physical
release callback is registered while the producer is live.

After the expected start trap, all tested pins report:

- `beforeRefs=0`, `afterRefs=0`;
- physical-release callback count **1**;
- `resourcesClosed=true`;
- no live producer registration;
- public funcref token count **0**.

The ownership assertion fails immediately. The harness does not read the saved
aggregate or any descriptor/code bytes, does not invoke escaped code, and does
not collect or continue teardown. It pins the bounded stopped store in Go until
the test process exits. Consequently eventual release *after dropping the nested
edge* is not tested: the producer already released while the edge existed, so
that branch must not run. The success-only harness branch clears the edge and
checks eventual release, but it was not reached and is not evidence.

Compiled artifacts deliberately stay open on a stopped test. The callback and
`resourcesClosed` prove instance physical teardown; they do **not** prove the
last compiled-code mapping was unmapped. No code-memory inspection is needed
for this ownership assertion.

This establishes a missing producer-retention invariant on Linux/amd64 for this
admitted struct/array publication path. It does not establish a crash, corrupted
read, or RV64 failure. The source report's function-import-only publication
shape is not separately executed; our normal fixture publishes via an imported
anyref global and has the same sole nested reverse ownership obligation.

Main and port have identical `reference_lifetime.go`, `import_attachments.go`,
`function_environment_other.go`, and `instantiate.go` at the compared pins.
`referenceLifetime.finalize` performs retention scans before quiescence and then
releases when resourceRefs is zero. Generic global retention filters direct
`ValFuncRef` globals; the anyref aggregate is outside that scan.
`function_environment_other.go` supplies no managed owner graph on this host.
The earlier rooting and updated port integrations do not repair the observed
invariant. The `45c83820f` update includes immutable exact-none/null admission and
EH/rooting repairs; those do not establish nested function owner retention.

Parent handoff: this is evidence for a PRESENT-OUT-OF-SCOPE disposition for the
**verified host nested-owner failure**, already present on main, subject to the
parent's port-scope triage. No INCLUDED-FIX claim or final cross-target bucket is
made. Proposed repair: establish a collector-traced aggregate-to-function-owner
edge before failed initialization may release producer resources, with release
when the final reachable edge disappears. A conservative admission boundary is
another option until such ownership is supported. Retaining only direct slots or
adding more native frame roots does not satisfy this persistent nested edge.
Production repair remains with the owning worker/parent.

## Read-only sibling cross-check

Before triage, read Slipstream's report
`dcb45b56386327a1f71fb039ed02e13562e1e2d7:hunt-findings.md` and committed source
`82d546679bab4887acfab056f6ad773a969b1e03:src/jit_store.goose:74-99,310-342` and
`src/jit.goose:330-346,410-425`. Its failed-runtime scan checks direct words and
an ABI2 slot-based fallback; its registration occurs in type-section decoding.
These provide analogous ownership obligations, not shared implementation or a
Wago runtime oracle. None of its fixtures were read from their temporary paths,
copied, built or run.

Read Starshine
`1d6c0af47140299d8f3c01ed03060d279c43ee03:docs/wiki/wasm-gc-core-boundary.md`.
Its documented model/codec/validator/authoring layers do not establish Wago's
native runtime rollback semantics. No Starshine runtime defect is inferred.

## Runtime gates and reproduction of the safe probes

- Linux/amd64 only. `qemu-riscv64` and `qemu-riscv64-static` are absent on PATH.
  No RV64 execution or cross-compilation was performed. The test intentionally
  has a Linux/amd64 build constraint. RV64's managed aggregate shadow edges and
  joint owner graph need a native/emulated state harness on a supported target.
- H2 converted managed externref retention and non-null compact coverage remain
  open; the admitted token controls cannot substitute for them.
- H6 eventual release after dropping the sole edge is blocked by the earlier
  failed retention assertion. No unsafe follow-through is authorized or needed.
- Other four round-9 findings are outside this bounded priority test assignment;
  this report neither re-triages nor closes them.
- The shared worktree can advance. Results apply only to the four recorded
  immutable pins, not later dirty or committed changes.

To repeat, extract those exact Git archives into `snapshots/<label>`, copy
`testdata/r9_ownership_state_lifecycle_test.go` to each archive's `src/wago/`, then
run `python3 run.py /absolute/path/to/go <label>` from this evidence directory.
The runner checks the injected probe bytes and records each command, exit code
and SHA-256. It does not install tools, create branches, or time workloads.
The expected current result is exit 1 in each Go test log from the two H6
ownership assertions. Do not replace those assertions with crash invocations.
