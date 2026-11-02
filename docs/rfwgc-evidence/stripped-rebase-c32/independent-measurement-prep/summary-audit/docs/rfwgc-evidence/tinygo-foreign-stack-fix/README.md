# TinyGo foreign-stack GC prerequisite

Implementation: `5bfd1817463230f7b9fe1c8c45f9c9cfffc4135c`.
Tooling-only template placement: `5a173c801`.
Close-failure correction and regression: `57a99d01df0ae2beb5afc7a947bc9e06621b2259`.
This is a prerequisite slice for the combined Snap port PR, not a separate PR.
Final combined review and publication remain parent-owned.

See [DESIGN.md](DESIGN.md) for the ownership, synchronization, transition proof,
cleanup failure behavior, and conservative retention/scan-cost tradeoff.

## Deterministic control

The new native handshake publishes an off-heap entered flag, then waits until
the collector completes explicit GC before allowing the guest to return. This
proves that the requested collection happens with SP on the foreign mapping;
it does not depend on a delay or cancellation scheduling probability.

`prefix-source-head.txt` records the pre-fix immutable source export:
`2e46c7995cf0fe4bb44ae130e2cd08c34a65f1fe`. Only the new regression test was added
to that export. The same patched 0.42.0 compiler was used for both sides: without
Wago registration its registry is empty and the old invalid span remains.
`prefix-handshake-gdb.log` catches SIGSEGV in runtime.scanConservative with the
guest stopped inside the handshake. The initial non-debugged test could not
complete after this fault (GC threads remained paused), so that owned process
was killed; its log is preserved, not counted as a passing or normal failure.
The debugger run uses a bounded command and passes TinyGo's GC signal SIG41.

The fixed handshake additionally covers handler-style stack unwinding for a
trap and interruption, GC while host frames are parked, nested native entry,
GC after resume onto the guest stack, prepared integer entry, GC after Close,
and forced thunk-unmap failure followed by collection and successful cleanup.
`forced-gc-close-failure.log` records these targeted passes on the final
production code. These executions supplement the boundary proof; finite
replays alone do not establish safety.

The original cancellation runner's source and cancellation behavior remain
unchanged; both runner and generator string hashes are compared in
`unchanged-repro-source.txt`. A separate standalone probe injects GC before cancellation and after
it returns, compiling ordinary/wagodebug/wago_regalloccheck TinyGo guest modes.
No GC suppression, heap threshold changes, retries as a fix, or new test skips
were introduced.

## Qualification

**All 34 final matrix commands passed on production head 57a99d01d.**
All eleven CLI Fibonacci checks returned 6765. No failed final-run gate remains.

| Build mode | Go host runtime/public API | Unchanged cancellation | TinyGo threaded runtime/public API |
| --- | --- | --- | --- |
| ordinary | PASS | PASS | PASS |
| wagodebug | PASS | PASS | PASS |
| wago_regalloccheck | PASS | PASS | PASS |

The additional forced-GC standalone guest modes all passed. The Linux amd64 CI
checker, tasks-runtime, CLI, optimized release and process smoke gates passed.
Bootstrap guards, workflow contract/lint, and final arm64 cross-compilation passed.


The authoritative final matrix is `qualification-results.txt`, with exact
commands in adjacent `.command` files and outputs in `.log` files. Source and
fresh cache identity are in `qualification-provenance.txt`. The matrix runs an
immutable export of the final production code, excluding other workers' dirty
changes. Parent changes committed before that export are included.

Host modes are Go 1.27.1 with `wago_runtime`, optionally `wagodebug` or
`wago_regalloccheck`. These host tags do not propagate to TinyGo subprocesses.
Full TinyGo runtime/public-API guest suites explicitly use the same three tag
sets with `-scheduler=threads -gc=conservative`. The standalone generator always
uses `wago_standalone_generator,wago_target_tinygo`; the additional standalone
probe explicitly sets each guest runtime mode, and does not claim to change
the generator's native-code instrumentation through host tags.

The pinned Linux amd64 CI lane additionally covers native standalone linkage
and stripping, tasks-scheduler checker/runtime/public-API tests, the minimal CLI,
its Fibonacci smoke, and ten optimized release startup/Fibonacci invocations.
Fibonacci output must equal 6765. Ordinary and optimized builds use CI's exact
scheduler/tag/optimization choices; commands record whether GC is explicit or
TinyGo's default. Automatic test-framework duration fields in logs are not
performance measurements or claims.

`arm64-final-cross-status.txt` records final Linux arm64 threaded test compilation.
Arm64 execution, Darwin execution, and the eventual combined-port final head
are not qualified on this Linux amd64 machine. They remain part of the parent's
combined gate, not an implied result of cross-compilation.

`qualify.sh` reconstructs the export and starts with a new Go/TinyGo cache.
To select the qualified source without switching branches or overwriting this
capture:

```sh
WAGO_QUALIFICATION_HEAD=57a99d01df0ae2beb5afc7a947bc9e06621b2259 \
WAGO_EVIDENCE_DIR="$(mktemp -d /tmp/wago-foreign-stack-recheck.XXXXXX)" \
bash docs/rfwgc-evidence/tinygo-foreign-stack-fix/qualify.sh
```

It
uses the existing pinned Release 3 corpus/interpreter and WABT 1.0.41, without
changing dirty submodules or regenerating expected accounting. Bootstrap tests
cover pristine apply, idempotence, source/hook drift, and partial-patch rejection;
`version-guard.log` confirms refusal of installed TinyGo 0.43.0. Workflow lint
was run directly with installed actionlint 1.7.7 after its mise shim lacked a
configured default (`workflow-lint-direct.log`).

`superseded-initial-gates/` preserves earlier harness failures: missing trust
for a temporary export's mise config, omitted corpus gitlink contents, then
unintended WABT 1.0.42 selection. Supplying the pinned dependencies made those
host suites pass. That first matrix was stopped when the Close failure-path
issue was found; it is not substituted for the corrected final run. No product
assertion or skip was changed to make those harness failures disappear.

## Scope and handoff

The three implementation commits modify TinyGo trampoline initialization and
owned state cleanup, pinned toolchain bootstrap/hooks, regression tests, and CI.
They do not modify collector production files under `src/core/runtime/gc/native`.
The extended sibling audit is read-only (`sibling-audit.txt`), with no comparable
TinyGo integration found in Slipstream/Starshine. No subagents, Claude review,
branch/worktree switching, PR creation, pushes, or benchmarks were performed.

Performance-sensitive paths are Engine initialization/Close, registry locking,
and TinyGo root scanning of complete registered stack mappings, including idle
engines. Conservative stale words can retain objects until overwrite or Close.
Independent measurement under the global lease should cover engine lifecycle
churn, prepared-call and host-reentry loops with collection, cancellation under
allocation, and GC with multiple active/idle engines and stack capacities.

READY TO MEASURE — branch `experiment/rfwgc-port`, production prerequisite head
`57a99d01df0ae2beb5afc7a947bc9e06621b2259`; measurement and final review are parent-owned.
