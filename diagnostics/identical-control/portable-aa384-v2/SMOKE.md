# Source build and correctness receipt

The reviewed portable v2 kit built successfully on the first attempt
from the pinned Wago and Wasmbench sources. Go 1.27.1 on Linux/AMD64,
CGO disabled, PGO off, T402000/R4096, all 20 runtime experimental flags
off. CGO was enabled for the historical adapter build; this is a
new source-built binary and not historical binary parity.

- Kit checksum manifest SHA-256: adc84449cb8c3b06e8a9010898237a66098f0790e36c9466d40868cbe7672dd5
- Input manifest SHA-256: 5e91ae8d36e9e0d47790136856cc1072a5b0cbc643986dd4c9d23fee05b0b7a1
- Local adapter SHA-256: 19a11a8c2ab16ed13cf4c8de66ef86fc39d0ca681a02e07452a8fc049603e561

Exactly four ordinary correctness probes verified their oracles:
tiny/add, fib_rec/fib, many_funcs/run, and pcre2/compile-match.
The smoke exercises the describe and compile protocol, not full API
or capability parity. Three inputs are unchanged and many_funcs
replaces CoreMark, so v2 is not an exact replication. No 384-record
A/A run or timing analysis was performed. No executables or Wasm
inputs are distributed here.
