// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build linux || darwin

package plugin

import (
	"os/exec"
	"path/filepath"
	"testing"
)

func TestBoundStagedRuntimeStartFailureDoesNotPanic(t *testing.T) {
	command := exec.Command(filepath.Join(t.TempDir(), "missing-helper"))
	if err := runBoundStagedRuntime(command); err == nil {
		t.Fatal("missing validation helper unexpectedly started")
	}
}
