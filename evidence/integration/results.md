# #815 practical local workflow integration

Issue: https://github.com/wago-org/wago/issues/815 ([Research] Add operand-aware
native-code comparisons for performance diagnosis). This remains a bounded useful
choice: it is unassigned and the refreshed PR inventory has no matching claim. It
uses existing #719 profile/source maps without modifying that implementation,
RISC-V, host/guest boundaries, #910, #909/#895 or paused SQLite experiments.

Recommendation: **keep the opt-in local developer workflow**, with the measured
cost and qualification limits below. This is not full #815 completion or a
production optimization; broader acceptance still requires revision and native
ARM64 evidence. No publication, PR, push, merge to shared main or issue mutation.

Private worktree: `/home/jtenner/Documents/Codex/2026-10-09/task-3/wago-815`.
Branch: `experiment/operand-comparison-815`.
Baseline current main: `05af869ebcf25feb723249ac64c7a1de87d28fd0`.
Private merge: `8dfea5eecfcd4b210d9b725a68f6b10f2d797a73`.
Integration: `ee0af276974f5d6f15bb2c9afadcc8ab3604cf63`.
Reviewed/frozen source: `60481522292483cf94a116b17a68c69a10a8c737`.
Historical prototype: `a53e245bb1fb7b453c4515d71783c1c3261037d5`.
Final evidence-only commit follows the source commits. Detached baseline worktrees
`../baseline-integration` and `../baseline-tool-815` preserve reproducibility.
No other agent checkout or shared settings changed.

## Hypothesis and acceptance

The original hypothesis stands: preserving operand facts detects changes hidden
by opcode-only comparison; stable-target relocation alone compares equal.
Continuation criteria were recorded in plan.md before renewed timing. Useful
integration requires executable capture/compare/test recipes, complete support
for the supplied Fibonacci scalar region, retained branch conditions/dependencies,
and conservative rejection of unknown effects or bad metadata/alignment.

Those bounded criteria pass. The producer derives instruction boundaries from
existing objdump, verifies them against every native image byte, and uses current
backend source maps. It bounds input, module/local counts, emitted code, listing,
instruction/region counts and output, streams executable hashing, and uses one
compiler worker. Source ranges must be ordered, nonoverlapping and align at both
ends; direct branch target IDs must name captured instruction starts. Missing,
out-of-range or mid-instruction targets remain literal; previous identities clear
on re-resolution. Metadata count/hash-format validation is not authentication of
user-supplied snapshots or proof of every source map.

## Workflow and measured coverage

From the repository root:

```sh
just bench native-capture tests/fixtures/wasm/fib.wasm /tmp/fib-before.json
# Repeat capture in a different compiler checkout for an actual version comparison.
just bench native-capture tests/fixtures/wasm/fib.wasm /tmp/fib-after.json
just bench native-compare /tmp/fib-before.json /tmp/fib-after.json
just test native-compare
```

Existing ordinary/checked go-test workflows already discover portable controls;
no CI workflow changes were necessary. The focused recipe adds native profile
capture and vet. Capture is AMD64-only and needs existing GNU objdump. Comparison
is portable. Native ARM64 capture/execution is unqualified; synthetic word controls
and an ARM64 cross-build pass. Use a built command for exact exit codes: `go run`
wraps tool failure exit codes as runner exit 1.

The frozen profile command is 8,923,852bytes (SHA256 retained in
frozen-tool-hashes.txt); it is a separate optional developer executable.
Frozen command `native-compare capture` produces a 2,610-byte Fibonacci JSON
snapshot. Self-comparison: eight regions, twelve supported instructions, zero
unknowns and zero changes; complete **for the supplied regions**. Native image105
bytes, mapped51, unmapped54. Wrapper/call bytes are not qualified. CMP reads two
registers and writes flags, Jcc reads flags and preserves its condition, immediate
ADD sign extension and GP32 zero extension are explicit, and the back edge names
a function/PC/occurrence/instruction anchor. Primary change categories identify
constants, dependencies, widths, addressing, branches and unknowns.

The standalone CLI replay gives exit0 for self-comparison and a constant-change
record control; the latter reports category `constant`. A valid unsupported INC
record gives unknown site/raw/source facts and exit3. These controls edit diagnostic
JSON only; no changed bytes are compiled, mapped or executed. Their inherited
native-image hash describes the original producer image, not the edited record.
They are explicitly synthetic controls, not independently captured after-images.

## Production control measurements

Native AMD64 Ryzen7 8845HS; GOMAXPROCS1; CPU15; five alternating A/B pairs, 20ms per
compile workload and 10,000 execution iterations. Three same-binary A/A controls.
Each process includes benchmark calibration/setup; all builds finished before
measurement. Host starting load0.53/0.83/0.88. Raw samples and summary.json retained.
These small workloads are not an end-to-end application study.

| Workload | Baseline median (range) | Candidate median (range) | Median change | Allocation medians before→after |
|---|---:|---:|---:|---|
| Small scalar compile | 6.124µs (6.046–6.216) | 6.148µs (5.949–6.285) | +0.39% | 8,784→8,771B;22allocs |
| Medium control compile | 7.219µs (6.993–7.324) | 7.276µs (7.085–7.535) | +0.79% | 10,670→10,705B;21allocs |
| ALU compile | 23.626µs (23.277–25.933) | 23.420µs (23.167–25.001) | −0.87% | 9,456→9,456B;11allocs |
| Bounds-facts execution | 33.77ns (33.64–37.01) | 33.91ns (33.55–35.46) | +0.41% | 0B;0allocs;811native bytes |

A/A small-scalar medians6.120→6.057µs (−1.03%). The prior61.4% signal did not
reproduce. Build/run noise is a plausible explanation, not a proven historical
root cause. Minor byte-allocation variation comes from the existing benchmark
process/setup/calibration; identical binaries exclude a patch-caused compiler
implementation difference. No speedup or regression in production is established.

Matched `-trimpath -buildvcs=false` AMD64 benchmark binaries are byte-identical:
15,223,309bytes, SHA256 `8d2675239af8599332655bbf1ede480060c8e1bf3525765f1cf62097f7687170`.
Actual ordinary runtime executable builds (`-tags=wago_runtime ./cli/wago`) are
byte-identical:14,755,751bytes, SHA256
`3552d4c0b92f427dcbfcae463b20ae89c0e01690cd55c07da064104e2d17b13e`.
`binary-identity.txt` also contains an auxiliary cli/runtime package archive
control; that archive is not the runtime executable. Runtime source, emission,
Wasm semantics and trap ordering have no changes in this feature diff.

## Diagnostic costs and negative finding

Frozen checked/profiled Fibonacci compare: median2.269µs,96B,8allocs. Relocation
normalization and multi-region validation allocate. Capture: median8.914ms
(range8.894–8.929), median132,665B and813allocs per capture, including executable
hashing and objdump. Five iterations per sample, three samples. It omits JSON
serialization/file writing from the timed capture. Do not interpret capture
cost as compilation alone, or B/op as peak/retained memory.

Matched ordinary portable tool builds, three alternating100-iteration pairs:

| Compare input | Prototype median | Richer records median | Change | B/op / allocs |
|---|---:|---:|---:|---|
| 16 equal instructions | 1.424µs | 2.241µs | +57.4% | 0 / 0 both |
| 256 equal instructions | 20.737µs | 34.157µs | +64.7% | 0 / 0 both |
| 4096 equal instructions | 333.547µs | 543.464µs | +62.9% | 0 / 0 both |
| 32 changed memory instructions | 5.667µs | 6.497µs | +14.6% | 26,656→32,192B / 6 both |

The richer diagnostic is measurably slower and creates a larger change payload.
Keep its offline opt-in scope; reconsider record representation before embedding
it in a high-volume analysis path. No quadratic alignment or per-instruction
register slices were introduced. RSS, peak heap, retained heap and whole-repository
CI were not measured. The input/code caps are not a compiler heap quota: emitted
size is checked after compilation. Arbitrary/untrusted workloads are not admitted
by this experiment. Capture/compiler/tool startup and file-system cache effects
remain timing limitations.

## Verification and independent review

Ordinary and checked/profiled focused suites and vet pass; frozen focused test
binary replay passes; portable ARM64 checked/profiled cross-build passes. Coverage
includes real source-map/objdump identity, independent native/Wasm ADD/XOR facts,
six supported public API XOR oracles, canonical NOPs, CMP dependencies, Jcc
conditions, signed immediates, zero displacement, negative prefixes/SIB/partial
encodings, provenance/metadata limits, incomplete unknowns and source/target
boundary rejection. Diagnostic artifact execution is not claimed. Legacy test
emitters now label source revision as unattested, replacing a stale hardcoded
revision; historical v1 evidence is retained unchanged.

Independent reviewer ran its own ordinary/profiled tests and vet, verified just
wiring, examined source and raw timing/binary evidence, and found no remaining
source blocker. Its report is retained separately. It required source boundary,
stale relocation and provenance fixes before acceptance. Full issue acceptance
still requires wider semantics, #719 artifact-interface settlement and native
ARM64 hardware; this useful local integration does not conceal those limitations.
