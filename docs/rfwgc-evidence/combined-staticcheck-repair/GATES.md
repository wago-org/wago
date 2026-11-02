# Final correctness gates

| Gate | Ordinary | wagodebug | wago_regalloccheck |
| --- | --- | --- | --- |
| Full GC/runtime/Wago packages | PASS | PASS | PASS |
| Focused ownership/receipt/lifecycle race | PASS | PASS | PASS |
| ARM64 cross-build | PASS | PASS | PASS |
| ARM64 actual QEMU ownership/receipt execution | PASS | PASS | PASS |
| RV64 cross-build | PASS | PASS | PASS |
| RV64 actual QEMU shape/exception/receipt/cycle execution | PASS | PASS | PASS |

Staticcheck reviewed-baseline: PASS in every configured profile. Build selection: PASS in all 18 combinations. Original archive integrity: PASS. Exact parent lifeMu hook preservation: PASS.

Each raw log, command, exit and test pass/skip/failure list is retained. Full package passes do not claim optional skipped tests ran. QEMU runs verify target ELF identity and execute the committed shape test, in addition to existing ownership oracles.

| Command | Passed tests/subtests | Skipped tests/subtests |
| --- | ---: | ---: |
| final/arm64-ordinary-execution | 172 | 0 |
| final/arm64-wago_regalloccheck-execution | 172 | 0 |
| final/arm64-wagodebug-execution | 172 | 0 |
| final/ownership-race-ordinary | 182 | 0 |
| final/ownership-race-wago_regalloccheck | 182 | 0 |
| final/ownership-race-wagodebug | 182 | 0 |
| final/packages-ordinary | 14683 | 44 |
| final/packages-wago_regalloccheck | 14683 | 44 |
| final/packages-wagodebug | 14756 | 53 |
| final/riscv64-ordinary-execution | 357 | 0 |
| final/riscv64-wago_regalloccheck-execution | 357 | 0 |
| final/riscv64-wagodebug-execution | 357 | 0 |
