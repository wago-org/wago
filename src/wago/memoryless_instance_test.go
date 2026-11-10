// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build !tinygo

package wago

import (
	"context"
	"testing"
)

func TestMemorylessInstanceDoesNotExposeRuntimeScratchMemory(t *testing.T) {
	rt := NewRuntime()
	defer rt.Close()
	in, err := rt.Instantiate(context.Background(), mustCompileWat(rt, t, `(module (func (export "f")))`))
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if got := in.Memory(); got != nil {
		t.Fatalf("Memory() = %p, want nil for a memoryless module", got)
	}
}
