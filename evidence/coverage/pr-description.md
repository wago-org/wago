**What does this PR do?**

Adds an opt-in native comparison diagnostic for optimization experiments. Opcode-only
comparisons can hide constants, widths and false dependencies; the new report links
admitted AMD64/ARM64 operand records to supplied Wasm source anchors and retains the
remaining native bytes as opaque owned ranges. Refs #815.

**How does this PR do it?**

Reuses the existing Railshot profile/source-map contracts and GNU objdump, with bounded
positional alignment and explicit provenance/coverage. It introduces no production
imports, compiler/runtime instrumentation or host/guest boundary changes.

```sh
just bench native-capture tests/fixtures/wasm/fib.wasm /tmp/fib-before.json
# Same diagnostic/schema, input and settings in candidate compiler checkout:
just bench native-capture tests/fixtures/wasm/fib.wasm /tmp/fib-after.json
just bench native-compare /tmp/fib-before.json /tmp/fib-after.json
just test native-compare
```

Comparison JSON goes to stdout and coverage to stderr. Build a standalone command for
exit codes 0=complete (changes allowed), 2=invalid and 3=incomplete; go run wraps failures
as 1. Capture requires AMD64/profile mode and existing GNU objdump. No uploads/install.
For an older compiler checkout, transplant the same diagnostic into an isolated
checkout, stamp its compiler base revision and record the dirty build. Legacy partial
coverage versus full raw coverage is incomplete.

**What changes are made/proposed?**

- Explicit initial GP instruction records preserve immediates, widths, zero extension,
  address displacements, register/flag dependencies and admitted direct branch targets.
  All 16 AMD64 Jcc conditions and ARM64 MOVZ/ADD/LDR/STR/B controls are covered.
- Identical mapped pairs decode once. Matching explicit direct-target identities alone
  permit displacement normalization; constants, conditions and widths stay distinct.
  Calls/returns/stack implicit effects, FP/SIMD/indexed/partial forms remain unknown.
- Profile-owned raw gaps plus mapped ranges form a checked, exhaustive nonoverlapping
  byte partition. Fibonacci accounts for 105 bytes: 51 mapped and 54 opaque across a
  24-byte entry adapter, 22-byte body setup and 8-byte epilogue. No invented Wasm PCs.
- Unknowns, configuration/provenance differences, ambiguous alignment and limits make
  the report incomplete. Aligned raw differences remain observations under mismatched
  settings, with completion false. Raw bytes never receive relocation normalization.
- Portable checked controls are discovered by existing tests; a focused profile recipe,
  CONTRIBUTING link, CHANGELOG entry, README, written acceptance table, output examples,
  raw measurements and independent review make the workflow reproducible.

`operand_scope=supplied-source-regions` qualifies the operand counts. `raw_complete`
qualifies aligned opaque ranges only; neither it nor overall completion proves semantic
equivalence, whole-image operand coverage, hotness or automatic root cause. Imported
JSON ownership is structurally checked, not authenticated backend provenance. Bounds:
1 MiB JSON, 64 mapped regions, 128 raw ranges/owners, 4096 instructions, 512 reports,
32 KiB trusted Wasm and 64 KiB native image (checked after compilation). Linear checks;
no unrestricted LCS, retained cache or compiler heap quota.

**Deltas**

Against current main, ordinary AMD64 runtime builds are byte-identical: 14,755,751 bytes,
SHA256 3552d4c0b92f427dcbfcae463b20ae89c0e01690cd55c07da064104e2d17b13e. There is no
production speed claim. Historical small paired compile medians vary −0.87% to +0.79%,
execution +0.41%, with A/A variation ~1%; an initial tiny compile signal did not reproduce.
Main has no comparator, so diagnostic deltas use the prior integration 8e82e1b2 as control.

| Diagnostic workload | Before median (range) | After median (range) | Median delta | B/op before→after; allocs |
|---|---:|---:|---:|---|
| Equal GP records (16) | 2.407 µs (2.392–2.845) | 1.082 µs (1.071–1.092) | -55.05% | 0→0; 0→0 |
| Equal GP records (256) | 36.244 µs (35.818–36.954) | 14.034 µs (13.954–14.437) | -61.28% | 0→0; 0→0 |
| Equal GP records (4096) | 576.848 µs (574.665–579.196) | 226.627 µs (225.982–227.376) | -60.71% | 0→0; 0→0 |
| 32 changed memory records | 9.684 µs (6.809–19.913) | 8.193 µs (6.982–8.956) | -15.40% | 32,192→26,912; 6→6 |
| Captured fib compare | 2.361 µs (2.320–2.366) | 2.118 µs (2.054–2.132) | -10.29% | 96→0; 8→0 |
| Fib capture | 11.208 ms (11.131–11.993) | 11.358 ms (10.565–11.770) | +1.34% | 132,668→133,920; 813→835 |

One worker, CPU 15, five alternating 100-iteration portable pairs and three profile
pairs (100 compares/five captures), builds completed before timing. Changed-report and
capture timings are noisy; no capture speed claim. Capture includes compilation,
executable hashing and objdump, excludes JSON/files, and adds allocations. B/op is not
RSS, peak or retained memory, which were not measured. This is a short bounded trial,
not a production workload performance study.

The [workflow README](https://github.com/wago-org/wago/blob/experiment/operand-comparison-815/tests/tools/native-compare/README.md), [results](https://github.com/wago-org/wago/blob/experiment/operand-comparison-815/evidence/coverage/results.md), [reproduction commands](https://github.com/wago-org/wago/blob/experiment/operand-comparison-815/evidence/coverage/reproduce.md), and [independent review](https://github.com/wago-org/wago/blob/experiment/operand-comparison-815/evidence/coverage/independent-review.md) retain the evidence.

**Validation and acceptance**

The five literal issue criteria have bounded supporting evidence in
[evidence/coverage/acceptance.md](https://github.com/wago-org/wago/blob/experiment/operand-comparison-815/evidence/coverage/acceptance.md).
Controlled paired bytes distinguish the opcode-only misses; unknown records remain
unknown; real fib/source/objdump and selected XOR attribution agree; six public API XOR
oracles and fib(20)=6765/fib(30)=832040 execute through the supported API/runtime.
Synthetic modified record controls are not executed or presented as captured after-images.

Focused ordinary/checked/profile suites, final just recipe, vet, ARM64 checked/profile
cross-build, inherited ARM64 encoder tests, formatting and docs validation (97 files)
pass. The checked benchmark-module suite passes. Completed ordinary root tests fail
TinyGo VCS-stamp discovery and the absent pinned spec-v3 corpus; the TinyGo package
passes with process-local GOFLAGS=-buildvcs=false. The checked root run with that
workaround fails only the absent spec-v3 corpus; all other packages pass. Unchanged
main selected staged controls reproduce the same missing-corpus error. The interrupted
app-update attempt and actual completed failure logs are retained; no full-root pass
is claimed. A formatting recipe's read-only sandbox temp-path error was resolved with
a private process-local runtime directory, without changing shared settings.

Independent review found no source blocker and separately checked full-image partition,
qualification, timing medians and binary identity. README and results retain limitations
and historical negative findings. Native ARM64 capture/execution cannot be qualified
on this AMD64 host; portable word controls/cross-build do not imply that qualification.

**Keep/revise decision**

Keep this bounded diagnostic, keep this PR draft. To classify opaque wrapper bytes as
operands, a separate architectural decision must supply authoritative code-versus-data
boundaries, stable entry/internal-call identities and stack/call/return implicit effects;
profile byte ownership alone is insufficient. Broader ISA, insertion alignment and
hardware qualification are deliberately outside the initial subset.

Draft CI runs limited Linux AMD64 CI smoke and GC draft smoke; it skips the full
native/conformance/benchmark matrix. Draft smoke must not be reported as full CI.
No merge, issue closure, readiness change or manual full-CI dispatch is requested.

**Checklist:**
- [x] I've added proper tests. All fixed bugs have a corresponding test
- [x] I've read the [contributing guidelines](https://github.com/wago-org/wago/blob/main/CONTRIBUTING.md)
- [x] I've added my name and email to [NOTICE](https://github.com/wago-org/wago/blob/main/NOTICE) (Joshua Tenner already listed)
- [x] I've updated [CHANGELOG](https://github.com/wago-org/wago/blob/main/CHANGELOG.md)
- [x] I use an LLM to assist with this PR

Codex assisted with implementation, controls, documentation, paired measurement and
an independent agent review. Presubmit failures and hardware limits remain explicit.
