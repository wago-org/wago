// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build ((linux || darwin || windows) && (amd64 || arm64) && !tinygo) || (linux && riscv64 && !tinygo && !wago_target_tinygo)

package wago

import (
	"testing"
	"time"

	"github.com/wago-org/wago/tests/support/regressiontest"
)

func runRegressionIsolatedPortTest(t *testing.T) bool {
	t.Helper()
	return regressiontest.RunIsolated(t, regressiontest.Timeout(t, 30*time.Second))
}
