// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build windows

package atomicfile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWindowsUmaskProbeIsHandleDeleted(t *testing.T) {
	dir := t.TempDir()
	mode, err := probeUmaskMode(filepath.Join(dir, "artifact.wago"), 0o644, true)
	if err != nil {
		t.Fatal(err)
	}
	if mode&0o200 == 0 {
		t.Fatalf("writable Windows probe mode = %03o", mode)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".wago-umask-") {
			t.Fatalf("umask probe was not deleted with its handle: %s", entry.Name())
		}
	}
}
