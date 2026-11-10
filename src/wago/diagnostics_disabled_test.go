// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build !wago_profile && !wago_codegenstats

package wago

import (
	"strings"
	"testing"
)

func TestCompilerTelemetryRequiresBuildTag(t *testing.T) {
	err := NewRuntimeConfig().WithGCCodeTelemetry(true).Validate()
	if err == nil || !strings.Contains(err.Error(), "wago_codegenstats") {
		t.Fatalf("expected explicit unavailable-telemetry error, got %v", err)
	}
}
