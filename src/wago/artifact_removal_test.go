// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package wago

import (
	"testing"
)

func TestLoadRejectsRetiredArtifact(t *testing.T) {
	// The retired native-code header must never enter an executable mapping.
	if compiled, err := Load([]byte{'W', 'A', 'G', 'O', 7, 0x22, 1, 0, 2, 0}); err == nil || compiled != nil {
		if compiled != nil {
			_ = compiled.Close()
		}
		t.Fatalf("Load retired artifact = %v, %v; want rejection", compiled, err)
	}
}
