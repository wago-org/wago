// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package cipolicy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestJustInstallUsesCurrentHeadAndLocalInstallerOverrides(t *testing.T) {
	recipe, err := os.ReadFile(filepath.Clean("../../../.just/install.just"))
	if err != nil {
		t.Fatal(err)
	}
	contents := string(recipe)
	for _, required := range []string{
		"git archive HEAD",
		"git rev-parse HEAD",
		`WAGO_MANAGER_PATH="$manager_path"`,
		`WAGO_MANAGER_SOURCE="$source_dir"`,
	} {
		if !strings.Contains(contents, required) {
			t.Errorf("just install does not preserve local HEAD installation rule %q", required)
		}
	}
}
