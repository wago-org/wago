# Operand-aware native comparison (#815)

This opt-in developer diagnostic compares native operand facts against Wasm source
anchors and compares the remaining bytes as opaque ranges. It reuses Railshot's
existing profile/source-map output and GNU objdump, with no production imports,
instrumentation, code generation or runtime dispatch. It is static diagnostic
evidence, not a semantic-equivalence proof or automatic performance verdict.

## Workflow

Use this alongside an optimization experiment: capture the **same trusted bounded
Wasm input** in each compiler checkout, compare snapshots, then interpret those
observations together with independent correctness tests and paired timings.
Do not infer hotness or execution cost from static counts. No artifacts are uploaded.
Use the same diagnostic/schema version for both captures. For an older compiler
checkout without the tool, transplant this diagnostic directory into that isolated
checkout before building it; stamp the compiler base revision and record the dirty
build. Comparing legacy partial coverage with new full coverage is incomplete.

From the repository root:

```sh
just bench native-capture tests/fixtures/wasm/fib.wasm /tmp/fib-before.json
# Capture in the candidate compiler checkout using the same input and settings.
just bench native-capture tests/fixtures/wasm/fib.wasm /tmp/fib-after.json
just bench native-compare /tmp/fib-before.json /tmp/fib-after.json
just test native-compare
```

The focused test recipe runs portable checked controls, native AMD64 profile/checked
capture controls and vet. The entire tool requires `wago_nativecompare`, including
comparison-only builds. Ordinary broad builds/tests/install discovery excludes it.
An existing Linux AMD64 CI job runs the explicit focused recipe. The native capture
recipe additionally requires an AMD64 host, `wago_profile` and existing GNU objdump; it installs no software. Use a
private writable GOCACHE when sharing the machine; recipes use one compiler worker,
GOMAXPROCS=1 and -p=1. Profile capture tests require GNU objdump too.

For scripts that consume exact exit codes, build the command:

```sh
GOMAXPROCS=1 go build -p=1 -tags=wago_nativecompare,wago_profile \
  -ldflags "-X main.compiledRevision=$(git rev-parse HEAD)" \
  -o /tmp/native-compare ./tests/tools/native-compare
/tmp/native-compare capture tests/fixtures/wasm/fib.wasm /tmp/fib.json
/tmp/native-compare compare /tmp/fib.json /tmp/fib.json
```

JSON goes to stdout; summaries go to stderr. Standalone exit 0 means the admitted
comparison completed (changes are allowed), exit 2 invalid input, exit 3 incomplete.
`go run` wraps nonzero tool exits as runner exit 1. With only `wago_nativecompare`, the command supports comparison and legacy
`native-compare before.json after.json`; capture additionally requires the profile tag.

## Read the result

Current Fibonacci self-comparison reports:

```text
supplied regions: compared=12 supported=12 unknown=0 changes=0 complete=true limit=false
opaque bytes without source locations: before=54 after=54; raw ranges compared=3 changes=0 complete=true
```

All 105 native bytes are accounted for: 51 source-mapped bytes and 54 opaque bytes.
The opaque ranges are 24-byte entry adapter, 22-byte body setup/initialization and
8-byte epilogue. Calls, stack setup and returns in these ranges have no invented
Wasm PC or decoded operand semantics. Profile ownership may include cold code,
padding or literals; its boundaries may split objdump records, so gaps are raw bytes.

`operand_scope` is `supplied-source-regions`. `compared/known/unknown` count mapped
instructions only. `raw_complete` means the opaque ranges were aligned and compared;
it says nothing about mapped operand equivalence, and may remain true when mapped
alignment fails. Overall `complete=false` on unknown mapped instructions,
incompatible provenance/configuration, ambiguous mapped/raw alignment or limits.
Neither complete flag proves whole-image semantics or that changes are harmless.
Aligned raw differences are still reported under a configuration/provenance mismatch,
with both completion flags false; these observations do not waive qualification.

Examples of interpretation:

- `constant`: at function 0 / Wasm PC 31, ADD immediate 1 changed to 2. The opcode is still
  ADD; preserve and inspect that constant before attributing a performance effect.
- `width`: a GP stack store changed 32→64 bits. Its address/register facts stay visible;
  source/allocator evidence is needed before calling it an actual spill/reload.
- `register-dependency`: XOR of a register with itself has no input dependency;
  XOR with a different register reads both inputs. That distinction is retained.
- `raw-bytes`: the entry-adapter range changed, with no Wasm PC attached. This may
  reflect layout, relocation or lowering; no operand/root-cause classification is made.
- `unknown`: an unsupported mapped instruction retains raw bytes, source/function
  anchors and offsets and forces incomplete output, even if both byte strings match.

The JSON includes full before/after facts and offsets, mapped/raw changes separately,
and native/mapped/unmapped counts. [Saved output examples](../../../evidence/coverage/)
include self-comparison and synthetic constant, raw-range and unknown controls.
Synthetic record edits are never compiled, mapped or executed and are not after-images.

## Records, relocations and alignment

Admitted AMD64 forms: full GP32/64 MOV, register ADD/XOR/CMP, register ADD/SUB/CMP
immediate, base-plus-displacement GP MOV without indexing, JMP/Jcc rel8/32, and
three exact canonical NOPs. Records preserve widths, register reads/writes, signed
immediate extension, address base/displacement, GP32 zero extension and flag effects.
Initial ARM64 records support MOVZ, non-SP ADD immediate, unsigned-immediate GP
LDR/STR and direct B, including W-register zero extension and SP/XZR distinctions.
Calls, returns, partial GP writes, indexed memory, SIMD/FP and unsupported encodings
remain unknown or opaque. Native ARM64 capture/execution is not locally qualified;
word controls and cross-compilation support the portable record subset only.

Raw bytes are separate from normalized comparison bytes. Only admitted JMP/Jcc/B
with a matching explicit target identity receives displacement normalization. Jcc
condition, encoded width and flag dependencies stay visible; different/missing
identities do not receive a waiver. There is no register renaming, constant erasure
or normalization of opaque bytes. Call/return semantics and target normalization
for wrappers remain deliberately unsupported.

Mapped regions use function/Wasm-PC/occurrence identity and positional alignment.
PCs include local-declaration bytes. Inlined regions keep the logical callee/PC and
`inline_parent`, referencing a bounded caller-frame table. Physical byte ownership
uses the root caller, including full function indices with imports. Caller tables
and parent references must agree exactly across snapshots; changed/reordered caller
context is incomplete. Changes and unknowns retain logical locations and both caller
tables. Dangling, cyclic/forward or oversized ancestry is invalid. Insertions or
changed anchors/counts produce
incomplete results rather than guessed alignment. Raw ranges use profile kind,
signed function ownership (-1 for module/shared), owner occurrence and adjacent
mapped anchors or explicit owner-start/end sentinels. Offsets/sizes are observations,
not logical identities. A raw range can change length and still align by anchors.

The producer validates actual backend profile ownership and source boundaries.
Snapshot validation checks declared identities/anchor positions, hex, count
consistency and an exhaustive nonoverlapping mapped/raw partition when raw coverage
is present. User-supplied metadata is not authenticated backend ownership or proof
of every source map. Legacy snapshots without raw coverage remain readable and
explicitly lack whole-image byte accounting. Relocation targets remain restricted
to source-mapped instruction starts; opaque data never grants new branch waivers.

## Provenance, bounds and costs

Capture records executable/input/native SHA256, target, established direct-backend
path, baseline CPU mask, optimization-selection fingerprint, explicit bounds and
profile mode. Build-info revision/dirty status is used when available; recipes stamp
the build checkout. A stamp alone does not attest a clean checkout. Unstamped builds
without VCS information remain unqualified. Unknown names are not normalized away.

Limits: 1 MiB input JSON, 64 mapped regions, 4096 instructions, 128 profile owners/raw
ranges, 128 inline caller frames and 512 changes or unknown sites. Capture additionally caps trusted Wasm at
32 KiB, module/local counts, native bytes at 64 KiB and objdump output at 1 MiB/five seconds.
Capture output JSON is capped at 1 MiB. Native size is checked **after** compilation;
these are not a compiler heap quota or a claim to accept arbitrary untrusted inputs.
Alignment and partition validation are linear; no LCS, global cache or second profiler.

At frozen measured source 66dedc1d, short pairs against the previous integration
cut equal scalar comparison
medians 55–61%. Captured Fibonacci compare: 2.361→2.118 µs, 96→0 B and 8→0 allocations,
including the new opaque checks. A 32-change report: 32,192→26,912 B, six allocations.
Capture adds 1,252 B and 22 allocations (133,920 B / 835 allocations); ~11.4 ms includes
executable hashing and objdump, excluding JSON/file writing. Capture timing is noisy,
with no speed claim. The later inline-ownership and build-variant correctness fixes
were not rebenchmarked; ancestry validation and added payload fields have unmeasured
costs. The figures above remain historical frozen-source measurements. RSS and
peak/retained heap were not measured. Ordinary runtime
builds are byte-identical; no production optimization or runtime overhead is introduced.

[Acceptance checklist and remaining scope](../../../evidence/coverage/acceptance.md),
[results and reproducible measurements](../../../evidence/coverage/results.md), and
[independent review](../../../evidence/coverage/independent-review.md) qualify these
claims. Historical trial results remain in evidence/integration, including the
initial comparator slowdown and the non-reproduced tiny compile signal.

[Fresh production-isolation qualification](../../../evidence/isolation/README.md)
records explicit opt-in gating and matched current-main build evidence.
