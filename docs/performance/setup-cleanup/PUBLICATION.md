# Pull request publication

The user authorized publication on 2026-09-21. The original Wago working tree and main branch remain unchanged. Unrelated .gitignore edits, AGENTS.md, BUG_REVIEW.md, standalone Wasm files, other notes, and go.work.sum are excluded from the pull request. All original setup/cleanup measurements are retained. No benchmark or production source was changed during publication.

The Wago branch is `perf/setup-cleanup-measured`, based on `95be283fa511db7b01d86ef72b6c58fbe1ab607a`. Its first commit, `f4ba25bca6603271790de6c7c346cd47c7de4575`, contains only the snapshot fix and its tests/benchmark. The second commit contains diagnostics, lifecycle tests, measurements, and this report. The unchanged dependency pin means the provider's additional 70-allocation reduction is not enabled by merging the Wago PR alone.

The provider change is [wasi#21](https://github.com/wago-org/wasi/pull/21), commit `af5933cb51a226d535e7415ba2cc31fa6ebfa027`, on `jtenner/wasi:perf/wasi-construction`. It applies the saved production and test patches to current provider main, `aef440ccb4368b87f122038cf31295bba1b4bd46`. The performance base remains `6a6684d2ecd2be2d17e5792733d1d0e03b2f2c0e`. These revisions have different dependency pins and release metadata, so the published PR does not claim a repeated full performance comparison on its new base. The provider's original core.go is identical across those bases; the candidate core.go still hashes to `8f580b186e2a3013e6919faa549feda07b78325b1d70714e4fedead0c6a2e17d`. No unrelated provider changes or dependency upgrades are included.

A temporary external Go workspace selected the publication Wago checkout, its bench and installer modules, and the provider checkout. It was not added to either repository. Go 1.27.1 and GOMAXPROCS=16 were used for the publication checks. These checks were not performance measurements.

Publication checks passed:

- `just lint` before the Wago commits. Its recipe continues to report and allow the existing standard-staticcheck findings after runtime-tagged checks pass.
- `go test -count=1 ./src/wago -run Import` and `go test -race -count=1 ./src/wago -run Import` with the pinned WABT binary on PATH. The initial isolated checkout lacked the pinned spec-v3 test fixtures; both commands failed on missing fixtures. After copying those fixtures from the original checkout, both passed without a source change.
- Provider `go vet ./...` and `go test -race -count=1 ./...`, with the temporary Wago workspace; both also passed with `GOWORK` unset and the provider's own pinned dependency.
- With the temporary workspace and `WAGO_BOUNDS=signals`: `go test -race -count=1 -tags wago_guardpage ./bench/suite -run 'ImportLifecycle|WASIConstruction|WASIRegistration|MinimalWASI|WorkerDiagnostic'` and `go test -count=1 -tags wago_guardpage ./src/wago`.
- `go run ./tests/tools/docs-check` and the staged source whitespace check.

[Publication check logs](publication) supplement the unchanged investigation records. No additional full benchmark or broad TinyGo run was needed to package the identical measured source. The two known TinyGo linker failures and all measurement limits in the main report remain explicit. GitHub CI status is separate from these local checks. No merge or release was requested or performed.
