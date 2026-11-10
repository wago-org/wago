// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build linux && amd64 && !tinygo

package riscv64

import (
	core "github.com/wago-org/wago/src/core/runtime"
	"testing"
)

func TestCurrentCoreStackContract(t *testing.T) {
	if DefaultNativeStackBytes != core.DefaultNativeStackBytes || MinNativeStackBytes != core.MinNativeStackBytes || MaxNativeStackBytes != core.MaxNativeStackBytes || trapBufferBytes != core.TrapBufferBytes {
		t.Fatal("current runtime stack/trap contract drifted")
	}
	e, err := core.NewEngineWithStackBytes(core.MinNativeStackBytes)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	if e.StackTop()-e.StackLimit() != uintptr(core.MinNativeStackBytes-stackFenceMargin) {
		t.Fatal("current stack fence margin drifted")
	}
}
