// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build windows && !tinygo && !wago_lean

package run

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOpenWatchedFileAllowsReplacement(t *testing.T) {
	path := filepath.Join(t.TempDir(), "module.wasm")
	if err := os.WriteFile(path, []byte("first"), 0o600); err != nil {
		t.Fatal(err)
	}
	file, err := openWatchedFile(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if err := os.Remove(path); err != nil {
		t.Fatalf("remove open watched file: %v", err)
	}
}
