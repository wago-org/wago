# PR 980: wait for native imported-start runtime teardown

The [native RISC-V job](https://github.com/wago-org/wago/actions/runs/39741136024/job/119699280029)
reports one failed checked subtest:
`TestRV64PublicNativeImportedStartStateReentryAndLifecycle/sharedtrue-artifactfalse-runtimetrue-runtime-close`.
It reports `failed native start leaked attachment/publication`. The ordinary
pass succeeds. This is separate from the repaired ARM64 GC scheduling fixture.

## Cause and deterministic regression

`Runtime.Close` publishes shutdown and returns promptly. A worker closes its
instances, runs hooks, and later publishes `Closed`. `Instance.Close` also
returns promptly when another goroutine owns close preparation. A failed
constructor must not wait for a concurrent close owner that can depend on
that constructor returning. The test joins the constructor but then inspects
its memory, thunks, and producer roots before the runtime close worker finishes.
That is a test synchronization race.

Hold the constructor's BeforeClose hook in the runtime close worker. Wait for
that hook before releasing the parked host callback. The constructor must still
return. With the original leak check, the exact reported case fails under
register-checked QEMU: producer roots 1 instead of 0, memory and thunks still
present. This controlled interleaving reproduces the reported failure without
relying on native scheduler timing.

The fixed fixture verifies that runtime shutdown remains pending while its hook
is held, releases the hook, and calls `Runtime.WaitClosed` with an independent
five-second context. It then retains the exact root-count, memory, thunk,
external-write, error, and lifecycle assertions. No sleeps or polling are used
to repair the leak check. Deferred cleanup releases both gates before joining
the constructor. The existing public start/cancel limits are unchanged.

## Source identity

The branch's old base lacks this test. Merge exact CI main
`688303ebfa75384e89773e3fc865b8bccb2e6a71` into old head
`33f5dbc392bb3064d4661c2b614f4550541101a5` in an isolated checkout. The automatic
merge index is byte-for-byte the hosted merge tree
`1c9bb87f815d5128f167e4dd68c0ec253324334f`, whose required CI already passes.
Only this RV64 test and this note differ from that tested merge. The AMD64
array-length lowering and ARM64 yield changes remain present. Runtime/compiler
source matches the tested merge; no production teardown behavior changes.

## Validation

Go 1.27.1, GOMAXPROCS=2, RVA20 baseline QEMU, package concurrency 1.
The gated runtime-close matrix passes 50 repeats in each build mode. All native
imported-start tests pass three repeats in each mode, including fresh/saved code,
shared/private memory, managed/direct entry, cancellation, reentry, traps,
producer retirement, active-loop close and cleanup. The failing gate is kept as
a red result, not relabeled as a passing test.

Times include builds. Long test-package builds exceed the 30-second compiler
activity target and are retained as build costs. No new speed claim is made.

| Check | Exit | Wall seconds | Peak RSS KiB |
| --- | ---: | ---: | ---: |
| shutdown-gated-red | 1 | 38.371 | 4,649,540 |
| shutdown-gated-ordinary | 0 | 39.869 | 4,728,068 |
| imported-native-start-ordinary | 0 | 3.634 | 352,328 |
| shutdown-gated-checked | 0 | 35.780 | 4,872,684 |
| imported-native-start-checked | 0 | 3.519 | 357,120 |
| docs-check | 0 | 0.060 | 27,392 |

The local diff is formatting-clean. The docs checker passes for 86 Markdown
files. Full ordinary and checked workspace suites are not rerun for this
RV64-only test synchronization change: required hosted CI on the identical
production merge tree already passed, including both ARM64 platforms.
Native CI must still qualify the new commit; QEMU is not native timing evidence.
