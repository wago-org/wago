// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package wago

import (
	"strings"
	"testing"
	"unsafe"
)

func TestSameExportName(t *testing.T) {
	const name = "run"
	if !sameExportName(name, name) {
		t.Fatal("identical export strings did not match")
	}
	cloned := strings.Clone(name)
	if unsafe.StringData(name) == unsafe.StringData(cloned) {
		t.Fatal("strings.Clone unexpectedly reused backing storage")
	}
	if !sameExportName(name, cloned) {
		t.Fatal("equal export strings with distinct backing storage did not match")
	}
	if sameExportName(name, "sum") || !sameExportName("", "") {
		t.Fatal("export-name equality mishandled unequal or empty names")
	}
}
