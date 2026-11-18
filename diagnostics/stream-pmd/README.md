# Supported-linker placement experiment

Experimental evidence and an explicit build recipe only. Active compiler source
remains `b5ab03f924e6b74ee3b558e34ad9cb32c9026acd`; project/default build flags
are unchanged. No compiler features are removed. This is not a source-level
40% compilation improvement, a general deployment recommendation, or a PR.

## Frozen arms and scope

All arms use Go 1.27.1 linux/amd64, the same full adapter and dependency graph,
`-pgo=off -trimpath -buildvcs=false -p 1`, common `-R 4096`, and all 20
experimental feature flags OFF. Only the standard Go linker `-T` value differs:

| Arm | Linker text address |
|---|---|
| Baseline, explicit default placement | `0x401000` |
| Candidate, 4 KiB shift | `0x402000` |
| Aligned relocation control, 2 MiB shift | `0x601000` |

The evidence is specific to the existing internally linked, static ELF
`ET_EXEC` executable on Linux/amd64. Build mode, link mode, CGO, security
permissions, kernel settings, cache policy and memory advice were not changed.
There is no qualification of PIE, external linking, other platforms, or other
loaders. Artifact labels `hot` and `neutral` mean candidate and aligned control
here, not the earlier source-order layout variants.

`build_variants.py` is a parameterized reproduction recipe requiring an explicit
matching adapter checkout, modfile resolving Wago to the frozen clean source,
Go executable, CPU and output directory. It requires `--execute-builds`; it
neither runs measurements nor changes project defaults. The full benchmark
harness and workload artifacts are separate prerequisites, not embedded here.
Local receipt outputs can contain caller-provided paths; review them before
sharing. This sanitized recipe was not the controller used for the recorded run.

## Static and API gates

All three binaries remain ET_EXEC without an interpreter. PT_LOAD offsets,
permissions, file sizes and memory sizes are identical, with 4096-byte alignment.
LOAD virtual addresses and the entrypoint shift uniformly by the requested
amount. Virtual-address minus file-offset congruence modulo 2 MiB is zero for
baseline/control and 4096 for the candidate; all remain congruent modulo 4 KiB.
All 9,463 named text functions retain their sizes (6,124,332 bytes total).
Relocation-aware instruction comparison reports zero changed functions, with
instruction offsets and non-address operands retained. This is not a claim of
identical raw executable bytes or identical linked pointer values.

Exact-placement API observers passed 145 native-code comparisons against the
pristine reference and mutual serialized-metadata comparisons in each arm.
Each arm also passed 22 HostCall callbacks, trap/recovery checks and PC-symbol
sanity. Some private/debug metadata fields are outside that serializer's scope.

## Bounded mapping mechanism gate

Nine fresh children covered three fixed workloads, all three arms, one trial.
All runtime oracles passed; 45 phase observations were retained. No address
search, retry, build or settings change was made during this probe.

Executable and read-only VMAs each reported 4096 KiB FilePmdMapped in every
observed phase for baseline and aligned control, versus zero for the candidate.
After describe, file RSS was 10,240 KiB for both aligned arms and 5,760 KiB for
the candidate. Absolute compiled peak measurements in KiB were:

| Workload | Baseline | Candidate | Aligned control |
|---|---:|---:|---:|
| audio-adpcm | 12176 | 9236 | 12180 |
| json-as serializeN | 13028 | 10796 | 13076 |
| utf8proc normalize-casefold | 13308 | 10972 | 13396 |

This confirms a mapping-mechanism change in the sampled environment, not an
8 MiB RSS saving: needed constituent pages can remain mapped separately.
Aggregate smaps fields do not identify exact PMD subranges. Residency does not
prove execution of all mapped functions. After describe includes initialization
and protocol handling and is not pristine startup.

The three-workload probe is not a 145-workload RSS or latency qualification and
does not meet the absolute 5 MiB target. The separately frozen full measurement
pipeline completed 540 long-execution, 2,900 cold-compile, 435 RSS, and 435
page-observation records, all verified. Outcome analysis is complete; see the qualification results below. One
authorized recovery reran the interrupted long phase; the original 275 partial
records were hashed, left unanalyzed and excluded. Cold, RSS and full page
phases ran once. There was no further retry or adaptive variant selection. No speed or broad execution-guard
claim is made. Earlier source-order layout failures remain valid separate
results, not results for this linker-placement experiment.


## Completed qualification: RSS reduction, targets unmet

The independent 435-process RSS measurement used 145 workloads, one fresh
process per arm and workload, and absolute compiled-barrier VmHWM. Arithmetic
means (one trial, no confidence interval) were:

| Arm | Mean MiB | Minimum MiB | Maximum MiB | Below 5 MiB |
|---|---:|---:|---:|---:|
| Baseline | 12.097414 | 11.867188 | 14.191406 | 0/145 |
| Candidate | 9.441164 | 8.832031 | 12.046875 | 0/145 |
| Aligned control | 12.097522 | 11.871094 | 14.082031 | 0/145 |

The candidate mean is 21.96% lower than the explicit-default baseline, but
remains 9.44 MiB. The absolute 5 MiB goal is unmet. This is unchanged b5ab,
not a pristine-main or compiler-source optimization qualification.

The separate 435-process page census confirmed zero executable/read-only
FilePmdMapped for the candidate at all five phases across all 145 workloads;
both controls retained 4096 KiB in each mapping. Candidate compiled backend
residency averaged 308.117 pages (range 277–325), versus 341 for both controls.
Candidate cumulative minor faults at compiled averaged 847.221 versus 763.986
for baseline; these are faults, not measured cycles or proof of their latency
cost. Major faults were zero. Page-observer RSS values are not substituted for
the independent RSS measurement above. Section coverage and backend coverage
overlap and must not be added; present pages do not prove executed code.

Cold compilation used 2,900 verified records. Means of per-workload medians
were baseline A 721,612.95 ns, baseline B 726,161.30 ns, aligned control
732,138.92 ns and candidate 761,974.80 ns. Candidate/baseline was 1.05262
(paired 95% interval [1.00998, 1.14813]); candidate/control was 1.04075
[0.99074, 1.13995]. The A/A control was 0.99374 [0.93681, 1.03533] and failed
its predeclared acceptance gate. Preserve the adverse candidate point and
interval, but do not claim qualified causal slowdown or improvement under the
failed controls. No compilation gain, much less 40%, is established.

Long execution analyzed only the 540 completed recovery records. Five of 15
cases passed their A/A control. Four of those passed the candidate's <=5%
upper-confidence-bound guard: pcre2, nanosvg, libtommath and drwav. Blake3
passed its control but its candidate/baseline upper bound was 1.05742, missing
the guard. The other ten cases are inconclusive because controls failed.
Nanosvg showed a small qualified slowdown of about 1.49% (95% interval
[0.11%, 3.21%]), within the guard. The aligned control also missed the guard on
libtommath (upper bound 1.07303). Full case ratios and intervals are retained
in `outcomes.json`; favorable points with failed controls are not wins.

The original interrupted 275 long-execution records remain unanalyzed and
excluded. A single authorized infrastructure recovery restarted the entire
540-record sequence with identical operations, seed and flags. Its only
controller difference was durable plain-JSONL/fsync output, compressed after
completion. No partial records were combined; there was no second retry or
address search. Cold, RSS and page phases completed on their first attempts.

This establishes an environment-specific file-mapping mechanism and an RSS
reduction, not joint target qualification. Default flags and active source
remain unchanged. No promotion, PR, portability promise or overall <=5%
execution claim is proposed.
