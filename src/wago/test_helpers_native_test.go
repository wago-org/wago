// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build ((linux || darwin || windows) && (amd64 || arm64)) || (linux && riscv64)

package wago

import (
	"errors"
	"testing"
)

func requireInterruptedTrap(t *testing.T, err error) {
	t.Helper()
	var trap *TrapError
	if !errors.As(err, &trap) || trap.Code != TrapInterrupted {
		t.Fatalf("error = %v, want interrupted trap", err)
	}
}

func mustCompileWat(rt *Runtime, t testing.TB, wat string) *Module {
	t.Helper()
	m, err := rt.Compile(watToWasm(t, wat))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	return m
}
