# Reconstructed experimental pure sink

NEW source reconstruction on live main752e12f7 after workspace reset. This is not byte-identical recovery of lost measured source d5f547ec or docs87a1444e. No prior benchmark establishes performance of this branch.

Feature default OFF: WAGO_EXPERIMENTAL_PURE_LOCAL_SINK=1 enables it. Necessary empty-logical-stack callsite gate is ON by default; WAGO_PURE_LOCAL_SINK_ENTRY_GATE=0 disables only that gate for controlled comparison. Complete internal admission checks remain unchanged in either mode. Profile and regalloc-check builds deliberately fall back to legacy lowering, not positive candidate coverage.

Complete pinned integer constant assignment/tee transition bypasses intermediate nodes while retaining current Go encoder forms and local-write invalidation, dirty state and post-tee byte-swap selection. No runtime LLVM, online action list, module-code cache or full-function SSA. Reconstructed proofs and fresh native/metadata/output measurements are required before promotion.

Prior lost-workspace results (historical only): official145 first screen ON+4.42% versus same binaryOFF; favorable synthetic512-update loop ON−36.44%, with513 admitted transitions/mode. The latter is not a production gain. No PR or RSS claim was made. Current branch must be independently qualified and backed up before additional expensive work.

## Fresh recovered correctness gate

Production checkpoint26013b0592fcc67b16dc39fbc8f102e5c6cb6dfa, parent752e12f7; official Go1.27.1, GOMAXPROCS1, p1. Focused TestPureSink normal480 selected state/native cases plus guard and tee-byte-swap cases PASS0.011s; codegenstats PASS0.014s. Both have zero skips. Profile/checker remain intentional legacy-only configurations, not positive candidate proof.

New TestPureSinkRecoveredCorpus validates all145 artifact SHA256 values and modules, compiles OFF/ON in both interruptibility modes with Workers1 and deferred mapping. Candidate normal580 compiles PASS0.373s; stats580 PASS0.515s; independent pristine-main test-overlay580 PASS0.348s. These are test durations, not performance measurements. Full CompiledModule JSON OFF/ON is identical, including every exported metadata field. Independent main versus candidate normal/stats also has exact290 native and metadata pairs. Main and candidate-normal exported JSON SHA256: e47040b32882699e63be54c693e9790e7ea85952ff619b5952852ae066f9c7ab. Stats JSON SHA256:0084b25e730e44c0d19bfdf2dd05ae6414a91095a6565cd80956d4aff63ad6b8; only selection counts differ.

Positive diagnostic selections total3400 across102 workloads:1699 noninterruptible and1701 interruptible. This is fresh reconstruction evidence, not a claim of old-source identity. Logs and290-row exports reside in compiler-recovery/parity. Native parity is not execution timing, runtime output proof, RSS qualification, or a performance gain. Public-adapter freshprocess measurements remain pending.
