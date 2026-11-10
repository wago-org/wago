// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build !tinygo

package railshot

import (
	"bytes"
	"testing"
)

func TestInternalCompilerErrorCapturesBoundedStack(t *testing.T) {
	ice := NewInternalCompilerError("amd64", 0, -1, "invariant")
	if len(ice.Stack) == 0 || !bytes.Contains(ice.Stack, []byte("TestInternalCompilerErrorCapturesBoundedStack")) {
		t.Fatalf("stack context missing: %q", ice.Stack)
	}
}
