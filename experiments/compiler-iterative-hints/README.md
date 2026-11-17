# Iterative hint scanning

Baseline 74ab035a. Both byteBodyScanner and globalScoreByteScanner now use
explicit bounded control frames rather than native recursion. The allowed
nesting depth and immediate/error consumption remain unchanged. An inline
16-frame buffer serves shallow functions; deeper expressions grow a slice.
No latency, RSS, or TinyGo crash fix is claimed from source changes alone.

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
performance remain to be measured independently.
