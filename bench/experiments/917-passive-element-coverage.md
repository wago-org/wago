# #917: compact passive element segments — phase-0 coverage stop

PR production baseline: branch parent `8097344a63a79e96230e09c883b2138232151370`. The diff from that parent adds only the survey test and this record; it changes no production source. Historical timing samples below used `209e448c392510a0325d5b282a0d86a776fb379c` as their recorded baseline.

The focused `TestPassiveElementCoverage` compiles five real applications, AssemblyScript output, and a WasmGC fixture. It records passive state slots, entries, expanded bytes, constant funcref/null candidates, and nonempty segments required by GC array instructions. It requests only `CoreFeaturesV3 & SupportedFeatures()`. On a target without native GC, only the GC fixture is explicitly skipped; the skip is not a zero-coverage result. Run:

```
GOCACHE=/tmp/wago-go-cache go test ./src/wago -run '^TestPassiveElementCoverage$' -count=1 -v
```

SQLite, QuickJS, jq, PHP, Lua, and AssemblyScript answer each have **zero passive element state slots and zero expanded bytes**. The GC array init fixture has two state slots, two entries, 64 expanded entry bytes plus 32 descriptor bytes, with zero eligible segments: its one simple payload is segment 1, required by generic `array.init_elem`, and is excluded from compact-candidate admission. The fixture is tiny and does not demonstrate a physical page saving. This survey cannot quantify real `table.init` frequency because there are no passive segments in the six application/toolchain samples. It does not scan every available WasmGC workload; broader corpus coverage may change the outcome.

| Sample | State slots | Entries | Eligible segments | GC-required segments | Expanded bytes | Descriptor bytes |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| Each of the six application/toolchain modules | 0 | 0 | 0 | 0 | 0 | 0 |
| GC array init fixture, on a native-GC target | 2 | 2 | 0 | 1 | 64 | 32 |

At instantiation, `instantiate.go` eagerly materializes every passive segment. The descriptor is consumed by native `table.init` and `elem.drop`; the GC array path can require element state during construction. A compact compiled representation would need a new safe runtime representation or on-demand bridge preserving per-instance canonical references, partial copies, dropped lengths, and trap order. Because the sampled real programs allocate **zero bytes** for this feature, the issue's real-benefit stop rule applies before changing this native/runtime ABI.

Correctness: the focused survey and GC eligibility regression pass. The scan classifies both `array.new_elem` and `array.init_elem`, using their second immediate as the element-segment index. A shared required-segment set covers generic execution, while the specialized compiled GC descriptor remains an additional exclusion. An unused simple segment remains eligible in the regression. Production source is unchanged against the actual PR parent; native output against that parent was not compared. Historical short instantiation samples of passive **externref** entries (outside the proposed funcref admission class) with `GOCACHE=/tmp/wago-go-cache go test ./src/wago -run '^$' -bench '^BenchmarkRuntimeInstantiatePassiveExternrefElements$' -benchtime=50ms -count=2 -benchmem` give historical baseline `209e448c` `5777, 5196 ns/op` and historical experiment `5620, 5304 ns/op`; all samples are `1368 B/op`, `8 allocs/op`. The exact experiment binary identity is not retained in this record. Production source differs between `209e448c` and the actual PR parent, so these samples are not an unchanged-code control for this PR. The samples are noisy and do not establish a PR speedup. Native bytes and compile cost against the actual PR parent were not measured; peak RSS and resident pages were not measured. For the six surveyed application/toolchain modules, the current per-instance expanded entry and descriptor allocation is zero.

Recommendation: the completed static survey is ready for review. Do not implement on-demand segments based on this corpus. A larger real module with a sizable, mostly unused constant passive funcref segment is needed to justify the runtime complexity.
