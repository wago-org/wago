// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build linux && amd64

package amd64

import (
	"strings"
	"testing"

	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestCompileRejectsCompleteFrameAboveStackFenceHeadroom(t *testing.T) {
	body := append([]byte{0x01}, wasmtest.ULEB(32768)...)
	body = append(body, 0x7e, 0x10, 0x00, 0x0b) // i64 locals; call self; end
	m := mod1(t, nil, nil, body)
	if _, err := CompileModule(m); err == nil || !strings.Contains(err.Error(), "exceeds stack-fence headroom") {
		t.Fatalf("complete-frame compile error = %v", err)
	}
}
