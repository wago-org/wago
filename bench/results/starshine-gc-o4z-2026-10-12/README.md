# Starshine GC `-O4z` on Wago PR 948, 2026-10-12

## Result

The reported Wago run took about 176 seconds. On this machine, the pinned PR baseline took **164.554 seconds** at the median of three runs. The selected Wago changes took **92.825 seconds**, a **43.6% wall-time reduction** (1.77× throughput). Both versions produced identical, valid 5,478,001-byte wasm output. [All six samples](samples.csv) are included.

| Phase | Before median, s | After median, s | Change |
| --- | ---: | ---: | ---: |
| Read input | 2.554 | 2.720 | +6.5% |
| Decode | 16.763 | 14.908 | -11.1% |
| Main pass pipeline | 0.888 | 0.694 | -21.8% |
| Final validation | 30.082 | 11.346 | -62.3% |
| Encode | 5.499 | 3.733 | -32.1% |
| Size portfolio | 58.730 | 32.457 | -44.7% |
| Candidate selection | 46.373 | 23.569 | -49.2% |
| Write output | 0.418 | 0.424 | +1.4% |
| Whole command | 164.554 | 92.825 | -43.6% |

Compile and instantiate time was about 1.17 seconds in each version. The invoked optimizer accounts for almost all wall time. Phase medians are calculated separately, so they do not add to the whole-command median. Starshine's trace timer has millisecond resolution. It reports `strip-debug` as 0 ms and the main pipeline as less than one second; this input skips many artifact-level optimization passes. Validation and size selection are the costly work here.

The baseline CPU profile sampled 182.96 seconds in a 192.44-second profiled run. The native-to-Go host loop had 140.98 seconds of cumulative samples (77.1%); GC struct helper dispatch had 81.18 seconds (44.4%), and typed struct reads had 30.71 seconds (16.8%). Profile phase labels put 40.9% of samples in size portfolio, 26.6% in candidate selection, and 16.5% in final validation. These are overlapping call stacks, so their percentages must not be added. Repeated reference reads crossed Wago's internal GC helper boundary during validation and size exploration.

The bulk CLI protocol is separate from those internal transitions. Without tracing, Starshine made 19 request/response pairs: one argument query, 15 environment queries, one file-existence query, one bulk read, and one bulk write. The read and write therefore used four host-function calls in total. The no-trace run took 93.244 seconds and produced the same output. Phase tracing added 106 stderr operations and used 124 request/response pairs. Guest host I/O call count does not explain the 164-second baseline.

## Changes and remaining cost

[The amd64 getter](../../../src/core/compiler/backend/railshot/amd64/gc_direct.go) now reads a final struct's four-byte collector reference through the checked native object resolver. It publishes the child as an exact operand root. Reference writes, open structs, function references, and external references keep their checked helper paths. [The resolver pin](../../../src/core/compiler/backend/railshot/amd64/control.go) survives ordinary stack canonicalization, so a later register allocation cannot overwrite a valid raw-address certificate.

[The root plan](../../../src/core/compiler/backend/railshot/shared/gc_frame_roots.go) keeps each collector-reference local's source type after frame-liveness compaction. A local can be dead in its frame while a value loaded from it stays live on the operand stack across an allocation. A reduced decoder tuple failed under Tiny collection before this fix and passes now. The serialized exact-root map grows from 3,337,733 to 3,340,841 bytes; both versions report 33,724 safepoints and 101,526 callsites. Collection remains enabled.

[Subtype checks](../../../src/core/runtime/gc/native/subtype_interval.go) now walk descriptor pointers. [Promotion planning](../../../src/core/runtime/gc/native/collect.go) calculates each rounded size-class key once and skips stable sorting when plans are already ordered. Three-sample mixed-size microbenchmarks improved from 303.3 to 258.2 µs for immediate promotion and from 342.1 to 293.7 µs for survivor aging, with unchanged allocation counts. A shallow subtype check improved from 14.60 to 12.56 ns. The whole-command comparison measures the combined changes; it does not assign a separate whole-command gain to each one.

The final CPU profile sampled 91.84 seconds. The host loop still has 64.37 seconds of cumulative samples (70.1%); GC struct helper dispatch has 43.43 seconds (47.3%). Initialized struct construction has 14.54 seconds (15.8%), and typed helper reads have 9.02 seconds (9.8%). Generated native code itself has 22.69 seconds of flat samples (24.7%). The remaining 93-second wall time is real. Broader native allocation and open-type read paths would need a separate root, type, and barrier proof.

Go cumulative allocation traffic rose from 5,625.15 to 5,632.57 MB at the median (+0.13%); malloc count rose from 2,070,521 to 2,134,920 (+3.11%). Median Go GC cycles stayed at 22. These are Go process counters, not Wago collection counts or peak memory measurements.

## Reproduction and checks

- Baseline Wago source: `ef57364b`; selected source: `32a118b3`. Both host binaries use the same bulk CLI bridge, Go 1.27.1, Linux/amd64, and explicit bounds checks. Host SHA-256: baseline `474fa6f03c579fd51b95667da3f0cf03ed94d69b4fe87eee27e0d3a194453712`; selected `67ec7d43739a12f113525906d1dc9671acbc9bdf1dcd987b0fa31b8678164c02`.
- Machine: AMD Ryzen 7 8845HS, 16 logical CPUs. Runs were sequential: baseline 1, selected 1, baseline 2, selected 2, baseline 3, selected 3. The CPU profiles are separate diagnostic runs. No core affinity or peak RSS measurement was used.
- Optimizer wasm SHA-256: `4488d8db0967e3f4c56d3b68844f72d769aa01f4b5d6759d5b74ad5dba39cd91`. Input debug wasm: 13,324,080 bytes, SHA-256 `bbd21e1ce934ae41b83c61c4c15e806d4123d473f7daec11b450ccd001e6f44e`. Every output SHA-256: `f9424eb5f4b7618aeef5f8ab4ec1143c2510ce782f7e7a9f19e9f3dc4988549e`.
- Command: `<bulk-host> optimizer.wasm <debug-input.wasm> -O4z --out <output.wasm> --tracing phase`. The host used Core V3, a 64 MiB native stack, a 2 GiB Throughput heap, and a 64 MiB nursery. Exact native-root admission was required and true. Output validation used `wasm-tools 1.251.0 --features all`.
- Full `go test ./...`, `go test -tags=wago_regalloccheck ./...`, guard-page runtime/Wago tests, and nested bench module register-allocation tests passed with WABT 1.0.41. Fresh and serialized reference-read tests ran with active Throughput and Tiny collection. The reduced tuple test was red before the root fix and green after it.
