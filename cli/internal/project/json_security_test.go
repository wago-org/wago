// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package project

import (
	"strings"
	"testing"
)

func TestProjectRejectsDuplicateJSON(t *testing.T) {
	if _, err := decodeManifest([]byte(`{"plugins":{},"plugins":{}}`), t.TempDir()); err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("manifest duplicate error = %v", err)
	}
	for _, data := range []string{
		`{"formatVersion":1,"plugins":{},"Plugins":{}}`,
		`{"formatVersion":1,"plugins":{"x":{"grants":[],"Grants":[]}}}`,
		`{"formatVersion":1,"plugins":{"x":{"config":{"flag":false,"flag":true}}}}`,
	} {
		if _, err := DecodeLock([]byte(data)); err == nil || !strings.Contains(err.Error(), "duplicate") {
			t.Fatalf("lock duplicate error = %v", err)
		}
	}
}
