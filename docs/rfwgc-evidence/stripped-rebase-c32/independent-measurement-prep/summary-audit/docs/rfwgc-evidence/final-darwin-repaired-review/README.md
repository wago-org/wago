# Fresh narrow activation review — repaired source f2bb27287

Review date: 2026-11-02 UTC.

Execution source: `f2bb27287220783e86a4ead41fee78693a5129de`.
Workflow/harness: `6bd79ae0403ed75e7f20489d93d731ce9a42baeb`.

Scope was limited to activation routing, CI policy, source/driver identity,
native contract selection and the associated harness scripts. This review made
no production or workflow change and performed no dispatch, push, PR, benchmark
or native target execution. Existing worktree and index state was preserved.

## Findings

No activation blocker was found.

## Activation-safe verdict

**YES: the reviewed special non-Linux dispatch is activation-safe at the exact
workflow/harness commit above, for the exact source above.** This verdict is
only about safely activating the evidence-producing route. It does not qualify
Darwin or Windows behavior, approve the port, supersede the original runtime
Claude re-review, or complete the active full-combined rerun.

The workflow graph contains 23 jobs. `ci-ok` directly depends on every other
job. `darwin-qualification-summary` depends on every normal job and both native
matrix jobs, but not on `ci-ok`; exhaustive traversal in the CI-policy test
finds no dependency cycle. On the special event, the normal aggregate is
disabled and its conditional display name is `Full CI not run (non-Linux
dispatch)`, including the skipped check GitHub may create. The enabled special
aggregate is named `Non-Linux qualification summary`. Therefore that event
emits neither a successful nor a skipped check named `CI`.

The special aggregate succeeds only when both native matrix results are
`success` and every normal dependency result is `skipped`. Its `toJSON(needs)`
check excludes only `darwin-qualification` and `windows-qualification`; it does
not hide a normal result. The normal `ci-ok` verifier remains fail-closed.

Normal-event behavior is unchanged relative to the established `e8af5e012`
baseline after removing the special routing additions: all normal job bodies
are byte-equivalent, `changes` has only the special-dispatch guard, and removing
the conditional name/guard plus three special dependencies recovers the prior
`ci-ok` body exactly. The local validator checked this mechanically. The test
expectations were not weakened; `TestAggregateCIRequiresEveryWorkflowJob`
requires direct coverage of every workflow job, and
`TestPinnedQualificationIsolatedAndAcyclic` independently checks the context
isolation and the complete dependency graph for cycles.

## Exact source, driver and command contracts

Both object IDs exist as commits, and the execution source is an ancestor of
the workflow/harness commit. The workflow checks out the harness at
`github.sha` and the source independently at the full `source_sha`. The default,
plan and contract source pins all equal the execution source above. `identity.py`
requires exact lowercase 40-hex identities, harness HEAD equal to
`QUALIFICATION_DRIVER`, and a clean harness. `contracts.py` and `run.py` repeat
source HEAD, clean-tree, native host/toolchain and manifest checks. Source,
harness and receipt paths must be independent. Darwin rejects Rosetta; both
native contracts reject Linux and cross-compilation before output creation.

The native contract runs a package build-file census followed by one exact
`go test -json` selector, with Go 1.27.1, `GOTOOLCHAIN=local`, `CGO_ENABLED=0`
and `GOWORK=off`. Inherited target/build overrides are rejected. Any failed or
skipped event, absent or duplicate expected top-level pass, census/command
failure, changed source/driver, or fixture/hash mismatch fails qualification.

Darwin requires exactly five top-level tests:

- `TestDesignRepairPublicTableOrdinary`
- `TestDesignRepairUnsupportedBulkAtomicity`
- `TestDesignRepairUnsupportedConvertedPublicToken`
- `TestDesignRepairUnsupportedConvertedPublication`
- `TestRFWStage3MeasurementGuest`

The graph test additionally requires all six exact `noGC`, `scalar` and `graph`
leaves for both false and true modes. It invokes each fixture 64 times, checks
the return oracle, collects, requires the graph to have at least 2,048
allocations and exactly 32 live objects, requires noGC to have zero allocations,
and compares exported graph/scalar/noGC bytes with hashed fixtures.

Windows requires exactly four top-level tests:

- `TestDesignRepairPublicTableOrdinary`
- `TestDesignRepairUnsupportedBulkAtomicity`
- `TestDesignRepairUnsupportedConvertedPublicToken`
- `TestQualificationWindowsGCAdmission`

The fourth test asserts that Windows AMD64 does not admit Core 3, compilation
returns no code and returns `UnsupportedFeatureError`. The Darwin-only converted
publication body is deliberately absent because `supportsCompleteCore3Backend`
excludes Windows AMD64. Consequently this route claims Windows ordinary public
storage and rejection/admission behavior only; it makes no false Windows
converted-GC-JIT or converted-guest-execution claim.

## Narrow local validation

Executed:

```text
PYTHONDONTWRITEBYTECODE=1 python3 docs/rfwgc-evidence/final-darwin/validate_local.py
PASS: local contracts; Darwin remains NOT_RUN
```

That validator passed the CI-policy package, CI planner package and full-profile
planner verifier in ordinary, `wagodebug` and `wago_regalloccheck` modes. It
also passed actionlint 1.7.7, Python/Bash syntax, 64 explicit special-summary
state combinations, exact and negative native test census cases, source and
fixture hashes, identity rejection cases, and all six Linux native-contract
refusals. It verified the 63-command Darwin supplement plan without executing
it. No broad matrix or benchmark was run. The validator's incidental rewrite
of duration strings in its pre-existing receipt was restored byte-for-byte.

## Full-platform-qualification verdict

**NO: full platform qualification is not established.** Native Darwin ARM64 and
Windows AMD64 execution is `NOT_RUN`; activation merely enables those runs.
The original runtime Claude re-review remains active, and the repaired-source
full-combined rerun remains the owner of broad source qualification.

TinyGo is an unclosed execution gap, not a waiver. A reviewed native Darwin
TinyGo 0.42.0 compiler archive and patched runtime identity still must be
provisioned, followed by the evacuation, foreign-stack/joint and supported
conversion selectors in all three modes with an exact target census.

Matched baseline graph evidence is also an unclosed supplement, not a waiver.
Candidate Darwin graph success cannot replace the separate baseline export and
matched baseline graph run at `a3c08365f531eb3c9003e3acf3e0a822c12c31f4`.
The Darwin candidate overlay itself is still `NOT_RUN` until native activation.

The earlier `final-combined-9180` failure is not credited as repaired-source
qualification: it stopped on archived probes being discovered as a package and
on missing aggregate dependencies, and its interrupted work and old-source
passes remain historical evidence only. The repaired route statically and
locally closes the activation-policy defect; only fresh native artifacts and
the independent full-combined result can close the execution and platform
qualification gates.
