# #917: compact passive element segments — phase-0 coverage stop

Base: `origin/main` `209e448c392510a0325d5b282a0d86a776fb379c`.

The focused `TestPassiveElementCoverage` compiles five real applications, AssemblyScript output, and a WasmGC fixture. It records passive state slots, entries, expanded bytes, and constant funcref/null candidates. Run:

```
GOCACHE=/tmp/wago-go-cache go test ./src/wago -run '^TestPassiveElementCoverage$' -count=1 -v
```

SQLite, QuickJS, jq, PHP, Lua, and AssemblyScript answer each have **zero passive element state slots and zero expanded bytes**. The GC array init fixture has two state slots, two entries, 64 expanded entry bytes plus 32 descriptor bytes, with one syntactically simple candidate segment. The fixture is tiny and does not demonstrate a physical page saving. This survey cannot quantify real `table.init` frequency because there are no passive segments in the six application/toolchain samples. It does not scan every available WasmGC workload; broader corpus coverage may change the outcome.

At instantiation, `instantiate.go` eagerly materializes every passive segment. The descriptor is consumed by native `table.init` and `elem.drop`; the GC array path can require element state during construction. A compact compiled representation would need a new safe runtime representation or on-demand bridge preserving per-instance canonical references, partial copies, dropped lengths, and trap order. Because the sampled real programs allocate **zero bytes** for this feature, the issue's real-benefit stop rule applies before changing this native/runtime ABI.

Correctness: the focused test passes. Production code and native output are identical to base. A short negative-control instantiation benchmark of passive **externref** entries (outside the proposed funcref admission class) with `GOCACHE=/tmp/wago-go-cache go test ./src/wago -run '^$' -bench '^BenchmarkRuntimeInstantiatePassiveExternrefElements$' -benchtime=50ms -count=2 -benchmem` gives base `5777, 5196 ns/op` and head `5620, 5304 ns/op`; all samples are `1368 B/op`, `8 allocs/op`. Differences are run noise, not an optimization effect. Native bytes and compile cost are unchanged; peak RSS and resident pages were not measured. For the six real programs, the relevant per-instance expanded entry and descriptor allocation is zero on both revisions.

Recommendation: leave draft and do not implement on-demand segments based on this corpus. A larger real module with a sizable, mostly unused constant passive funcref segment is needed to justify the runtime complexity.
