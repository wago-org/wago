# November 4 delivery target

Owner target: landed Wednesday 2026-11-04. Planning checkpoint 2026-11-02
02:20 UTC, source head5c0309145. The target does not waive any gate. Integration
owner alone assigns implementation and final dispositions; support Astra is advisory.

## Critical path and planning cutoffs (UTC)

- Monday Nov2: complete A/B verification (A4bf395915, B7b14d483a already landed),
  compiler repair/diagnostic qualification and Staticcheck resolution. Parent race
  fix3b04e6837 has targeted all3 race PASS. Freeze coherent candidate by end of day.
- Tuesday Nov3: complete all immutable combined suites, explicit corpus/spec,
  actual supported target/TinyGo lanes and platform gaps. After correctness workers
  drain, independent performance worker gets the global lease; matched controls,
  thermal rule and evidence completeness remain mandatory. Run three Daybreak plus
  one Claude review concurrently on the pinned complete diff, consolidate findings.
- Tuesday into Wednesday: fix findings, independently re-review each fix and rerun
  affected plus required final-head gates. Zero unresolved findings before ready PR.
  Open READY early Wednesday only if every gate is green; link owning thread and
  steward CI/review/rebase. Landing also requires external review/merge availability.

These are allocated windows, not measured completion forecasts. Review fixes can
move the source pin and invalidate prior passes; no stale-head qualification.
No runtime load from correctness workers may overlap measurement acquisition.
Review can overlap measurement only when read-only/source analysis introduces no
measurement-host load; otherwise keep them serialized.

## Current risks and time cost

- A final immutable lanes: worker estimates tens of minutes within current session
  if no new failure, explicitly uncertain. B combined qualification still running;
  exact remaining duration not established.
- Deferred-load invariant: fix5c0309145 landed, validation still pending. Default
  route diagnostics still being repaired. Root-cause/validation cost not yet
  bounded; worker asked to report estimates and failures immediately.
- Eight distinct Staticcheck locations: unresolved, several target-specific fields
  used by RV64. Must preserve cross-target semantics, not remove/suppress blindly.
  Requires A ownership release then cleanup and lint recheck; duration unknown.
- Darwin execution: no native/remote endpoint established. Linux QEMU cannot
  substitute. Acquisition cost unbounded until a usable runner exists; deadline
  risk now, not a Wednesday surprise.
- Independent measurements: not started; lease/worker drain/thermal/corpus controls
  required. Runtime duration and cool-down cost unknown until protocol finalized;
  cannot promise completion within Tuesday's window.
- Missing supplied997421119d1d/100311bcf23fd objects: verified alternative topic heads
  integrated, but no exact-equivalence claim. Provenance must be settled before
  final bucket/PR representation; recovery time unknown.
- Four-reviewer fan and any findings: no honest repair-time bound before reports.
  Reserve Tuesday/Wednesday for fix/re-review; missed Monday freeze directly
  consumes this margin. External PR review/CI/merge cannot be guaranteed by agent.

Report each new blocker immediately with failing pin/command or invariant, owner,
repair action, observed duration/credible estimate when available, and unknown
cost explicitly. If a hard gate stays red/unrun, PR waits rather than landing to date.
