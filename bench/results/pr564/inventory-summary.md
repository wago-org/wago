# PR #564 reported-metric inventory

These are historical claims, not results of the new qualification run.
The sources are the PR description at reviewed head
`ab29bf3a9ad215833b5138220de6cd7190461a78`, its `REPORT.md`, and the
commit messages between reviewed base `447f057115ee04d9e58580061dbee696becec21f`
and that head, plus the attached CI build-size artifact. PR comments and reviews
returned empty lists in the captured API response.

The JSON and TSV index contains 1,036 source lines: 117 from the description,
522 from the report, 312 from commit messages, and 85 from the CI size artifact.
It includes all numeric
lines plus resource/equality claims, with source line numbers, section names,
commit IDs where available, and table headers. This is a line inventory, not
a claim that there are 1,036 independent measurements. Full sources are retained
so units and surrounding qualifications remain available.

## Main aggregate claims

| Source checkpoint / metric | ARM64 | AMD64 |
|---|---:|---:|
| PR description: backend compile time | -20.36% | -16.32% |
| PR description: public compile time | -24.83% | -26.37% |
| PR description: backend heap | +0.53% | +1.07% |
| PR description: public compile heap | +1.26% | +1.85% |
| PR description: generated code size | 0.00% | 0.00% |
| PR description: execution time | +0.17% | -1.90% |
| Report pause checkpoint: backend compile time, five large modules | -35.70% | -25.58% |
| Report pause checkpoint: public compile time, five large modules | -40.20% | -36.26% |
| Report pause checkpoint: backend heap | -6.33% | -0.28% |
| Report pause checkpoint: public compile heap | -1.62% | +0.24% |
| Report pause checkpoint: generated code size | -4.50% | 0.00% |
| Report pause checkpoint: execution time | -0.90% | -0.25% |

The description names implementation `b467c044`; the report's pause checkpoint
names `96cfd406`. Both use an older main baseline,
`c46f2129edb52e6f30f4d0bfc5ae105cfde0c84d`. The report contains still later
incremental validation and internal-LEB checkpoints. None establishes exact-head
qualification of the reviewed PR. The report uses native Apple ARM64 and
Rosetta AMD64; native Linux AMD64 was pending. These results cannot be combined
with the new Linux host as one sample population.

## Attached CI binary-size results

The attached build-size job used Go 1.22.12 and TinyGo 0.41.1 on Linux AMD64.
It checked out CI merge `db46f2467e5ae209e9b06043130ede56cb3c20bf`, which merges
the reviewed PR head into its reviewed base. Its comparison fetched `origin/main`
at run time. Do not treat that moving name as the later pinned benchmark base.

| Profile | Binary bytes | Reported delta bytes | Budget bytes |
|---|---:|---:|---:|
| manager | 7,774,360 | +8,192 | 9,000,000 |
| runtime-standard | 7,807,128 | -147,456 | 8,870,000 |
| runtime-minimal | 7,491,736 | -143,360 | 8,560,000 |
| runtime-minimal-tiny | 2,255,976 | +12,536 | 2,317,000 |

The artifact also reports the 25 largest symbols for each Go profile. All 75
symbol-size rows are indexed. The original artifact, profile/symbol TSV files,
job logs, and check-run metadata are retained. The failed Linux AMD64 log confirms
the 28-versus-24-byte hint test and 52-versus-68-entry arena test failures.

## Inventory coverage

- Backend, compact, full-pipeline, decode, validation, and hint-scan latency;
  per-module rows; serial/parallel/adaptive workers; sample counts, durations,
  medians, geomeans, p-values, and confirmation runs.
- Compiler heap bytes and allocation counts; retained hint layout, stack-arena
  capacity, scratch storage, snapshot/local limits, and bounded cache sizes.
- Native code bytes, code-size percentages, absolute byte reductions, hash or
  byte-equality claims, and allocation-free execution claims.
- Execution results for the full runnable corpus and focused memory, calls,
  globals, integer, floating-point, SIMD, and library workloads.
- Incremental retained changes, optimization ablations, rejected probes,
  profile percentages, and stated future optimization estimates.
- Historical test counts/status and environment limits that qualify the claims.

Two claims require explicit correction: the reviewed AMD64 hint record is
28 bytes, not 24; and the later ARM64 changes alter generated native code.
Equal AMD64 code sizes alone do not prove byte equality or semantic equivalence.

The complete row-level inventory is in `metric-inventory.tsv` and
`metric-inventory.json`; source text is in `pr-description.md`,
`original-REPORT.md`, and `pr-commit-messages.txt`.
