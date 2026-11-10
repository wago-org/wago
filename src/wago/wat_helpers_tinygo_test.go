// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build tinygo && (linux || darwin || windows) && (amd64 || arm64)

package wago

import "testing"

// TinyGo cannot shell out to WABT. These definitions keep benchmark and test
// files type-checkable; WAT-backed tests must call requireExternalWAT before
// attempting assembly.
func watToWasm(t testing.TB, _ string) []byte {
	t.Helper()
	t.Fatal("wat2wasm is unavailable under TinyGo")
	return nil
}

func watToWasmCA(t *testing.T, wat string) []byte {
	return watToWasm(t, wat)
}

func requireExternalWAT(t *testing.T) bool {
	t.Helper()
	t.Log("wat2wasm-backed test is unavailable under TinyGo")
	return false
}
