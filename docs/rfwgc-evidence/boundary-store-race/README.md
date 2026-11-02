# Lifetime boundary reference-store snapshot

Repairs the verified race in combined-store-hook-gates/FINDINGS.md item1.
referenceStoreForBoundary publishes Instance.refStore under lifeMu; the host
lifetime-boundary hook now snapshots under the same mutex and releases it before
graph lookup/drain. All three call sites execute after lifeMu is released; graph
collection therefore retains its existing topology/domain/native lock order.
No source-owned API or A/B implementation file was changed.

source.txt identifies the immutable HEAD archive with only this one production
file overlaid. results.json and mode logs record the existing targeted detector
regression in all three modes. Initial mise trust failure is retained separately;
subsequent commands use the explicit Go1.27.3 binary and matching GOROOT.
The pre-fix full race and read-only Slipstream/Starshine cross-check are retained
in ../combined-store-hook-gates/FINDINGS.md and sibling audit artifacts.

This is a scoped repair, not full combined-head/race qualification or final review.
No benchmarks, performance measurements, push or PR. Sensitive path: end-of-
invocation/construction lifetime hook; independent leased measurement should
cover numeric/no-GC calls, host reentry and reference boundary publication.

Final targeted result: PASS under the race detector in ordinary, wagodebug and
wago_regalloccheck (three commands, exit0). Full combined rerun remains open.
