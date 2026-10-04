# Corpus fixes paused for incoming upstream work

These changes are preserved for review, not merged into main or used to replace beta.11 measurements.

- Decoder owned byte/string allocations now charge one allocation plus rounding, rather than twice the buffer size. Swift's original decoder-budget rejection is resolved.
- ARM64 metadata loads handle large offsets, including a 11,537-global get/set regression. Swift's exact stdout oracle passes on ARM64.
- ARM64 fused compare/local.tee/br_if lowering reconciles the assigned local on the branch edge and keeps its reconciliation out of deferred cold fragments. Clang's LLVM output oracle passes with default optimizations.

ARM64 validation passed: src/wago, ARM64 backend, and Wasm decoder tests; exact output checks for clang, Swift, age (with the companion WASI fix), and source-rebuilt Lua using standardized exception handling.

AMD64 remains incomplete: Swift now reaches a GP-register exhaustion error in function 120226 at offset 0x53. Initial AMD64 src/wago tests also lacked the pinned spec-v3 submodule fixture. Work was stopped at the user's request pending an incoming PR. No performance results from this development build replace the released engine's evidence.
