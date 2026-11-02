# Exact current-pin activation review

Independent bounded review of source `08f511fac41a77f4c0f9046f88ebc78ad05a5da6`
and harness `041f7ed4c376e2a33ce21b22075cd617ddca6597`.

## Activation decisions

- **Darwin/ARM64 + Windows/AMD64 activation: YES** for exactly this source and
  harness. This authorizes the pinned workflow to collect new evidence; it is
  not native qualification. Native Darwin and Windows execution is **NOT RUN**.
- **Linux/ARM64 activation: YES** for exactly this source and harness. Native
  Linux ARM64 execution, timer control, race binary, and all suite results are
  **NOT RUN**.

No later source or harness inherits either decision without another review.

## Correctness and expectation audit

The source delta changes only the branch selecting the already-existing
unsupported expectation in `TestGCExternConversionAcrossReferenceTables`:
non-RV64 targets without compiled conversion-store hooks reject; RV64 retains
its compact-reference route; targets with compiled hooks take the positive
route. Wasm bytes, the positive sum `545`, host value `hello`, full-collection
assertion, before-write table-byte rejection, rejection message, and retained
host token are unchanged. The exact test passed in ordinary, wagodebug, and
wago_regalloccheck on Linux AMD64 with no skip. Darwin/ARM64 and Windows/AMD64
source-selected test packages cross-built in all three modes.

The archived run 41447853354 remains a Darwin **FAIL** at its historical 7e41
source. None of its 15 failing aggregate commands is promoted to pass. Its
successful Windows storage/admission and other passing subsets retain their
original bounded meaning only. The c259 audit and raw artifacts were not edited.

`audit.json` records exact pins and census counts: Darwin focus 73 including 22
RR names and the new multi-table test, Windows six, Linux ARM focus 85, guard
one, runtime 11, backend nine, and all 63 broad commands. Source blob hashes,
the unchanged cancellation source, Core 3 215/690 enforcement, rejection-before-
accumulation, graph fixtures, and current revision contracts match. The native
ARM probe census, timer/bootstrap hashes and deadlines are unchanged except the
known formatted `rr_recheck_flags_current_test.go` hash. Backend cross-builds
retain `wago_codegenstats` in every mode.

## Harness and CI validation

The existing bounded `final-darwin/validate_local.py` was reused through
`run_validation.py`, redirecting only its final receipt into this directory.
It passed all three CI-policy packages, all three planner test/verify modes,
actionlint 1.7.7, 64 summary-state combinations, identity/refusal negatives,
exact and negative contract censuses, source/fixture hashes, normal-job and
concurrency equivalence, cancellation/Core 3 discriminators, and continuation
fail-closed controls. The six existing native ARM harness unit controls also
passed. Check names, dependencies, mutual isolation, time ceilings, original
censuses, timer/deadline behavior, and backend-only diagnostic tags remain
intact.

The shared HEAD advanced after the requested harness to an evidence-only commit;
the reviewed workflow, final-Darwin harness, native-ARM harness, and CI-policy
file remain byte-identical to `041f7ed4c`. The combined worker's dirty files
were not read as qualification outputs or edited.

## Remaining gaps

Fresh native execution is still required for Darwin positive multi-table
follow-through and full Darwin aggregation, Windows storage/admission rerun,
and Linux ARM timer/race/focused/guard/runtime/backend censuses. Darwin TinyGo,
full CI, and other architectures remain outside this activation review. Cross-
compilation is not runtime evidence. No benchmark, timing, dispatch, push, PR,
workflow/source edit, full-suite duplicate, Claude review, or subagent was used.
