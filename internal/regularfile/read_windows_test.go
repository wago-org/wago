// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build windows

package regularfile

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestAtomicSnapshotRetriesWindowsSharing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "record")
	if err := os.WriteFile(path, []byte("selected"), 0600); err != nil {
		t.Fatal(err)
	}
	name, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	hold := func() syscall.Handle {
		handle, err := syscall.CreateFile(name, syscall.GENERIC_READ, 0, nil, syscall.OPEN_EXISTING, syscall.FILE_ATTRIBUTE_NORMAL, 0)
		if err != nil {
			t.Fatal(err)
		}
		return handle
	}
	t.Run("temporary", func(t *testing.T) {
		handle := hold()
		closed := make(chan error, 1)
		timer := time.AfterFunc(30*time.Millisecond, func() { closed <- syscall.CloseHandle(handle) })
		defer timer.Stop()
		data, err := ReadAtomicSnapshot(path, 32)
		closeErr := <-closed
		if err != nil || closeErr != nil || string(data) != "selected" {
			t.Fatalf("snapshot %q: %v, close %v", data, err, closeErr)
		}
	})
	t.Run("persistent", func(t *testing.T) {
		handle := hold()
		defer syscall.CloseHandle(handle)
		if _, err := ReadAtomicSnapshot(path, 32); !errors.Is(err, syscall.Errno(32)) {
			t.Fatalf("persistent sharing error = %v", err)
		}
	})
}
