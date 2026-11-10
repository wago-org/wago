// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package regularfile

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadLimitsAndRegularFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "data")
	if err := os.WriteFile(path, []byte("1234"), 0600); err != nil {
		t.Fatal(err)
	}
	if data, err := Read(path, 4); err != nil || string(data) != "1234" {
		t.Fatalf("exact limit: %q, %v", data, err)
	}
	if _, err := Read(path, 3); err == nil {
		t.Fatal("accepted oversized file")
	}
	if _, err := Read(dir, 4); err == nil {
		t.Fatal("accepted directory")
	}
}
