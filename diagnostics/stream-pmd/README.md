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
page-observation records, all verified. Outcome analysis is pending. One
authorized recovery reran the interrupted long phase; the original 275 partial
records were hashed, left unanalyzed and excluded. Cold, RSS and full page
phases ran once. There was no further retry or adaptive variant selection. No speed or broad execution-guard
claim is made. Earlier source-order layout failures remain valid separate
results, not results for this linker-placement experiment.
