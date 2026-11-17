# Iterative hint scanning

Baseline 74ab035a. Both byteBodyScanner and globalScoreByteScanner now use
explicit bounded control frames rather than native recursion. The allowed
nesting depth and immediate/error consumption remain unchanged. An inline
16-frame buffer serves shallow functions; deeper expressions grow a slice.
Measurements and qualification follow below.

The original recursive scanner bodies are retained test-only under !tinygo.
Differential tests compare full hint views including local-event tapes, sparse
global scores/eligibility, partial eligibility tracker state, entry-local state,
loop constant candidates, call/terminator returns, error fields and reader
positions. They cover 1,000 deterministic generated control trees, truncations,
nested loops/if/else/try_table and the 20,000/20,001 depth boundary. A separate
iterative-only deep test remains available under TinyGo, avoiding an oracle
stack overflow that would obscure production behavior.

An initial differential failure identified recursive error propagation's zero
terminator for nested invalid else, even though error offsets already matched;
the iterative path now preserves it. No test was weakened to accept this.

Go 1.27.1 linux/amd64 full backend suite passes (8.965s), GOMAXPROCS=2,
go test -p=1 ./src/core/compiler/backend/railshot/amd64 -count=1.
Native-code corpus equivalence, TinyGo default-stack causality, RSS and paired
performance were measured independently as recorded below.

## Measured checkpoint

Cloud Linux AMD64, Go 1.27.1 and TinyGo 0.42.0. Corpus: 132 exact
artifact hashes recovered from the live wasm.fyi 145-contract Linux manifest;
13 rebuilt artifacts remain unavailable. This is not full-corpus acceptance.
Fresh processes, single compile worker, CPU affinity, absolute `/proc` VmHWM.

Standard Go, nine alternating paired trials: mean of per-workload median
compilation times 1.051 ms baseline versus 0.984 ms iterative (6.4% lower).
Mean per-workload median peak RSS 7.584 versus 7.602 MiB. All 2,376
compilations pass and all 132 generated native-image hashes are identical.
The 40% compilation-latency target is not achieved by this result.

TinyGo default-stack iterative: all 660 compilations pass (five trials),
mean per-workload median peak RSS 2.643 MiB and compilation time 1.616 ms.
The original scanners crash on pcre2 and yyjson with the same default-stack
build flags. Increasing only the old build's stack to 2 MiB passes all 132,
but uses 6.809 MiB mean peak RSS and 3.709 ms compilation. That oversized-stack
workaround is not a fair baseline for claiming a latency improvement.
All 132 iterative TinyGo native-image hashes match the old 2 MiB-stack build.
Execution latency and additional tagged qualification are recorded below.

These measurements distinguish the stack-safety fix from the broader compiler
performance goal. Go and TinyGo must not be compared as identical toolchains.

### Additional qualification

Full AMD64 regalloc-checker and profile suites pass (13.320 s and 21.455 s).
Public GC tests pass (8.247 s) with the clean pinned Release3 spec corpus
9d36019973201a19f9c9ebb0f10828b2fe2374aa.

All 102 exact application kernel result oracles pass seven paired trials,
128 timed calls each. Aggregate mean of workload median execution times is
204.412 us baseline versus 203.611 us iterative. Eighteen initial per-workload
medians appeared over 5% slower. Retesting these with seven paired trials,
2,048 calls and an additional identical-baseline control produced no candidate
median regression above 3.2%; the identical-binary control itself reached 8.1%
on one workload. This is reassuring, not proof that cloud timing is noiseless.
The 30 other exact fixtures have compilation/native-image verification, not
this application-export execution check.

TinyGo arithmetic mean RSS across all 660 samples is 2.643 MiB; averaging
worst-trial RSS per workload instead gives 2.646 MiB. The default Go embedding
remains above the memory target. Full 145-workload acceptance is still blocked
on 13 exact artifact bytes, and the 40% latency target remains unachieved.

### TinyGo nesting-boundary test

The default-stack TinyGo 20,000/20,001 nesting-boundary test passes (0.018 s)
with unchanged production files and only the iterative depth test selected at
source level. The normal package test build is independently blocked by
pre-existing compile_test.go constant indexing of TinyGo's zero-sized scratch
array (lines 111, 125, 130), even with a test-name filter. Therefore this is an
isolated targeted test, not a full TinyGo backend-suite pass. All temporarily
excluded test sources were restored; none were removed from this branch.
