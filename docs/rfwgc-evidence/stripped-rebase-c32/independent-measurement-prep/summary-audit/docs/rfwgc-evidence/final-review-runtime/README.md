# Final runtime integration review

Review source: `5345bd365c13963f1bf60e26bb80789ab9ead4cc`

Evidence source: `2c9f72744dd6f414fc8f236c2d45aa39ce6ced69`

Campaign base: `9713878ab452848f7eb7549a90be94b938a898be`

This is one independent review slice, not final approval. It does not replace
performance qualification or native Darwin execution. The existing combined
matrix reports 195 executable passes and Darwin `NOT_RUN`; those results were
treated as execution receipts, not design proof.

## Verdict

No verified production defect was established in this slice. In particular, the
review did not find a missing authority transition in the host-created/imported
extern table/global store paths, receipt release/retirement journal, conditional
owner reachability, owner-cycle collection, public Close handoff, PR997/1026
foreign-frame continuation, PR1027 scalar-EH collector choice, or TinyGo
registered foreign-stack cleanup.

This is not a statement that every target is qualified. The coverage gaps below
remain material and are deliberately not promoted to defects without an
executable target or a contradictory source invariant.

## Source review coverage

Reviewed the full relevant campaign range and the integration commits for
PR997/1026/1027 and R9/H2/H6, then traced these concrete paths at the production
pin:

- lifecycle admission, logical Close, reentrant Close, `WaitClosed`, invocation
  quiescence and physical finalization in `src/wago/instance_lifecycle.go`;
- topology reader/writer ordering, domain registration/unlink, host retry,
  deferred receipt-claim drain and collector Close-after-unlink in
  `function_environment_schedule_linux.go`, `function_environment_retry_host.go`
  and `gc_conversion_receipt_host.go`;
- host-created physical table/global registration, public mutation and Close in
  `gc_conversion_host_container_host.go`, `cross_instance.go` and `globals.go`;
- module-owned/imported single stores, fill/copy/init/grow, staged initialization,
  transactional prepare/commit/rollback, migration identity and conditional
  receipt roots in `gc_conversion_store.go` and `gc_conversion_store_host.go`;
- host reentry, bounded scalar-adapter/foreign caller continuation, exception
  ownership and collector selection in `gc_collect.go`, `reference_store.go`,
  `gc_frame_roots*`, `host_execution.go` and `gc_type_domain.go`;
- owner cycles, quiescent retries, deferred finalizers and terminal retirement in
  `function_environment_*`, `import_attachments.go` and `reference_lifetime.go`;
- TinyGo foreign-stack registration lifetime and unregister-before-fallible-unmap
  in `trampoline_tinygo_{amd64,arm64}.go`, `foreign_stack_tinygo_*` and
  `engine_prepared_state_tinygo.go`;
- excluded implementations selected by `!linux`, `riscv64`, `tinygo` and
  `wago_target_tinygo` build constraints.

The central lock/authority sequence is coherent in the inspected host receipt
paths: public host mutation and host-container Close acquire topology/domain/native
authority before container mutation; receipt creation takes a domain claim before
publication; release removes the root before queuing claim consumption; retirement
unlinks a domain under the topology writer and closes the collector only after the
writer is released. The reviewed public Close path returns promptly on reentry by
contract and exposes the owning result through `WaitClosed`.

## Verified defects

None.

Because there is no verified defect, no sibling project triage bucket or repair
proposal is asserted.

## Questions and coverage gaps

1. **Native Darwin execution remains absent.** The combined README explicitly
   records Darwin `NOT_RUN`. TinyGo foreign-stack code includes Darwin arm64 in
   its build constraint, but the retained prerequisite evidence establishes only
   cross-compilation there. A Darwin arm64 execution lane should cover threaded
   conservative GC during parked host calls, nested entry, trap/interruption,
   prepared entry and Close/unmap failure before portability closure.

2. **Opaque physical conversion receipts are intentionally target-limited.** At
   this pin `src/wago/gc_conversion_receipt_host.go:1` and
   `gc_conversion_host_container_host.go:1` select Linux, non-RV64, standard-Go
   hosts. `gc_conversion_receipt_other.go:1-18` supplies no receipt/container
   implementation for non-Linux, RV64 or TinyGo, while
   `gc_conversion_receipt_riscv64.go:1-7` only supplies the compact-domain hooks.
   The final combined report correctly does not count RV64/TinyGo compatibility
   as opaque-host-receipt support. This is a support boundary, not a regression
   against the campaign baseline; either keep the public support map explicit or
   add target-specific physical authority plus Close/rollback tests before making
   a broader claim.

3. **TinyGo serial schedulers rely on a non-collection invariant rather than
   registration.** `src/core/runtime/foreign_stack_tinygo_serial.go:7-10` returns
   a nil registration token because native code cannot allocate or call Go on the
   foreign stack. The threaded patched-0.42 lane exercises the registered path,
   but a future trampoline or scheduler change that permits a Go callback on the
   foreign stack would invalidate this premise. Keep this as a reviewed invariant
   with a build/test guard; current source and evidence do not contradict it.

4. **Public reentrant `Close` intentionally suppresses the active owner's eventual
   error.** `src/wago/instance_lifecycle.go:34-45` returns nil to a callback that
   joins an in-progress Close; `WaitClosed` is the result-bearing API. Existing
   terminal-result tests cover the owner/join API. Documentation should continue
   to make this distinction prominent for embedders; it is contractual behavior,
   not a newly introduced defect.

5. **Performance is pending.** The reviewed paths add topology, domain and native
   lock acquisition to host conversion access and add foreign-stack registry work
   for threaded TinyGo. No timings or benchmarks were taken because this review
   had no measurement lease. Correctness passes cannot close that obligation.

## Independent deterministic replay

A fresh `git archive` of the production pin was used; no dirty worktree source,
submodule or index content entered the run. The focused selection covered every
`TestR9H2*` test plus extern conversion/frame roots, scalar adapters/EH and the
named Close-result tests. All three commands passed:

```text
GOROOT=/home/dev/.local/share/mise/installs/go/1.27.1 \
  /home/dev/.local/share/mise/installs/go/1.27.1/bin/go test \
  -tags=wago_runtime ./src/wago -run <focused-pattern> -count=1

... -tags='wago_runtime wagodebug' ...
... -tags='wago_runtime wago_regalloccheck' ...
```

The exact focused pattern was:

```text
Test(R9H2|GCExtern|GCDomainHostRootsAcrossReentry|GCScalarAdapter|GCScalarException|InstanceCloseTerminalCollectionHandoff|InstanceCloseAndWaitJoinsOrdinaryAndTerminalErrors)$|TestR9H2.*|TestGCExtern.*|TestGCScalarAdapter.*
```

No benchmark, timing loop, race beyond the already retained combined evidence,
unsafe-prefix execution, production edit, push, PR action, closure or subagent was
performed. The first tool invocation used a nonexistent `/usr/local/go/bin/go`
and therefore executed no test; the replay then used the explicit Go 1.27.1 path.

## Read-only sibling audit

Slipstream was inspected at object
`084be3d4bf70fb5337085ceddfab1bfa9db8b1c9`. Its `src/jit.goose:270-304`
registers imported table/global native regions before publication, but its region
model does not provide Wago's physical host-container receipt/Close authority.
Starshine was inspected at object
`1d6c0af47140299d8f3c01ed03060d279c43ee03`; its
`docs/wiki/wasm-gc-core-boundary.md:35-60` is a model/codec/validator/generator
boundary and has no analogous runtime collector ownership protocol. Neither
sibling supplies evidence that closes Wago's excluded-target gaps. Both sibling
working trees were read only and their pre-existing dirt was untouched.

## Handoff

The parent should consolidate this no-defect review together with the other
independent reviews, retain the target/performance gaps, and request fresh review
after any production repair or scope change. The evidence commit is reported in
the handoff rather than embedded here to avoid a self-referential hash.
