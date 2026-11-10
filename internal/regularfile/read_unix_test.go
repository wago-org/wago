// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build !windows

package regularfile

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestReadRejectsSymlinkAndFIFO(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "link")
	if err := os.Symlink(dir, path); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(path, 10); err == nil {
		t.Fatal("accepted symlink")
	}
	fifo := filepath.Join(dir, "fifo")
	if err := syscall.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(fifo, 10); err == nil {
		t.Fatal("accepted FIFO")
	}
}
