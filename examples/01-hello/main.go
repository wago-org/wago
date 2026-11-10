// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

// Example 01: hello — the lowest-level API.
//
// Compile a wasm module to native code, instantiate it, and invoke an export.
// This is the raw path (no Runtime, no plugins). Run:
//
//	go run ./examples/01-hello
package main

import (
	"fmt"

	wago "github.com/wago-org/wago"
	"github.com/wago-org/wago/examples/internal/mods"
)

func main() {
	// mods.Add() returns the bytes of a module exporting add(a, b i32) -> i32.
	// In a real project these bytes come from a .wasm file on disk.
	compiled, err := wago.Compile(nil, mods.Add())
	if err != nil {
		panic(err)
	}

	// Instantiate creates a runnable instance. This module has no imports.
	inst, err := wago.Instantiate(compiled, wago.InstantiateOptions{})
	if err != nil {
		panic(err)
	}
	defer inst.Close()

	// Invoke uses raw uint64 slots; encode/decode with I32/AsI32/etc.
	out, err := inst.Invoke("add", wago.I32(2), wago.I32(40))
	if err != nil {
		panic(err)
	}
	fmt.Printf("add(2, 40) = %d\n", wago.AsI32(out[0]))
}
