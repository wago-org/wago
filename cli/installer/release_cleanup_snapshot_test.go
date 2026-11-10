// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package installer

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCleanupProtectionDetectsReplacedRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "protected")
	if err := os.Mkdir(root, 0755); err != nil {
		t.Fatal(err)
	}
	protection, err := captureCleanupProtection([]string{root})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(root, root+"-old"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(root, 0755); err != nil {
		t.Fatal(err)
	}
	if err := protection.validate(); err == nil {
		t.Fatal("replaced protected directory accepted")
	}
}
