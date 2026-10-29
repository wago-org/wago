# Native GC instruction timing

The vector fixture measures `array.get` and `array.set` with `v128` elements.
Each operation runs 8192 guest loop iterations. Both engines check the returned
value. Compilation and instantiation are outside the samples; V8 warms up for
250 ms before each five-sample series. Wago retains its public invocation cost.

Run from the repository root. Use one CPU for both engines, and keep other
work off that CPU. This example uses CPU 15; choose an allowed CPU on your host.

```sh
go test -c -o /tmp/wago-vector.test ./src/wago
cd src/wago
GOMAXPROCS=1 taskset -c 15 /tmp/wago-vector.test -test.run='^$' \
  -test.bench='^BenchmarkGCVectorArrayAccess$' -test.benchtime=200ms \
  -test.count=5 > /tmp/wago-vector.txt
cd ../..
taskset -c 15 node bench/gc-native/vector-access.mjs > /tmp/v8-vector.json
node bench/gc-native/compare-vector.mjs /tmp/wago-vector.txt /tmp/v8-vector.json
```

The comparison uses medians and a ratio target of 1. It prints both engine
timings and fails visibly when Wago is slower. A failure is a performance gap,
not a correctness failure. Do not replace it with a timeout or hide it in CI.
These loops isolate two instructions; they do not establish compiler parity.
