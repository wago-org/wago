# PR564: bounded final memory pass

Main baseline: `731e95ff2cda7309eaf6d956f1417066bf7f1b69`.
Previous PR control: `b4f2360f517641803249c7ed998f37d8b0ec82a2`.
After: that PR plus `source.patch`. Frozen binary hashes are in each run's
`identity.json`; the production patch hash is in `plan.json`.

This is a focused follow-up, not another full-suite qualification. The final
468 processes completed in **217.287 seconds** (3 minutes 37 seconds), all
successfully. The earlier three-sample discovery screen is retained separately.

## Changes and safety

Non-compact parallel compilation uses completed worker output lengths to reduce
the final code buffer's initial capacity when the old Wasm expansion estimate
is larger. Arithmetic is checked; per-function alignment and a 4 KiB tail
allowance are included. This is only a capacity hint. The existing append growth
path handles larger output. Compact adapter sharing keeps its prior policy.

Dynamic `memory.copy` emission keeps its four small branch-patch records in
stack-backed slices, retaining append fallback. Bounds checks, overlap handling,
branch targets, code layout, full-width indexes, and runtime execution are not
changed. No cache, manual GC call, worker-count change, or validation shortcut
was added.

## Fixed-work memory: main versus after

One operation per fresh process, six alternating main/prior/after triples.
Values are medians. E means explicit bounds; G means guard pages.

| Full compile case | Bounds | Main B/op | After B/op | Change | Main allocs/op | After allocs/op | Change |
| --- | --- | ---: | ---: | ---: | ---: | ---: | ---: |
| esbuild p2 | E | 178664816 | 165099644 | -7.59% | 23991 | 20745 | -13.53% |
| esbuild p4 | E | 188662172 | 172869968 | -8.37% | 28654 | 24315.5 | -15.14% |
| esbuild auto | E | 186069384 | 175656152 | -5.60% | 28244 | 24642 | -12.75% |
| esbuild p2 | G | 173569956 | 154253860 | -11.13% | 23085 | 20476 | -11.30% |
| esbuild p4 | G | 181469728 | 163384408 | -9.97% | 27402 | 25283 | -7.73% |
| esbuild auto | G | 180199736 | 161709504 | -10.26% | 27451.5 | 24435.5 | -10.99% |
| SQLite p2 | E | 14740772 | 14322640 | -2.84% | 8329 | 8409 | +0.96% |
| SQLite p2 | G | 14347188 | 13072656 | -8.88% | 8253 | 8231.5 | -0.26% |

All esbuild byte/count improvements have unadjusted p = 0.0022. Explicit
SQLite's byte decrease has p = 0.065; its allocation increase has p = 1.000.
Guard SQLite's byte decrease has p = 0.0022 and count decrease p = 0.041.

JSON p8 is a backend-only compile benchmark, not the full pipeline:

| Bounds | Main B/op | After B/op | Change | Main allocs/op | After allocs/op | Change |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| E | 751604 | 736316 | -2.03% | 591.5 | 580 | -1.94% |
| G | 746008 | 731224 | -1.98% | 572.5 | 596 | +4.10% |

Only guard JSON's cold byte decrease has p < 0.05. Its allocation increase has
p = 0.669. In separate adaptive timed samples, guard JSON uses 629.5 -> 584
allocations (-7.23%, p = 0.0022) and 749554.5 -> 728487.5 B/op (-2.81%).
Cold and adaptive data remain separate; neither replaces the other.

## Timing and remaining costs

Six triples with 250 ms requested time, separate from the 1x memory checks:

| Case | Bounds | Main ns/op | After ns/op | Change |
| --- | --- | ---: | ---: | ---: |
| Full esbuild p4 | E | 413171787.5 | 199811934 | -51.64% |
| Full esbuild p4 | G | 403761938.5 | 183259309.5 | -54.61% |
| Full SQLite p2 | E | 73433888.5 | 42799458.5 | -41.72% |
| Full SQLite p2 | G | 69009891.5 | 39276228 | -43.09% |
| Backend JSON p8 | E | 519559.5 | 343415.5 | -33.90% |
| Backend JSON p8 | G | 491693.5 | 315048 | -35.93% |
| Compact JSON | E | 1264981 | 1124278 | -11.12% |
| Compact JSON | G | 1215631 | 1060235 | -12.78% |

No selected timing increase versus the previous PR control has unadjusted
p < 0.05. Compiler timing medians versus that control range from -1.84% to
+1.36%; this is not proof of equal speed. The main-relative compilation gains
largely predate this pass; the new contribution is lower temporary allocation.

These timing medians still lose to main:

| Case | Bounds | Main ns/op | After ns/op | Change | p |
| --- | --- | ---: | ---: | ---: | ---: |
| Coremark | E | 34224215 | 34514618 | +0.85% | 0.240 |
| isa_f32.min | E | 32156.5 | 32525.5 | +1.15% | 0.394 |
| tiny.add | E | 8.2765 | 8.5545 | +3.36% | 0.015 |
| Parallel process bulk copy 64 | G | 332.1 | 352.8 | +6.23% | 0.589 |

The earlier longer-repeat timing warnings are not cleared merely because these
shorter samples differ. No native execution optimization was attempted here.
Compact JSON also retains a small byte cost: guard mode 136400.5 -> 136517 B/op
(+0.085%), while its allocation count falls from 148 to 135.

Peak process memory is not uniformly lower. Explicit esbuild p2 is
184574 -> 188160 KiB (+1.94%, p = 0.026); the previous PR control is 181680 KiB.
This increase remains a measured cost despite fewer allocated bytes. Explicit
JSON p8 is essentially flat, 91906 -> 91924 KiB (+0.02%, p = 0.818); guard
JSON p8 is 92018 -> 90972 KiB (-1.14%, p = 0.589). All other selected cold peak
RSS medians are lower than main. This does not establish a hard device-memory
budget: process peaks include setup, Go GC behavior, and corpus loading. In
`BenchmarkCompileWorkers`, other wanted modules are decoded before subbenchmark
selection, so its peak is not the selected module's retained compiler memory.

## Checks and method limits

- Native and guard-page `./src/...` tests pass.
- Targeted AMD64 parallel and memory-scratch race tests pass.
- Full ARM64 backend suites under Go 1.22/QEMU pass in both modes.
- Bench module tests pass.
- All 112 checked AMD64 native-code pairs remain identical to main.
- The new capacity test failed before the helper existed, then passed, including
  overflowing hints and the retained allocation-growth policy.
- The dynamic-copy allocation test failed at one allocation per warmed AMD64
  emission, then passed at zero; its ARM64 counterpart also passes.

Linux AMD64, Go 1.27.1, GOMAXPROCS=8, GOGC=100. No agent-run builds, tests, or
profiles overlapped the timed comparisons. P-values are unadjusted two-sided
exact rank tests; they are not proofs of cause or equivalence. Parallel execution
reports throughput time, not one-call latency. Fixed 1x values support memory
comparisons, not speed claims. Adaptive RSS is retained only as raw evidence.

The two sampled allocation profiles are diagnostic, not timing measurements.
An initial profile launch from the repository root failed to find the bench
fixtures; successful profiles ran from `bench/`. Early focused test commands
also used the wrong working directory; the full correctly scoped passing check
logs are retained. No expectations or checks were weakened.

The full benchmark matrix was deliberately not rerun. Native ARM64 speed,
fresh TinyGo release sizes, and local Wine behavior were not requalified in
this bounded pass. CI status must be checked on the new pushed commit; earlier
green CI belongs to the previous PR code.

## Full numbers and reproduction

- [All main/prior/after metrics, including discovery samples](all-measurements.csv).
- [Every remaining measured metric increase in this pass](remaining-increases.csv).
- [All process peak-memory samples summarized](all-process-rss.csv).
- [Run plan and source identities](plan.json).
- [Correctness commands and exit status](checks.json).
- [Native-code equality results](code-audit.json).

Each run directory holds binary hashes, raw benchmark aggregates, comparisons,
and per-process status. `raw-process-logs.tar.gz` preserves all individual logs
and resource measurements. Scripts use original workspace paths; adjust paths
to reproduce. New test sources are preserved under `scripts/` as `.go.txt` so
they do not become packages in the benchmark module. `source.patch` contains
the tracked backend diff; the newly added test files are separate. SHA256SUMS
covers every exported file except itself.
