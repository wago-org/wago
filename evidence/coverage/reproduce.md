# Reproduce the bounded experiment

Source under measurement: `66dedc1decfe9061b60a3b42101400950915a7c3`.
Previous diagnostic: `8e82e1b26e0b5ac015b761f0c4bf5c9c894fb0b2`.
Final production control: verified main `e4bcc5244a29ddf4028fdeb3a7255ec8c76d810f`.
Initial production control c95243aa is retained in the historical validation record.
Later commits add documentation/evidence only. Historical integration measurements
use older bases and are explicitly superseded where acceptance conclusions differ.

Use separate isolated checkouts, existing Go/GNU objdump, private cache, no concurrent
builds during measurement, GOMAXPROCS=1 and a permitted CPU (15 here). Do not reuse
another agent's checkout. No installation or credential changes are needed.

Build in the previous and candidate checkouts respectively, using before/after:

```sh
GOMAXPROCS=1 GOCACHE=/tmp/wago-815-cache go test -p=1 -trimpath -buildvcs=false \
  -c -o /tmp/wago815-coverage-after.test ./tests/tools/native-compare
GOMAXPROCS=1 GOCACHE=/tmp/wago-815-cache go test -p=1 -trimpath -buildvcs=false \
  -tags=wago_profile,wago_regalloccheck -c \
  -o /tmp/wago815-coverage-profile-after.test ./tests/tools/native-compare
```

Finish both builds, then run `python3 evidence/coverage/run-pairs.py` from the
candidate checkout. It alternates five portable 100-iteration pairs and three
profile pairs (100 comparisons, five captures); it overwrites local result files.
Profile benchmarks run in the package directory for the repository fixture path.
Save binary SHA256, Go/objdump/host versions and load with every new run. This small
sample is not a long saturated campaign or a confidence-interval estimate.

Build a frozen capture command and save actual producer outputs:

```sh
GOMAXPROCS=1 GOCACHE=/tmp/wago-815-cache go build -p=1 -trimpath -buildvcs=false \
  -tags=wago_profile -ldflags '-X main.compiledRevision=66dedc1decfe9061b60a3b42101400950915a7c3' \
  -o /tmp/wago815-coverage-tool ./tests/tools/native-compare
/tmp/wago815-coverage-tool capture tests/fixtures/wasm/fib.wasm /tmp/fib-before.json
/tmp/wago815-coverage-tool capture tests/fixtures/wasm/fib.wasm /tmp/fib-after.json
/tmp/wago815-coverage-tool compare /tmp/fib-before.json /tmp/fib-after.json
```

These are same-producer self-controls. They do not claim compiler-before/after
native differences. The producer native hash covers the actual emitted image;
synthetic record-change controls explicitly do not have that attestation.
Compile time, execution and runtime code size remain historical paired controls
plus fresh byte-identical current-main production builds. Capture timing includes
hashing/objdump and excludes JSON/files: it is not compilation time alone.
B/op is allocated bytes, not peak/RSS/retained memory.
