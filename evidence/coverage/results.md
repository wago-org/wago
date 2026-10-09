# #815 experiment outcome

Issue: https://github.com/wago-org/wago/issues/815.
Recommendation: **keep as an opt-in bounded developer diagnostic; keep the PR draft**.
It supplies useful operand/raw observations and workflow commands without production
imports or compiler/runtime edits. It does not prove semantic equivalence, root cause,
whole-program performance or complete ISA coverage.

## Hypothesis and decision

Before implementation, plan.md specified conservative profile-owned raw coverage,
no fabricated source/implicit semantics, no new relocation waivers, linear budgets,
paired short runs and rejection on false qualification. The fixture now partitions
all 105 native bytes: 51 mapped, 54 opaque in three owned gaps. Source and ownership
controls reject inconsistent/truncated input. Unknown mapped semantics remain unknown;
aligned raw changes remain observations even under configuration mismatch.
The original identical-opcode controls distinguish constants, dependencies and widths
and waive only admitted direct displacement changes with stable target identity.
All five literal issue criteria have bounded support in acceptance.md. Native ARM64
hardware, peak/RSS/retained memory and full CI remain unqualified.

The prior integration's requirement for broader ISA/#719 settlement was an overly
broad interpretation of the literal initial-subset criterion; acceptance.md supersedes
that historical conclusion. Historical negative measurements remain retained.

## Source and provenance

The first production control was current main c95243aa438e4026bf5d2f263c83e53f2616d7da. Fresh final production builds use e4bcc524 as control.
Main advanced to e4bcc5244a29ddf4028fdeb3a7255ec8c76d810f during checks with an
ARM64-only immediate-store fix; it was incorporated in measured merge 66dedc1d before final source freeze.
Previous diagnostic control: 8e82e1b26e0b5ac015b761f0c4bf5c9c894fb0b2.
See frozen-source.txt and frozen-binary-hashes.txt for the exact measured candidate.
The final publication head may add documentation/evidence only.

Captured Fibonacci snapshots are independent same-tool invocations, not compiler
before/after differences. Native SHA256:
23884eaa206f055bd7862d2c03f2f0bbfeac39eadf7c02c80000c44af58e00ea.
Independent review reconstructs the full image from mapped/raw ranges. Synthetic
constant/unknown/raw controls edit JSON only; none are executed or producer-attested
changed native images. Actual public runtime results: fib(20)=6765, fib(30)=832040.

## Measurements

Go 1.27.1, GNU objdump 2.44, Linux AMD64 Ryzen 7 8845HS, GOMAXPROCS=1, CPU 15.
Builds finish before timings; five alternating portable pairs, three profile pairs,
100 comparison iterations/sample and five capture iterations/sample. Host load is
saved. These bounded trials do not estimate reliable confidence intervals.

| Diagnostic workload | Before median (range) | After median (range) | Median delta | B/op before→after; allocs |
|---|---:|---:|---:|---|
| Equal GP records (16) | 2.407 µs (2.392–2.845) | 1.082 µs (1.071–1.092) | -55.05% | 0→0; 0→0 |
| Equal GP records (256) | 36.244 µs (35.818–36.954) | 14.034 µs (13.954–14.437) | -61.28% | 0→0; 0→0 |
| Equal GP records (4096) | 576.848 µs (574.665–579.196) | 226.627 µs (225.982–227.376) | -60.71% | 0→0; 0→0 |
| 32 changed memory records | 9.684 µs (6.809–19.913) | 8.193 µs (6.982–8.956) | -15.40% | 32,192→26,912; 6→6 |
| Captured fib compare | 2.361 µs (2.320–2.366) | 2.118 µs (2.054–2.132) | -10.29% | 96→0; 8→0 |
| Fib capture | 11.208 ms (11.131–11.993) | 11.358 ms (10.565–11.770) | +1.34% | 132,668→133,920; 813→835 |

Capture includes compilation, executable hashing and objdump but excludes JSON/files;
its time is not compile time alone. Allocation figures are B/op/allocs, not retained
or peak memory. Capture cost is small in this opt-in scope but increases; no capture
speed improvement is established. Changed-report timing is particularly noisy.

The frozen optional diagnostic command is 8,950,106 bytes; its executable SHA256
is retained in frozen-binary-hashes.txt. This standalone tool is absent from ordinary
production builds.

Fresh ordinary production runtime builds are byte-identical at 14,755,751 bytes,
SHA256 3552d4c0b92f427dcbfcae463b20ae89c0e01690cd55c07da064104e2d17b13e.
The ARM64-only main advance does not affect that AMD64 binary. Historical paired
production controls (integration/results.md) show compile medians +0.39%, +0.79%,
−0.87% and execution +0.41%; A/A small compile variation −1.03%. No production speed
or regression is established. The initial 61.4% tiny compile signal did not reproduce;
noise is plausible, not a proven retrospective cause.

## Validation and limitations

Focused ordinary/checked/profile controls, independent source attribution, six public
API XOR oracles, vet, workflow checker and ARM64 checked/profile cross-build are the
relevant diagnostic checks. Detailed final outcomes are saved in validation.md.
Broad ordinary root testing was interrupted once by the app update, then rerun.
The completed ordinary run failed the TinyGo harness's VCS-stamp discovery and staged
specification families requiring the absent pinned spec-v3 corpus. A process-local
GOFLAGS=-buildvcs=false rerun of the TinyGo package passes. Unchanged main reproduces
the missing staged-corpus error. No shared settings were changed, and these failures
are not hidden or counted as passing. The original interrupted log is retained.

No host/guest boundary, active RISC-V/vectorization/SQLite work, another checkout,
credentials or CI workflows are altered. ARM64 emitted-code execution/source capture
is unavailable on this AMD64 machine. Full CI still must qualify the patch before
readiness/merge; draft smoke is a distinct, limited profile. No merge is authorized.
