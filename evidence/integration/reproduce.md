Frozen source60481522292483cf94a116b17a68c69a10a8c737; native GNU objdump2.44,
Go1.27.1. Use GOMAXPROCS1, -p1, private GOCACHE; no installs. Baseline currentmain
05af869e and old portable-tool baselinea53e245b are detached sibling worktrees.

Build both baseline/candidate from their respective roots:

```sh
GOMAXPROCS=1 go test -p=1 -c -trimpath -buildvcs=false -o /tmp/wago-815-integration-SIDE.test ./src/core/compiler/backend/railshot/amd64
GOMAXPROCS=1 go build -p=1 -trimpath -buildvcs=false -tags=wago_runtime -o /tmp/wago815-runtime-SIDE ./cli/wago
GOMAXPROCS=1 go test -p=1 -c -trimpath -buildvcs=false -o /tmp/wago815-portable-SIDE.test ./tests/tools/native-compare
```

Replace SIDE with before/after; portable-before uses a53, which contains the
original tool, rather than main (which has no tool). From candidate root:

```sh
GOMAXPROCS=1 go test -p=1 -c -trimpath -buildvcs=false -tags=wago_profile,wago_regalloccheck -o /tmp/wago815-integration-diagnostic.test ./tests/tools/native-compare
GOMAXPROCS=1 go build -p=1 -trimpath -buildvcs=false -tags=wago_profile -ldflags '-X main.compiledRevision=60481522292483cf94a116b17a68c69a10a8c737' -o /tmp/wago815-integration-tool ./tests/tools/native-compare
python3 evidence/integration/run-measurements.py
```

Run the profiled diagnostic test binary from tests/tools/native-compare because
it uses relative fixture paths. `-test.bench=^BenchmarkCompare` uses100x/count3;
`-test.bench=^BenchmarkCaptureExistingFib$` uses5x/count3, CPU15/GOMAXPROCS1.
Paired portable-tool benchmarks use100x/count1 three alternating before/after
pairs with the same BenchmarkCompare selection and CPU15. Initial path mistakes
in continuation commands affected no benchmark samples; resulting standalone
artifacts and test replay were verified from their correct paths.
