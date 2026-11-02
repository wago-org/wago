**What does this PR do?**

Ports the Snap region collector to Wago and integrates the related GC rooting, lifetime, allocation, and collector fixes into one campaign PR.

**Joshua directed this PR to be opened early so Jairus can test, ahead of final qualification. It is ready for review, not a claim that CI, performance proof, or merge gates are complete.**

**How does this PR do it?**

Adds opt-in region Eden, bounded native reservations, mature/LOS descriptors, transactional evacuation and joined parallel collection while preserving stable handles and checked rollback. Integrates conditional container receipts and nested function-owner reachability, EH/frame-root repairs, TinyGo foreign-stack support and serial evacuation, and ARM64 lowering fixes. Default and supported-target distinctions are preserved in the evidence.

**What changes are made/proposed?**

- Collector controls and region/mature evacuation, including failure rollback, helper completion and reclamation.
- GC fix rollup from #997, #1003, #1019, #1020, #1026, #1027 and #1030, with provenance and adaptations recorded. Final gates-green inclusion verdicts remain pending; this PR does not close those topics. The supplied historical local heads for #997/#1003 were unavailable, so verified published heads were used and the gaps remain documented.
- Persistent provider roots, converted-reference publication receipts, nested owner/cycle lifetime, invocation snapshots and nonblocking retirement; Linux and Darwin ARM64 support, with unsupported-target admission kept explicit.
- Regression tests, immutable qualification receipts, native/QEMU/TinyGo checks, review findings and repairs. The retained evidence is extensive; failed historical runs are preserved rather than relabelled as passes.

**Current qualification — not all green**

| Native Darwin mode | Result at source `889b46c45` / harness `71fb2a16e` |
| --- | --- |
| ordinary | Full native qualification PASS, with recorded existing skips |
| wagodebug | Full native qualification PASS, with recorded existing skips |
| wago_regalloccheck | One unresolved `TestPDFiumCorpus` timeout in the race-detector suite; remaining command receipts pass |

[Native run 41501609461](https://github.com/wago-org/wago/actions/runs/41501609461): the checked race receipt has **zero data-race reports**, 10,818 passing test events, and one isolated PDFium child exceeding its unchanged **60-second deadline** (60.01s). This is a deadline failure, not a verified race defect. Ordinary/debug race PDFium runs passed at 24.23s/24.72s. Instrumentation overhead is plausible, but a port slowdown has not been established or ruled out.

[Targeted native diagnostic 41526277406](https://github.com/wago-org/wago/actions/runs/41526277406) is queued as of opening: baseline `9713878ab` versus candidate `889b46c45`, same native runner, `-race -tags=wago_regalloccheck`, unchanged 60-second child deadline and all PDFium oracles, with compile/execute phase diagnostics. It is diagnostic evidence, not a substitute for closing the failed full-race gate. The opening head adds documentation/harness work beyond the qualified pin; no full opening-head CI pass is claimed. Results and remaining qualification will be posted here as they land.

The campaign's review fan and scoped repair re-reviews are recorded; remaining qualification, review reconciliation and normal PR checks are still required. Windows storage contracts do not establish GC JIT qualification. Earlier target evidence must be read at its exact pin and scope.

**Deltas**

**The laptop benchmark proof has NOT run. The earlier strikingly large measurements have NOT been reproduced on the final code.** They are historical measurements pending proof, not current results; this PR makes no current speedup claim and supplies no stale delta table. Independent matched-pair reproduction of the final port against pinned `origin/master`, using the earlier workloads/protocol and controls, remains pending under the global lease, quiet-laptop and thermal discipline. Non-reproduction will be reported explicitly.

**Evidence and ownership**

- [Latest native audit and PDFium disposition](docs/rfwgc-evidence/native-run-41501609461/README.md)
- [Targeted discriminator](docs/rfwgc-evidence/pdfium-native-discriminator/README.md)
- [Combined correctness receipts and limitations](docs/rfwgc-evidence/final-combined-5b02/README.md)
- [Review consolidation and rollup provenance](docs/rfwgc-evidence/final-review-consolidated/README.md)
- [Owner's early-opening order](docs/rfwgc-evidence/campaign-endgame-policy/README.md)
- Owning T3 thread: [Read two files and run them together: /tmp/brief-wago-rfwgc-port-resume.txt and…](t3-thread://v1/mcp:0e526b5a-99e6-44dc-86d8-5a78d92beef4)

**Checklist:**
- [x] Regression tests and retained failure/repair evidence are included; open qualification is disclosed above.
- [ ] Final qualification and review reconciliation complete.
- [ ] Fresh laptop benchmark proof reproduced on final code.
- [ ] NOTICE / CHANGELOG / contribution checklist finalized before merge.
- [x] LLM assistance used.
