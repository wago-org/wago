# Legacy common-path experiments

The three independent patches each apply to Wago source b5ab03f9. They are
archived experiments, not part of the default compiler. None is promoted.

- **Span detach** (f21f44ad): splices a consumed operand span once while
  retaining per-node cleanup, prefix/spill tracking and stale-link clearing.
  Normal and ownership-check suites each passed 214 subcases. Isolated
  mixed-state span lengths 1 and 2 were 1.056 and 1.072 times original
  median latency. In the 145-workload corpus, 96.15% of spans had length
  at most two (4,403 length-one and 63,951 length-two out of 71,092).
  The microbenchmark is not a compile-time result.
- **Combined batch** (4fd4d5db): adds unary constant-root reuse, zero-arity
  branch fast path, a borrowed brOnNonNull type view and br_table payload
  reuse. Normal and ownership/stats focused gates passed. All 145 native
  outputs and serialized metadata matched the baseline; 22 HostCall
  callbacks with trap/recovery and PC checks passed, as did all 145
  production-adapter runtime oracles. All 359 observed payload captures
  were zero-arity and no unary folds activated. No performance qualification.
- **Narrow branch candidate** (b89078d6): saves zero-arity branch bookkeeping
  and removes the brOnNonNull type copy, without the other changes. Focused
  checks plus all 145 native/serialized metadata and 22 HostCall/trap/PC
  checks passed. Counters found 18,474 zero-arity fast path uses and no
  brOnNonNull copy removals in the corpus.

## Narrow cold result

One first-attempt frozen comparison used eight cases, four binary arms and
nine trials (288 verified records). Both baseline arms used the same b5ab
binary, and both candidate arms the same narrow binary. Standard Go 1.27.1,
T402000/R4096, PGO and experimental flags off. The affected six-case
candidate/baseline ratio was 0.96203 (paired 95% CI 0.91417–1.00686).
Baseline A/A was 0.97358 (0.92482–1.03516), candidate A/A 0.98865
(0.93545–1.05011); both failed the preregistered control thresholds.
All eight individual cases failed both control gates. Both negative
control cases had zero activation of either change.

The measured point is not a qualified gain. No rerun, short-span tuning,
full-suite compile or RSS claim, or default promotion followed.
