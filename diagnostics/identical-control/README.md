# Identical-binary cold compile diagnostic

Stable timing controls were not established. This was one predeclared run,
not a retuning or rerun of the common-path candidate.

All 384 first-attempt fresh-process observations were kept and all oracles
verified. Both labels used one unchanged full-API b5ab adapter binary and
one executable inode, built with Go 1.27.1, PGO and all experimental flags
off, at T402000/R4096. Four workloads and two file-preparation strata used
balanced ABBA/BAAB blocks in three fixed epochs. One stratum rewrote the
same bytes before each launch; the other skipped the rewrite. Both hashed
the executable each time, so neither stratum implies a cold file cache.
No build or layout variant ran.

| Preparation | Workload | A/B | Paired block 95% CI |
|---|---|---:|---|
| Rewrite | tiny/add | 0.98657 | [0.89200, 1.05523] |
| Rewrite | fib_rec/fib | 0.90695 | [0.81077, 1.03287] |
| Rewrite | coremark/2k-performance | 1.03338 | [0.96212, 1.06992] |
| Rewrite | pcre2/compile-match | 0.98591 | [0.91522, 1.04362] |
| Rewrite | Aggregate | 0.98761 | [0.92202, 1.04079] |
| No rewrite | tiny/add | 0.99336 | [0.93069, 1.05936] |
| No rewrite | fib_rec/fib | 1.03095 | [0.95255, 1.06112] |
| No rewrite | coremark/2k-performance | 1.01727 | [0.89328, 1.11537] |
| No rewrite | pcre2/compile-match | 1.04478 | [0.96496, 1.11301] |
| No rewrite | Aggregate | 1.04252 | [0.96779, 1.10539] |

The aggregate compares the arithmetic means of four per-workload medians.
The fixed analysis used 5,000 paired block bootstrap draws with no workload
resampling. The unchanged gate required a point within 2% of parity and a
95% interval wholly within [0.97, 1.03]. Both aggregates and all four
individual workloads in both strata failed.

No major faults were observed. Runqueue-wait and minor-fault correlations
were weak and descriptive; their phase measurements include IPC/barrier
time outside the compile timer. They neither identify a unique cause nor
justify correcting elapsed times. Omitting rewrites and balancing labels did
not establish the required precision in this run.

Small-effect timing qualification stops on this protocol and environment.
No retry, observation trimming, threshold relaxation, protocol switch, or
retrospective rescue of the zero-arity candidate followed. This is not a
claim that future measurements cannot work, and makes no compiler gain or
RSS claim.
