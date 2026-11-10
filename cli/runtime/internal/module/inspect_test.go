// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package module

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCompileEmptyModule(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.wasm")
	if err := os.WriteFile(path, []byte{'\x00', 'a', 's', 'm', 1, 0, 0, 0}, 0o600); err != nil {
		t.Fatal(err)
	}
	rt, mod := Compile(path)
	defer rt.Close()
	defer mod.Close()
	if got := mod.Imports(); len(got) != 0 {
		t.Fatalf("imports = %#v, want none", got)
	}
	if got := mod.RequiredCapabilities(); len(got) != 0 {
		t.Fatalf("capabilities = %#v, want none", got)
	}
}
