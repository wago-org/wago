// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build wago_lean

package run

import (
	"strings"
	"testing"
)

func TestLeanProfileRejectsWatch(t *testing.T) {
	_, err := Command(testEnvironment{}).Parse("wago run", []string{"--watch", "module.wasm"})
	if err == nil || !strings.Contains(err.Error(), "unknown flag --watch") {
		t.Fatalf("lean --watch error = %v", err)
	}
}
