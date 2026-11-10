// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build !windows

package standalone

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStripTinyGoLinuxUsesPortableOptions(t *testing.T) {
	dir := t.TempDir()
	strip := filepath.Join(dir, "strip")
	if err := os.WriteFile(strip, []byte("#!/bin/sh\n[ \"$#\" -eq 2 ] && [ \"$1\" = -s ]\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	if err := stripTinyGo(dir, os.Environ(), false, "program", Target{OS: "linux", Arch: "amd64"}); err != nil {
		t.Fatalf("strip with portable implementation: %v", err)
	}
}
