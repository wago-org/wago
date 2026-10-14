# Pinned main legacy host-call controls

Apple M4 Max, Go 1.27.1, `GOMAXPROCS=1`,
`BenchmarkInvokeHostFuncDirect`, five 200 ms samples per source/order under the
shared device reservation. The benchmark and `benchReturningImportModule`
function text are identical across the compared sources; their SHA256 values
and every raw result are retained in the JSON. Both sources retain 64 B/op and
one allocation/op. This is `Instance.Invoke` with a custom-slot Go callback,
not the typed pure-helper shared-memory SDK contract.

| Branch source `9ed26450` compared with | Forward delta | Reverse delta |
| --- | ---: | ---: |
| Current-main snapshot `069a26ea` | +6.10% | +4.96% |
| PR merge base `8097344a` | +5.13% | +2.37% |

Current GitHub main was verified as `069a26ea07bdd312b672d4fc56b989aa55e89e98`
before the fresh run. Each comparison uses a separate checkout pinned to the
recorded commit. See [current-main samples](pr-main-host-control-pure-callee.json)
and [merge-base samples](pr-merge-base-host-control-pure-callee.json).

The legacy control remains slower. The pure-helper improvement does not resolve
this contrary result or establish a branch-wide speedup. A complete default-main
feature/compile/RSS comparison remains outstanding; unsupported features require
separate admission accounting. The PR remains draft.

## Historical baseline correction

The cleanup, parent-fix and guarded-import control scripts referenced
`pr-main-baseline-source` but hardcoded `8097344a` in their reports. Its branch
reflog shows it was created from `069a26ea` at 2026-10-14 16:50:58 UTC and never
moved. All three runs' output timestamps follow that creation. Their tracked
baseline HEAD was therefore `069a26ea`, not the reported `8097344a`.

The original output files and timings are preserved. The companion JSON files
now retain the originally reported label alongside the corrected HEAD and a
qualification: no historical clean-tree receipt exists. Prefer the fresh pinned
runs above. The [source-attribution evidence](main-control-source-attribution.json)
contains the reflog and SHA256/timestamps of every historical raw output.

| Historical branch control | Current / reconstructed-main medians |
| --- | --- |
| Cleanup workspace, subsequently committed through `66602322` | 1.07262 / 1.06306 |
| Caller parent fix `30b16166` | 1.00782 / 1.08251 |
| Guarded import retention `75171913` | 1.09862 / 1.06949 |

These remain distinct historical controls. No reported sample is discarded or
retimed, and their source-qualification limit is explicit. See the separate
[matched pure-helper audit](native-pure-callee.md) and
[guarded-import audit](guarded-native-imports.md) for incremental SDK results.
