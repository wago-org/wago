// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build arm64 && !wago_profile && !wago_codegenstats

package arm64

import (
	"strings"
	"testing"
)

func TestCompilerDiagnosticsRequireBuildTag(t *testing.T) {
	for _, opts := range []CompileOptions{{Stats: &ModuleStats{}}, {CollectInlineReport: true}} {
		_, err := CompileModuleWith(nil, opts)
		if err == nil || !strings.Contains(err.Error(), "wago_codegenstats") {
			t.Fatalf("expected explicit unavailable-diagnostics error, got %v", err)
		}
	}
	t.Setenv("WAGO_EXPLAIN", "1")
	if got := diagnosticEnv("WAGO_EXPLAIN"); got != "" {
		t.Fatalf("ordinary build read diagnostic environment: %q", got)
	}
}
