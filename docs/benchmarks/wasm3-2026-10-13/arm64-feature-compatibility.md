# ARM64 feature compatibility audit

On 2026-10-14, the Apple M4 Max ran all 373 contracts in the local wasm.fyi
`corpora/features/manifest.json`, covering 29 feature families. The retained
source was `191b7ceb01ef891361b4dd396a8c68c25d084b81`; its tracked Go and
assembly inputs match main `c4efb91b` (the subsequent commit changes documentation).
The manifest SHA-256 and exact input identity are archived with the run.

Results: **346 passed, 27 unsupported, zero failed**. Threads passed 84/84;
compact imports passed 9/9. Unsupported contracts comprise 15 JSPI, 9 JavaScript
string builtins, and 3 component async contracts. JSPI and JS strings lack the
corresponding adapter host profiles. Component async remains outside the
adapter's supported component compilation/phase contract; this is an explicit
adapter limitation, not evidence that guest execution passed.

The existing first-call-only feature compatibility policy was used, with one
launch, one sample and one operation, and independent corpus oracle checks.
All 355 successful producer trials (including component sacrificial trials)
record verified samples. The 373 capture outcomes are distinct from those
producer trial counts. This is corpus compatibility evidence, not complete
specification conformance. The separate V8 worker and upstream JavaScript runners
are not Wago contracts and were not substituted for Wago execution.

Raw captures, producer manifests/trials, unsupported reasons, ledger, inventory,
and per-family report are under
`.validation/wasm3-parity/arm-full-feature-compatibility-191b7ceb/`.
The companion JSON records family counts and exact unsupported reasons.

At the user's request, the local refresh combines these 373 compatibility
captures with the retained Apple performance pass (266 captures / 1064 phase
rows). No production deployment or remote push is authorized or performed.

## Matched w2c2 Clang compatibility run

The same 373 contracts ran through pinned w2c2
`f303ed27a8673d8cb7a6b7c39dd72a57a3b9999b` on the same Apple ARM64 host.
At the user's request, compatibility uses **Clang -O0**. All 373 producer
manifests record that flag. A separate controller source copy changes only the
pinned native compiler optimization flag; the translator, SDK, thread support
and other compiler flags remain unchanged. The partial -O3 run was stopped,
archived privately, and excluded from publication.

| Engine | Passed | Unsupported | Failed |
| --- | ---: | ---: | ---: |
| Wago | 346 | 27 | 0 |
| w2c2 Clang (-O0) | 169 | 111 | 93 |

All 373 artifact and contract digests match across engines. All 169 successful
peer producer trials have oracle-verified samples. The peer's 93 failed outcomes
retain the producer's original classification: **90 translation rejections and
3 guest traps**. Translation diagnostics include unsupported opcodes, module
formats and compact import kinds. They are not relabeled as unsupported.
Threads record 9 passed / 75 unsupported for peer versus Wago 84 passed;
compact imports record 3 passed / 6 failed for peer versus Wago 9 passed.
These counts include all corpus sizes and scalar baselines.

Peer raw evidence is in
`.validation/wasm3-parity/arm-full-feature-compatibility-w2c2-clang-o0-f303ed27/`.
The companion peer JSON records per-family outcomes and compiler verification.
The local publication adds 373 first-call compatibility rows. Existing optimized
performance captures remain preserved; the -O0 compatibility samples contain no
steady or compile performance rows.
