# H2 implementation checkpoint: incomplete, stopped qualification

No H2 production implementation is contained in this checkpoint. This is not
INCLUDED-FIX, a completed supported path, or READY TO MEASURE. The requested
implementation and combined qualification remain outstanding. No external
permission or tooling blocker was established.

Immutable source: `bca404e4481e5948193a2e6a32eec58ad692761d`. This is the
post-H6 authorization head, including production
`631cb70499e3fa61357171a0e3d1a60e7a0883fa` and its evidence
`30ad134bf48f94a3a64c7e0700d7fbf9e269c813` (18/18 and driver0). The earlier
`6a25f86e5` replacement transaction result remains a prerequisite only.

## Results

All three stopped probe commands returned 1: ordinary, wagodebug and
wago_regalloccheck. Each mode records four provider-ownership failures and four
region admission skips, plus one converted-function-aggregate admission failure.
Totals: twelve provider failures, twelve explicit region skips and three
aggregate admission failures. Parent-test summary lines are not extra cases.
No skip is counted as a pass. No payload read, collection, or container teardown
follows a failed ownership invariant. The aggregate probe stops before instance
construction.

`attempt-bca404e44-formatted/` contains exact commands, raw logs, source/archive/probe hashes,
the unchanged executed driver copy, and its final hash verification. The driver
finished normally; its result ledger explicitly says all_suites_pass=false.
The original converted-provider probe is byte-for-byte copied from
`../r9-converted-provider/testdata/r9_converted_provider_state_test.go`.
The first `attempt-bca404e44/` run is also retained, including its exact
pre-gofmt aggregate probe. The commit hook formatted that new probe; the final
run re-executes the formatted copy in all three modes without editing its driver.
Original main/port evidence70359b920 remains authoritative for its own pins:
24 failures and24 region skips. This checkpoint does not relabel those results.

## Additional implementation prerequisite

The new compile-only fixture contains a function-valued struct, a declared local
function, `struct.new`, `extern.convert_any`, and publication to an imported
externref global. It compiles with:

```
product=12 generic=false conversion=true managed=false frameRoots=false sharedSafe=true
```

This defeats a repair limited to container receipts: the converted aggregate
never enters H6 function ownership tracking. `api.go` chooses stagedGCStructExtern
for the source conversion obligation, while
`function_environment_platform_host.go:compiledManagedFunctionReachability`
requires usesGenericGCExecution. `dispatchManagedFunctionAggregate` therefore
bypasses its shadow-edge path. The absent frame roots also mean region admission
cannot safely be widened by removing DisableCollection alone. This is a concrete
unimplemented dependency, not an assertion that the authorized task is impossible.

Other source constraints confirmed during inspection:

- AMD64/ARM64 extern global stores and most extern table operations still write
  inline, including collector-free providers that later clear/copy a value.
- Conversion checked roots belong to the publishing conversion state; close
  clears that state and private collector regardless of the surviving provider.
- H6 private root slots are addressed through owner.gc. A nil-collector provider
  needs an explicit slot/domain association, without treating its opaque word as
  a compact handle or making the conversion root unconditional.
- Receipt-only domains must remain in ordered invocation/joint collection leases
  after publisher retirement, with final domain release outside held topology
  readers. Existing topology-membership retirement checks must remain intact.

No production files, native collector/configuration files, shared log, unrelated
working-tree edits or preexisting index entries were changed. No full suite,
corpus, backend, race, target/QEMU or TinyGo result is newly claimed here. These
remain required after implementation. No timing, benchmark, subagent, PR, push,
or closure was performed. Evidence-only changes cannot affect runtime performance.

## Read-only sibling source checks

Read Slipstream actual `084be3d4bf70fb5337085ceddfab1bfa9db8b1c9`, merged as
`1a4759e5c`: imported table/global roots are installed if absent, failure is checked,
and registration occurs before the first semantic section even without types.
That establishes the before-publication obligation but not Wago opaque identity,
domain lifetime or conditional-cycle semantics.

Read Starshine `1d6c0af47140299d8f3c01ed03060d279c43ee03`,
`docs/wiki/wasm-gc-core-boundary.md`: its documented model/codec/validation layers
do not establish an analogous native conversion ownership contract. No sibling
runtime defect or absence claim follows.

Reproduce from this worktree (output directory must not exist):

```
python3 docs/rfwgc-evidence/r9-converted-provider-fix/replay.py --pin bca404e4481e5948193a2e6a32eec58ad692761d --out /tmp/wago-h2-stopped-replay
```
