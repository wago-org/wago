# Unresolved TinyGo standalone interruption abort

The initial runtime-tagged debug CLI suite failed
`TestTinyGoSupportsCooperativeInterruption` in
`cli/manager/internal/standalone/build_linux_test.go:71`:

```
TinyGo standalone interruption: signal: aborted (core dumped)
    panic: runtime error: invalid memory address or nil pointer dereference
```

This is a generated executable failure, not a Go test compilation failure.
The test builds a standalone runner using `-scheduler=threads -gc=conservative`
and checks cancellation of an infinite guest from another host thread.
It does not enable NurseryRegionShift. Its source is byte-identical between
baseline 9713878ab452848f7eb7549a90be94b938a898be and current snapshot
d546a1af4513c63e4bda6fae7d19cf9cd86f332a (empty tinygo-test-baseline.diff).
Root debug and ordinary/checked CLI runs passed this case, so it is intermittent
in this environment. A later passing probe cannot erase the observed abort.

The host provides TinyGo 0.43.0 / LLVM 22.1.4, not CI's pinned TinyGo 0.42.0.
Go/GOROOT remain 1.27.1. Exact binary hash is in tinygo-provenance.txt.
No toolchain source or production code was patched.

Matched probe: reproduce.sh runs the same test with runtime/debug tags,
-count=3, GOMAXPROCS=2, GOFLAGS=-p=2 and the same installed TinyGo on each
immutable export. All three baseline repetitions passed. All three current repetitions also passed. Results
are retained separately and summarized in summary.json/results.txt.
This does not demonstrate a Snap regression or establish a pre-existing
baseline defect. Triage remains unresolved, and aggregate qualification must
not be reported unconditionally green.

Read-only sibling cross-check, before triage, is in sibling-audit.txt.
Slipstream HEAD 781e53d2e10ebe8d9ac415c350f25081c6127b1b includes a Wago
standalone/TinyGo test manifest explicitly marking the analogous artifact
behavior not applicable to its process-CLI surface. Starshine checkout HEAD
1d6c0af47140299d8f3c01ed03060d279c43ee03 has no matching cooperative-interruption
or TinyGo-artifact execution test in the inspected src/runtime/tests paths.
Neither supplies a directly comparable TinyGo-generated standalone runtime.
No sibling execution, mutation, or performance inference was performed.
