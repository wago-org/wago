# Prepared semantic adapter schedules

The Wago and wazero corpus execution benchmarks use their own prepared semantic
adapters. The separate semantic oracle runner does not exercise these adapters.
`TestPreparedSemanticSchedule` qualifies their scalar and vector call schedules
with an import-free guest that records completed calls and all three arguments.

The fixture covers signed scalar arguments, explicit vector buffer offsets,
pointer-export overrides, zero-length vectors and repeated operation on one
instance. Input bytes are checked independently. Pointer setup has separate
counters and must happen once. The guest stores a bounded ring of 16 argument
tuples; tests cross its wrap boundary. After long benchmark runs, the oracle
checks the last 16 tuples, not the entire history. The completed-call counter
is 32 bits and is compared modulo 2^32. Normal duration-calibrated benchmark
runs stay below that limit; extremely large fixed-count runs do not prove an
exact total across counter wrap.

Omitted, duplicated, reordered and wrong-argument schedules run through the real
adapters and must fail for the expected count or argument mismatch. A duplicate
that preserves the total count ensures the count alone cannot qualify a row.

`guest-calls/op` in existing `Exec` and `WazeroExec` rows states the prepared
schedule length. Scalar operations contain one guest call. Semantic vector
operations contain the complete case-set; the current BLAKE3 rows contain 35
calls with different input lengths. Their historical `ns/op` remains per case-set.
Do not divide that value by 35 and present it as a fixed-work call latency.
Metadata is reported after timing; it does not add counters to ordinary calls.
The fixture qualifies the adapter implementation, not every production call's
dynamic outcome or the compiler/API/artifact identity of a published row.

`BenchmarkPreparedSemanticSchedule` uses the same adapters and recording guest.
It checks state before and after timing and reports allocation traffic per
complete operation. Its numbers include guest recording and are synthetic.
Compile, instantiate, input preparation, warmup, checking and close are outside
the timer. These are repeated calls, not cold startup or first-call timings.

```sh
go test ./bench/suite -run '^TestPreparedSemanticSchedule'
go test ./bench/suite -run '^$' -bench '^BenchmarkPreparedSemanticSchedule$' -benchmem
```

This is a bounded contribution to #826. Cache/API/compiler-path attribution,
first-call phases and native ARM64 timing remain separate qualifications.
The batched allocation-unit correction from merged #878 is inherited from main;
this change adds schedule qualification and call-count metadata. Native CI
qualifies execution correctness rather than benchmark latency.
