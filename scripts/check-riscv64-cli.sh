#!/usr/bin/env bash
# This Source Code Form is subject to the terms of the Mozilla Public
# License, v. 2.0. If a copy of the MPL was not distributed with this
# file, You can obtain one at https://mozilla.org/MPL/2.0/.
#
# SPDX-License-Identifier: MPL-2.0
# Copyright (c) 2026 Wago authors and contributors

# Qualify manager installation and compiler-free standalone builds natively.
set -euo pipefail
[[ $(go env GOOS)/$(go env GOARCH) == linux/riscv64 ]]
[[ $(uname -m) == riscv64 ]]
go test -count=1 -timeout=10m \
  -run '^(TestRISCV64InstallBuildSelection|TestLinuxRISCV64StandaloneTarget|TestBuildEmbedsArtifactWithoutCompiler|TestCanaryWorkflowArtifactFailureBuildsExactSource|TestCanaryResolvesLatestArtifactAsSource|TestMissingReleaseAssetFallsBackToSource)$' \
  ./cli/manager/internal/version ./cli/manager/internal/standalone
