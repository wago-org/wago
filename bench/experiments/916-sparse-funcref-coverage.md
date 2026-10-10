# #916: sparse canonical funcref descriptors — phase-0 stop

Base: `origin/main` at `209e448c392510a0325d5b282a0d86a776fb379c`.

`go test ./src/wago -run '^TestSparseFuncRefCoverage$' -count=1 -v` scans decoded function bodies for `ref.func`, compiles each module with Core V3, and counts *simple* function targets in compiled element segments. The last two columns estimate 4 KiB descriptor pages touched if only those element targets were populated. They assume a page-aligned descriptor start, so actual arena offsets can shift a boundary; export, global, expression, and host paths can only increase the required set.

| Module | Functions | Dense bytes | Unique simple element targets | Body `ref.func` sites | Element-touched / directory pages |
| --- | ---: | ---: | ---: | ---: | ---: |
| SQLite | 1,574 | 63,000 | 523 | 0 | 16 / 16 |
| QuickJS | 915 | 36,640 | 414 | 0 | 9 / 9 |
| jq | 738 | 29,560 | 206 | 0 | 7 / 8 |
| PHP | 12,635 | 505,440 | 6,505 | 0 | 122 / 124 |
| Lua | 797 | 31,920 | 200 | 0 | 7 / 8 |
| GC constexpr roots fixture | 1 | 80 | 0 | 0 | 0 / 1 |
| GC array init fixture | 2 | 120 | 0 | 0 | 0 / 1 |
| TinyGo answer | 7 | 0 | 0 | 0 | 0 / 0 |

The two GC fixtures need descriptors but fit within one page. Numeric-only TinyGo already omits the directory. SQLite and QuickJS touch every descriptor page, while the best case among the five larger applications omits two 4 KiB pages before additional required references. Since the retained dense directory remains reserved, this sparse-population approach cannot deliver a material physical-memory saving in these samples. The issue's explicit stop rule applies; no production materialization path was introduced.

Existing canonical descriptor observers in `instantiate.go`, `reference_store.go`, `instance_call.go`, GC constexpr handling, globals, and native `ref.func` addressing mean a policy must account for body references and late host requests before omitting a record. Simple element targets alone are not an authoritative required set. In particular, `localFuncrefDescriptor` and `FuncRefMatchesFunction` can inspect canonical slots after instantiation. A sparse prototype based only on this survey would be unsound.

Correctness: the coverage test passes. Production source and generated code are identical to base. Short control benchmark (`GOCACHE=/tmp/wago-go-cache go test ./src/wago -run '^$' -bench '^BenchmarkRuntimeInstantiateFuncrefIngressCaller$' -benchtime=50ms -count=2 -benchmem`) on the same host: base `6241, 6509 ns/op`, head `6847, 6630 ns/op`; both `1480 B/op`, `12 allocs/op`. The timing spread is noise and does not represent a speedup or regression from the test-only branch. This benchmark is a small synthetic ingress caller, not the surveyed real applications. Native bytes and production compile cost are unchanged; peak RSS and physical page residency were not directly measured. Page counts are a conservative directional proxy for the maximum opportunity from sparse writes, not measured RSS.

Recommendation: leave draft and do not enable sparse population. A separate compact descriptor layout could reduce reserved bytes, but that changes the ABI and is outside #916.
