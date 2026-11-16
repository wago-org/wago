# Payload-boundary refactor comparison

Source/protocol only; do not run builds or timings without the shared CPU window.
Baseline is `bb0c4e3` plus the **identical**
`src/core/runtime/gc/native/payload_access_bench_test.go` from the candidate.
Candidate contains only the payload refactor and its correctness fixes relative
to that baseline, plus documentation/tests. Use distinct worktrees, not a live
checkout switched underneath another worker.

Build each binary before entering the timing window:

```sh
GOCACHE=/workspace/shared/go127-cache GOPATH=/workspace/shared/go-path \
  /workspace/shared/toolchains/go127/bin/go test -p 1 -c \
  -o /workspace/shared/gc-payload-evidence/baseline.test ./src/core/runtime/gc/native
# Same command in candidate worktree, output candidate.test.
```

Record both exact source commits (and baseline benchmark-source addition), Go
version and benchmark source SHA256. Then, with all known workers idle:

```sh
python3 bench/experiments/gc-payload/compare.py \
  /workspace/shared/gc-payload-evidence/baseline.test \
  /workspace/shared/gc-payload-evidence/candidate.test \
  /workspace/shared/gc-payload-evidence/results.json
```

Three fresh-process pairs alternate order. Each executes the same fourteen
Throughput/Tiny access/constructor/borrow subcases at 100ms benchmark target with
GOMAXPROCS=1. Raw complete stdout/stderr, binary hashes and process exit status are
retained. Shared-host noise is uncontrolled. Compare per-case process medians and
allocations, not an aggregate score or V8 claim. Constructor timing includes real
collector work, unlike a pure access loop; preserve that distinction.
