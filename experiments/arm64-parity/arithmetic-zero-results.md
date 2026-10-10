# Arithmetic zero-test prototype: arm64

Default off, `arithmetic-zero-test` / `WAGO_ARM64_EXPERIMENT_ARITHMETIC_ZERO=1`. Compare SUB/XOR operands directly when their result is consumed only by eqz. Register ADD uses CMN; constant ADD compares against modular negation. Consume only Z, so signed overflow/carry cannot change the predicate. Both materialized boolean and flags/branch consumers use the same helper. Retain the existing RHS-before-LHS realization order and register ownership rules.

Tests exercise i32/i64, signed-boundary and dirty-carrier inputs, source reuse, branch consumers, negative/zero/wide constants, and per-compilation overrides. Independent CMN instruction goldens cover both widths. Correctness/impact qualification is queued through the shared device lock; no performance claim yet. The earlier borrowed-comparison-source experiment was already rejected after mitigation and is not being retried.

## Qualification and independent correctness fix

The initial qualification exposed an existing arm64 immediate-range bug with the prototype disabled: negating `INT64_MIN` overflowed and passed the small-negative-immediate check, producing an ADD of zero. `addFoldImm3` now tests `v >= -0xFFF` directly. `TestI64MinimumArithmeticConstant` verifies full-width ADD/SUB results at boundary inputs with signal and explicit bounds; this fix is independent of the proposed optimization.

After the fix, diagnostic backend/catalog/encoder tests pass with the prototype both disabled and enabled. All 46 core and 102 application signal-bounds oracle checks pass. An additional disabled core image dump confirms that all 13 changed core images are due to the prototype, rather than the correctness fix. The only changed application is compiler-constant-fold.

Initial focused on/off timings (medians; negative means improvement):

| Workload | Execution | Compilation |
| --- | ---: | ---: |
| compiler-constant-fold | +0.56% | +0.34% |
| CoreMark | +0.31% | -15.10% |
| PCRE2 | -0.22% | -0.23% |
| PolyBench ADI | -1.16% | +2.09% |
| PolyBench Cholesky | -2.31% | +2.39% |

These preliminary differences do not establish a useful gain. CoreMark compilation in particular needs confirmation. A focused six-core reversed-order confirmation batch is running; the feature remains default off pending evidence. No broad suite is being repeated for timing.

## Reversed-order confirmation

Six affected core workloads were measured in OFF/ON/ON/OFF order, with two 300 ms samples per state per pass. Median execution changes: CoreMark -3.08%, PCRE2 +44.00%, ADI -0.48%, Cholesky +1.09%, correlation -1.16%, Floyd-Warshall -1.18%. Compilation changes: -0.99%, -5.95%, +0.68%, -0.42%, -2.86%, +5.19%, respectively. PCRE2's two enabled passes disagree strongly (7.888/7.984 µs versus 18.269/15.360 µs); the +44% aggregate is not a reliable regression estimate. A focused repeat is required before attributing the difference to generated code. Cholesky's initial improvement failed to repeat. The feature remains default off.

PCRE2 isolated repeat (ON/OFF/ON/OFF/OFF/ON, two 500 ms samples per pass): disabled median 7974.5 ns, enabled 8051.5 ns (+0.97%). All invocations passed their exact oracle. This repeat supersedes the noisy aggregate above for PCRE2.

## Branch-only mitigation

Removed the materialized-boolean hook while preserving direct flags consumers; diagnostic backend/catalog/encoder tests pass, including source reuse and wraparound assertions with admission restricted to the branch shape. All 46 core oracle checks pass. All 46 core native images are byte-identical to the broader prototype, so this mitigation cannot change its core execution tradeoff. A focused six-core confirmation is queued/running under the lock; do not interpret a timing difference between identical images as a mitigation gain. The original materialization hook is preserved in `arithmetic-zero-materialized-hook.go.disabled` for review.

## Decision: shelved after mitigation

Branch-only confirmation execution changes: CoreMark -2.10%, PCRE2 +76.45% (unstable between passes), ADI -3.01%, Cholesky -0.92%, correlation -0.35%, Floyd-Warshall -0.50%. A second general mitigation excluded constant operands; diagnostics and all46 core oracles pass, but the affected core native images are unchanged. Neither restriction addresses PCRE2's observed instability. The modest gains and unresolved sensitivity do not justify enabling or retaining the compiler hooks. Prototype source/tests are archived under `experiments/rejected-arithmetic-zero`; public option and production hooks removed. CMN encoder helpers/goldens remain independently useful. The INT64_MIN immediate-range correctness fix and independent regression remain in production.
