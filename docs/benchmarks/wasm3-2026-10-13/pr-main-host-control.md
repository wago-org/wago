# Default-main legacy host-call control

Apple M4 Max, Go 1.27.1, GOMAXPROCS=1, `BenchmarkInvokeHostFuncDirect`,
five 200 ms samples per source/order. Compare default main `8097344a` against
the branch with the PR cleanup changes (subsequently committed through
`66602322`). Run sources in main/current and current/main order under the
shared device reservation, with no concurrent compilation or benchmark job.

The median current/main ratios are **1.07262** and **1.06306**: a **7.3% / 6.3%
regression** in this common legacy host-import control. Main median/range is
114.3 ns [111.9,128.6] then 111.0 ns [110.0,119.4]; current is 122.6 ns
[118.8,124.6] then 118.0 ns [116.1,119.4]. All samples remain in the JSON.

This is not the fixed-shared integer or compact-import SDK contract and does
not negate their separately measured deltas. It is a retained contrary control
and prevents a branch-wide regression-free or default-main speedup claim.
The complete default-main feature/compile/RSS comparison is still outstanding;
features that default main cannot execute need separate admission accounting.

After fixing foreign Caller parent propagation (`30b16166`), repeat the same
matched control in both source orders. Current/main median ratios are **1.00782**
and **1.08251** (+0.8% / +8.3%). Main median/range: 115.1 ns [109.7,116.3]
and 111.5 ns [109.3,119.4]; exact ranges in the companion JSON are authoritative.
Current medians are 116.0 ns and 120.7 ns. Both sources retain 64 B/op and one
allocation/op. The two-order spread is retained; it does not establish that the
legacy-control regression has been eliminated. The initial control remains
unchanged above. See `pr-main-host-control-parent-fix.json` for every sample.


## Guarded import retention control

The common legacy host-import benchmark was repeated at `75171913` against
default main `8097344a`, with five 200 ms samples per source in both orders,
Go 1.27.1, and `GOMAXPROCS=1` under the Apple device lock. The current/main
median ratios are **1.099x / 1.069x**. Both retain 64 B/op and one allocation.
This control remains slower than default main, and its test binary includes
the branch's additional tests. The targeted compact-import improvement does
not resolve or supersede this contrary result. The
[raw control](pr-main-host-control-guarded-imports.json) is separate from the
[matched feature SDK audit](guarded-native-imports.md).
