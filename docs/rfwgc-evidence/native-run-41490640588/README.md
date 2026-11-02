# Native run 41490640588 audit and packaging repair

Source `21d7be58952b64435110246f65d44e11046c5083`; harness `6d52c087f3136f0cb4d5457c733e1a92449df08b`.

## Verdict

Darwin ordinary executed and FAILED. Of 26 retained command result receipts, 25 pass (some with recorded skips); only `full-root-replay-ordinary` fails. Its three failure events describe two accidentally discovered evidence packages:

- `docs/rfwgc-evidence/native-run-41466556039/inputs`: archived runtime source cannot compile as a standalone package (`undefined: Compiled`, `Instance`, `gcConversionInvocation`, etc.).
- `docs/rfwgc-evidence/qemu-arm64-map-recheck`: archived `TestPlatformExecutionArchitecture` expects Linux and an explicitly supplied architecture, but observes Darwin/ARM64. This is an overlay probe, not a portable repository test.

No runtime defect established. Race, both reservation tests, two-result entry, full backend, both diagnostics routes, collector, cancellation, corpus, and exact Core 3/spec receipts pass. Original failures remain failures; skips are retained in the ZIP, not waived. Focused contracts and matched baseline summaries are retained separately.

Darwin debug and regalloccheck were cancelled before acquisition: zero steps, runner_id 0, 17:23:01–17:38:02 UTC. GitHub annotations report Mac capacity. They have no code verdict. Windows artifacts are retained; the owner's reported passes are separate from this ordinary-first audit.

## Repair and preservation

Eight archived `.go` payloads are renamed to `.go.txt`, byte-identically. `audit/rename-manifest.json` maps every original name to its new location and SHA-256. Historical launchers/manifests remain verbatim; when replaying one, use the mapping for its source payload paths. The native audit SHA256SUMS filenames are updated without changing payload digests. No production file, durable regression, assertion, timeout, workflow job, or compiler option changes.

Tracked-source discovery projections for Darwin ARM64 in all three modes contain 183 packages and zero docs packages. They use `go list -e`; this is package-discovery evidence, not a compilation or native test pass. Original ZIPs and full job logs are retained. All four artifact ZIP hashes match GitHub's API digests. Source/harness identity matches the requested pins. Native source-manifest full replay verification is not newly claimed here.

## Dispatch and deadline

Do not requeue the cancelled modes at the old source: both evidence packages are build-selected on Darwin in every mode, so the defect is mode-independent. Retarget the unchanged command plan to the packaging-repaired source, then rerun all three modes so ordinary's failed root gate is also repaired.

Repair cost: eight file renames plus metadata, no production rebuild design work. Native revalidation still requires fresh Mac capacity and a full run; the last ordinary job took 44m49s, an observation rather than a forecast. Wednesday remains at risk from BOTH repeated Mac acquisition cancellation and this executed packaging failure. No new run is claimed until dispatch succeeds. No benchmarks or measurements were taken; evidence-only changes cannot affect runtime performance.
