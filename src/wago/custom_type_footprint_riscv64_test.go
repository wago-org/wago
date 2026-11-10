// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build linux && riscv64 && !tinygo

package wago

import (
	"fmt"
	"testing"
)

func rv64CustomWarmAllocations(t *testing.T, in *Instance, want uint64) float64 {
	t.Helper()
	fn, err := in.WasmFunc("run")
	if err != nil {
		t.Fatal(err)
	}
	session, err := fn.OpenSession()
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	return testing.AllocsPerRun(64, func() {
		got, err := session.Invoke0()
		if err != nil || len(got) != 1 || got[0] != want {
			panic(fmt.Sprintf("custom warm checksum failed: got=%v want=%d err=%v", got, want, err))
		}
	})
}
