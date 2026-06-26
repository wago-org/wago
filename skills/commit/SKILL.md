# Commit Skill

Use this skill whenever preparing or reviewing commits for `wago`.

## Goal

Produce measurable, small, atomic commits that move one topic forward without
hiding unrelated work. A good commit does one of these things:

1. adds a red test that demonstrates a missing behavior or regression;
2. makes focused tests pass with the smallest implementation change; or
3. does both for one tightly related topic when splitting would add noise.

## Commit Shape

Each commit should have:

- **One topic.** Keep decoder, validator, backend, runtime, CLI, docs, and
  benchmark work separated unless the behavior is inseparable.
- **A measurable claim.** The commit should name the test, benchmark, fixture,
  or observable behavior that proves the change.
- **A docs companion.** Include associated developer/agent documentation changes
  under `docs/` when the commit changes workflow, expectations, testing,
  benchmarking, or agent behavior.
- **A clear boundary.** Avoid drive-by cleanup, broad formatting, unrelated
  renames, or speculative refactors.

## Red / Green Discipline

Prefer this sequence:

1. **Red commit:** add the smallest failing test or fixture that captures the
   behavior. Do not include the implementation unless the red state cannot be
   represented independently.
2. **Green commit:** implement the smallest change that makes the focused test
   pass while preserving existing tests.
3. **Refine commit:** only if needed, clean up structure or performance after the
   behavior is proven. Keep this separate and measurable.

If a red and green change are combined, explain why in the commit message or PR
notes.

## Documentation Companion Rule

Every commit should ask: "Did this change how future developers or agents should
work?"

If yes, update `docs/` in the same commit. Examples:

- new test helper or fixture convention;
- changed benchmark procedure or required measurement;
- new unsafe/runtime review rule;
- changed commit, review, or agent workflow;
- new performance or memory-footprint expectation.

If no docs update is needed, the commit message or PR notes should make that
obvious, for example: "Docs unchanged: internal bug fix with no workflow impact."

## Message Template

```text
area: imperative summary

Why:
- what behavior, regression, or measurement motivated this

What:
- the focused code/test/docs change

Proof:
- go test ./...
- specific package test, fixture, benchmark, or measurement

Docs:
- docs/<file>.md updated
- or: unchanged, no developer/agent workflow impact
```

Keep the subject short and specific, such as:

- `wasm: reject passive data segments explicitly`
- `amd64: add red test for i64.load8_s`
- `runtime: reduce host-call buffer allocation`
- `docs: record backend benchmark workflow`

## Pre-Commit Checklist

- [ ] The diff has one topic.
- [ ] Tests or benchmarks prove the claim.
- [ ] Hot-path or memory-sensitive changes include before/after numbers when
      practical.
- [ ] Developer/agent docs in `docs/` were updated, or the no-docs reason is
      stated.
- [ ] Unsupported wasm behavior is rejected explicitly.
- [ ] No unrelated formatting, renames, or cleanup are included.
