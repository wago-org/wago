#!/usr/bin/env bash
# Qualify manager installation and compiler-free standalone builds natively.
set -euo pipefail
[[ $(go env GOOS)/$(go env GOARCH) == linux/riscv64 ]]
[[ $(uname -m) == riscv64 ]]
go test -count=1 -timeout=10m \
  -run '^(TestRISCV64InstallBuildSelection|TestLinuxRISCV64StandaloneTarget|TestBuildEmbedsArtifactWithoutCompiler|TestCanaryWorkflowArtifactFailureBuildsExactSource|TestCanaryResolvesLatestArtifactAsSource|TestMissingReleaseAssetFallsBackToSource)$' \
  ./cli/manager/internal/version ./cli/manager/internal/standalone
