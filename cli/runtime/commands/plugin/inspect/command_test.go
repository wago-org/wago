// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package inspect

import "testing"

func TestPackageArgumentIsOptionalForInteractiveSelection(t *testing.T) {
	if got := Command().Args; got != "[plugin-id]" {
		t.Fatalf("inspect args = %q", got)
	}
}
