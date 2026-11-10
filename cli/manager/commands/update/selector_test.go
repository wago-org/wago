// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package update

import (
	"strings"
	"testing"
)

func TestComponentPickerSelectsEverythingByDefault(t *testing.T) {
	picker := ComponentPicker()
	if got := strings.Join(picker.Chosen(), ","); got != "Manager,Runtime,Plugins" {
		t.Fatalf("default selection = %q", got)
	}
	frame := picker.Frame()
	for _, want := range []string{"Choose what to update", "Manager", "Runtime", "Plugins", "space toggle", "a toggle all", "enter/→ update"} {
		if !strings.Contains(frame, want) {
			t.Errorf("picker does not contain %q:\n%s", want, frame)
		}
	}
}
