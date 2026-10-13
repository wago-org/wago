# Prepared semantic adapter correctness checks

`TestPreparedSemanticSchedule` exercises the existing prepared Wago and wazero
adapters with a small import-free guest. The guest records completed calls and
all three arguments in a bounded ring. Independent expected tuples check the
adapter schedule; the separate corpus oracle runner does not exercise these
prepared adapters.

The fixtures cover signed scalar arguments, explicit vector buffer offsets,
pointer-export overrides, zero-length vectors, and repeated operations on one
instance. Tests check the prepared input bytes independently. Separate counters
require each pointer export to run once during preparation. Each fixture runs
nine operations. The vector fixtures cross the 16-entry ring boundary.

The observer checks the completed-call count and the retained argument tuples.
Omitted, extra, reordered, and wrong-argument schedules must fail for the
expected count or argument mismatch. A duplicated tuple with the correct total
count must also fail, so the work count alone cannot qualify a schedule. These
controls use the real prepared adapters in both engines.

The completed-call counter is 32 bits and is compared modulo 2^32. After a long
synthetic run, the observer checks the last 16 tuples rather than the entire
history. Extremely large fixed-count runs do not prove an exact total across
counter wrap. The checks qualify these adapter fixtures, not the compiler or
artifact identity of published benchmark results.

`BenchmarkPreparedSemanticSchedule` is a separate synthetic diagnostic with the
same recording guest. It checks state before and after timing. Compile,
instantiate, input preparation, warmup, checks, and close are outside the timer.
Guest recording is inside the timer. Standard timing and allocation units cover
one complete fixture operation: one call for the scalar fixture or three calls
for either vector fixture. Its `guest-calls/op` field states that fixture unit.
This field is confined to this diagnostic; ordinary `Exec` and `WazeroExec`
rows retain their existing output. The diagnostic does not establish production
latency or a performance change.

```sh
go test ./bench/suite -run '^TestPreparedSemanticSchedule' -count=1
go test ./bench/suite -run '^$' -bench '^BenchmarkPreparedSemanticSchedule$' \
  -benchtime=4x -benchmem
```

This is bounded correctness coverage for #826. Cache/API/compiler-path
attribution, first-call phases, and native ARM64 timing remain separate work.
Native CI checks execution correctness rather than benchmark latency.
