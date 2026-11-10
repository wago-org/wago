// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build !windows

package atomicfile

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestReplaceFileApplyUmaskKeepsArtifactTempRestrictive(t *testing.T) {
	oldUmask := syscall.Umask(0)
	defer syscall.Umask(oldUmask)

	destination := filepath.Join(t.TempDir(), "artifact")
	err := ReplaceFile(destination, Options{Mode: 0o644, ModeSet: true, ApplyUmask: true}, func(writer io.Writer) error {
		file, ok := writer.(*os.File)
		if !ok {
			return fmt.Errorf("temporary writer type = %T", writer)
		}
		info, err := file.Stat()
		if err != nil {
			return err
		}
		if mode := info.Mode().Perm(); mode != 0o600 {
			return fmt.Errorf("artifact temporary mode = %03o, want restrictive 600", mode)
		}
		_, err = io.WriteString(writer, "complete")
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(destination)
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode != 0o644 {
		t.Fatalf("published mode = %03o, want umask-adjusted 644", mode)
	}
}
