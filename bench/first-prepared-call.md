# First prepared-call lifecycle

`BenchmarkFirstPreparedCall` extends the lifecycle diagnostics with a small,
import-free guest. The compiled module is reused within each benchmark trial.
Every operation creates 16 distinct instances and prepares one `WasmFunc` for
each. Inputs are `17*i+7` for instance indices 0 through 15.

| Row | Timed work per operation | Work outside the timer |
|---|---|---|
| `first` | One `WasmFunc.Invoke` per fresh instance | Compile, instantiate, export preparation, host state checks, close |
| `second` | One `WasmFunc.Invoke` per instance, after one warm call each | The same setup and checks, plus the first call, close |
| `lifecycle` | Instantiate, prepare, first call and close for all 16 instances | Compile and host state/result checks |

The timed invocation loop includes error and result-count checks and copies each
returned value before another call can reuse its storage. Both isolated rows use
the same instance count, input order and call loop. The second row clears the
saved warmup results before timing. The lifecycle row uses three non-overlapping
timed intervals so its correctness checks remain outside the measurement.

The first row has no prior guest invocation on those instances. Host memory
reads verify zero state without invoking a getter. Export preparation and host
memory/lease checks have already occurred. This is not cold-process startup,
cold compilation, cold CPU caches, or the first API access to the instance.
The benchmark driver may run a preliminary trial; runtime and allocation caches
can be warm, and native mappings can be reused. Existing startup/end-to-end
benchmarks remain separate; do not add these isolated rows to their times.

The guest increments a counter and stores and returns `(input XOR 90)+counter`.
The host checks every instance's counter, stored value and copied return value.
Warmed or reused instances, duplicate prepared handles and corrupted return
values must fail for the intended reason. These controls qualify fresh-instance
state, not compiler-cache identity, actual native compiler path, or an independent
API-path observer. Those broader #826 contracts remain open.

Standard `ns/op`, `B/op` and `allocs/op` all remain **per 16-call batch**.
`calls/op=16` states the multiplier. `ns/call` is the batch average, including
the loop/check/copy cost; for `lifecycle` it also includes amortized setup and
close. It is not a distribution of individual call latencies. `linear-capacity-B`
is logical guest memory capacity, not committed pages, retained memory or RSS.
`native-B` is compiled guest code size. Go allocation traffic excludes native
memory and must not be read as total memory use.

The diagnostic is opt-in and requires an explicit iteration count from 1 to 256.
This bounds instance setup even when the timed calls are short. Invalid duration
or excessive-count settings fail before compiling or creating an instance.
At most 16 instances are live at once, and partial setup is cleaned up on error.
Use independent alternating baseline/candidate samples and retain spread.

```sh
go test ./bench/suite -run '^TestFirstPrepared'
go test ./bench/suite -run '^$' -bench '^BenchmarkFirstPreparedCall$' \
  -wago.bench.first-prepared -benchtime=64x -count=7 -benchmem
```

The fixture is synthetic. It can expose first-use cost but does not establish a
production workload speedup, a break-even invocation count, or native ARM64
qualification. The benchmark logs the exact fixture digest, input size, function
count and native size for each trial. Source revision, compiler binary, target
and build settings still belong in the measurement record.
