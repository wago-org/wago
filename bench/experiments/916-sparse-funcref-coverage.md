# #916: sparse canonical funcref descriptors — phase-0 stop

PR production baseline: branch parent `8097344a63a79e96230e09c883b2138232151370`. The diff from that parent adds only the survey test and this record; it changes no production source. Historical timing samples below used `209e448c392510a0325d5b282a0d86a776fb379c` as their recorded baseline.

`go test ./src/wago -run '^TestSparseFuncRefCoverage$' -count=1 -v` scans decoded function bodies for `ref.func`, compiles each module with the supported subset of Core V3, and counts *simple* function targets in both active (`c.Elems`) and passive (`c.passiveElems`) compiled element payloads. One shared target set removes duplicates across the two slices; active and declarative state slots have empty payloads. The last two columns estimate 4 KiB descriptor pages touched if only those element targets were populated. They assume a page-aligned descriptor start, so actual arena offsets can shift a boundary; export, global, expression, and host paths can only increase the required set.

The eight rows below retain the measured Linux results. On targets without native GC, the two GC fixtures and the GC passive-element regression are explicitly skipped. A skip supplies no page measurement and is not reported as zero pages. The other six fixtures and the descriptor-span test remain enabled. This portability correction adds no performance measurements.

| Module | Functions | Dense bytes | Unique simple element targets | Body `ref.func` sites | Element-touched / directory pages |
| --- | ---: | ---: | ---: | ---: | ---: |
| SQLite | 1,574 | 63,000 | 523 | 0 | 16 / 16 |
| QuickJS | 915 | 36,640 | 414 | 0 | 9 / 9 |
| jq | 738 | 29,560 | 206 | 0 | 7 / 8 |
| PHP | 12,635 | 505,440 | 6,505 | 0 | 122 / 124 |
| Lua | 797 | 31,920 | 200 | 0 | 7 / 8 |
| GC constexpr roots fixture | 1 | 80 | 0 | 0 | 0 / 1 |
| GC array init fixture | 2 | 120 | 1 | 0 | 1 / 1 |
| TinyGo answer | 7 | 0 | 0 | 0 | 0 / 0 |

The two GC fixtures need descriptors but fit within one page. Numeric-only TinyGo already omits the directory. SQLite and QuickJS touch every descriptor page, while the best case among the five larger applications omits two 4 KiB pages before additional required references. Since the retained dense directory remains reserved, this sparse-population approach cannot deliver a material physical-memory saving in these samples. The issue's explicit stop rule applies; no production materialization path was introduced.

Existing canonical descriptor observers in `instantiate.go`, `reference_store.go`, `instance_call.go`, GC constexpr handling, globals, and native `ref.func` addressing mean a policy must account for body references and late host requests before omitting a record. Simple element targets alone are not an authoritative required set. In particular, `localFuncrefDescriptor` and `FuncRefMatchesFunction` can inspect canonical slots after instantiation. A sparse prototype based only on this survey would be unsound.

Correctness: the coverage test passes. Production source is unchanged against the actual PR parent. No generated-code comparison against that parent was measured. Historical short benchmark samples (`GOCACHE=/tmp/wago-go-cache go test ./src/wago -run '^$' -bench '^BenchmarkRuntimeInstantiateFuncrefIngressCaller$' -benchtime=50ms -count=2 -benchmem`): historical baseline `209e448c` `6241, 6509 ns/op`, historical experiment `6847, 6630 ns/op`; both `1480 B/op`, `12 allocs/op`. The exact experiment binary identity is not retained in this record. Production source differs between `209e448c` and the actual PR parent, so these samples are not an unchanged-code control for this PR. The samples are noisy and do not establish a PR speedup or regression. This benchmark is a small synthetic ingress caller, not the surveyed real applications. Native bytes and compile cost against the actual PR parent were not measured; peak RSS and physical page residency were not directly measured. Page counts are a conservative directional proxy for the maximum opportunity from sparse writes, not measured RSS.

Recommendation: the completed static survey is ready for review. Do not enable sparse population from this evidence. A separate compact descriptor layout could reduce reserved bytes, but that changes the ABI and is outside #916.

Review correction: descriptor-page coverage now includes both the first and last byte of each 40-byte descriptor. The descriptor at byte 4080 crosses a 4 KiB page boundary; a focused test fails with first-page-only counting and passes with the corrected span. The earlier span-only recheck left all eight module counts unchanged. Page-aligned placement remains an explicit estimate, with no production descriptor change.

Passive-element correction: the scan now includes retained passive payloads as well as active initializers. The GC array init fixture has one simple `ref.func 0` target and touches one descriptor page; the other seven survey rows are unchanged. `TestSparseFuncRefPassiveElementCoverage` fails with active-only counting and passes with the shared active/passive target set. This correction does not change the stop result for the five larger applications.

Repeat-marker correction: a table initializer can retain an expression once and
fill later slots with `RepeatPrevious` markers. These markers have no independent
function index and are excluded from the simple-target count. A focused regression
checks that the expression and its repeat marker do not invent a function-zero
target. This changes no production representation or materialization path.
