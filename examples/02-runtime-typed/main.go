// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

// Example 02: runtime invocation.
//
// The high-level Runtime wraps compile/instantiate. InvokeContext accepts Wasm
// values encoded with wago.I32, wago.I64, wago.F32, and wago.F64. Run:
//
//	go run ./examples/02-runtime-typed
package main

import (
	"context"
	"fmt"

	wago "github.com/wago-org/wago"
	"github.com/wago-org/wago/examples/internal/mods"
)

func main() {
	rt := wago.NewRuntime()
	defer rt.Close()

	// rt.Compile returns a *Module (a runtime-aware wrapper over the compiled code).
	mod, err := rt.Compile(mods.Add())
	if err != nil {
		panic(err)
	}

	ctx := context.Background()
	inst, err := rt.Instantiate(ctx, mod)
	if err != nil {
		panic(err)
	}
	defer inst.Close()

	// The context is honored for cancellation.
	out, err := inst.InvokeContext(ctx, "add", wago.I32(2), wago.I32(3))
	if err != nil {
		panic(err)
	}
	fmt.Printf("add(2, 3) = %d\n", wago.AsI32(out[0]))

	// Inspect the module.
	fmt.Println("exports:", mod.Exports())
}
