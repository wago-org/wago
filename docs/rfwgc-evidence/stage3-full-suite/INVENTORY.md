# Independent gate inventory

Inventory source: immutable d546a1af4513c63e4bda6fae7d19cf9cd86f332a,
`.github/workflows/ci.yml`, `.github/workflows/regression-stress.yml`,
`.just/test.just`, `.just/spec.just`, `go.work`.

Root `go test ./...` does not traverse the two nested modules:
`cli/wago-installer` and `bench`. The driver runs both in all three modes.
The bench driver runs correctness tests only (no `-bench`), including the
normally excluded corpus tests with their default quick profile. The separate
corpus-all gate selects all corpus/application correctness workloads explicitly.
No failed test is excluded with `-skip`.

Required test-unit recipe also builds root and runtime-tagged CLI binaries;
tests compile those packages, and runtime-tagged CLI tests run in all modes.
Independent CI correctness gates include focused race detection, GC hardening,
guard-page paths, opt-in native-comparison diagnostics, diagnostic DCE,
portable 386 execution, Core v1 and exact Core v2, Core v3 explicit/signals,
corpus/application shard accounting, bounded fuzzing, and TinyGo qualification.
The root suite includes exact Core v2 validation/execution when its pinned
fixture and WABT are available. The independent driver supplements this with
race, GC package repetitions, guard-page, all corpus, and v1/v3 suites.

Other required CI jobs include docs, lint/generated/vet/staticcheck/actionlint,
release-script contracts, module-download contracts, regression provenance and
conditional corpus rebuild, current-Go build, and build-size qualification.
The root suite is not the complete CI job matrix. Size/performance work is
outside this worker's authorization; no benchmark or size measurement is run.
Fuzz seed tests run normally, but bounded fuzz campaigns are separate gates.
Other native platforms (Linux arm64, Darwin amd64/arm64, Windows amd64/arm64)
and RISC-V lanes require independent execution qualification. TinyGo-specific
root tests run if tools are available, but do not replace the full TinyGo CI lane.

Region mode is opt-in via nonzero NurseryRegionShift. Default-policy spec and
corpus conformance is not blanket region-enabled conformance. Actual region
coverage in the pinned runtime is six TestEdenRegions collector tests and
TestGCEdenRegionsCallerRoot (lifetime off/on × shift 0/6); exact outcomes are
extracted in summary.json. The guest test is Linux/amd64, ordinary Go,
non-guardpage only. New host integration tests added by other workers are
outside this immutable snapshot and need their own qualification.
