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
