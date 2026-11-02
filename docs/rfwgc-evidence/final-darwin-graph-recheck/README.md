# Final narrow Darwin graph harness recheck

Decision: **activation-safe to rerun the exact source
`f2bb27287220783e86a4ead41fee78693a5129de` with the repaired harness at
`1fe8aab8edc8451731f3534f59b37a4fcca42640`.** This is approval only for the
focused native qualification rerun. It is not broad platform approval.

## Finding

The archived native run `41393006371` verifies the first failure as a harness
configuration error. In every Darwin mode, each of the six graph/scalar/noGC
leaves stopped during `Compile` with
`wago: unknown arm64 optimization "gc-lifetime-analyze"`. At source f2bb27287,
`src/core/compiler/optimization/catalog.go` registers that option through
`experimentalAMD64`, and `src/wago/config.go` rejects configured optimization
names not present for `runtime.GOARCH`. The failure therefore precedes guest
invocation, collection, and heap inspection; it is not evidence of a runtime GC
defect.

The reviewed commit removes exactly one occurrence of
`.WithOptimization("gc-lifetime-analyze", true)` from the Darwin-only graph
fixture. A byte-for-byte transformation check proves that this token removal is
the fixture's only content change. The fixture still requires CoreV3 and
`inline=false`; preserves identical graph, scalar, and noGC Wasm bytes; runs all
six leaves and 64 invocations per leaf; invokes collection; requires at least
2,048 graph allocations and exactly 32 live graph objects; checks the invocation
result; and requires zero noGC allocations. The contracts fixture hash was
updated to the independently reproduced SHA256
`be828e247b0e6286d18f9b159414c7ccd2baf7bfa5af6ddeebcd1d85b661b101`.
The validator independently reconstructs the fixture from source f2bb27287,
requires exactly one removable option occurrence, performs that removal, and
then requires exact equality. No oracle was weakened.

The commit changes only the fixture, its hash/repair receipt, regenerated local
validation receipt, and the validator transformation. Its `src` tree is
identical before and after the commit. The archived failed-run directory is
unchanged, retaining the original artifacts, logs, receipts, metadata, and
verifier. The execution source pin remains f2bb27287.

## Local recheck

The following completed successfully from the reviewed checkout without native
execution, remote dispatch, or timing work:

- `PYTHONDONTWRITEBYTECODE=1 python3 docs/rfwgc-evidence/native-run-41393006371/verify.py`
- `PYTHONDONTWRITEBYTECODE=1 python3 docs/rfwgc-evidence/final-darwin/validate_local.py`

The latter re-executed the CI policy package, planner tests, and full-profile
planner verification in ordinary, wagodebug, and wago_regalloccheck modes. It
also passed actionlint 1.7.7, Python and Bash syntax, exact hashes, exact and
negative test census checks, and Linux refusal checks. It reports `Darwin
remains NOT_RUN` as required.

Previously retained native results remain bounded: the actual Windows four-test
contract passed in all three modes, and the four applicable Darwin contract
tests passed in all three modes. The repaired Darwin six-leaf graph contract,
the broad 63-command route, matched baseline graph supplement, and patched
TinyGo execution remain open. A successful focused rerun cannot by itself close
those broader gates or authorize platform inclusion.

No production edit, push, remote dispatch, benchmark, timing measurement, or
subagent was used for this recheck.
