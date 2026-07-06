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
	compiled, err := wago.Compile(mods.Add())
	if err != nil {
		panic(err)
	}

	// Instantiate creates a runnable instance. This module has no imports.
	inst, err := wago.Instantiate(compiled, nil)
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
