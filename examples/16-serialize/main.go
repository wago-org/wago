// Example 16: precompiling to a .wago blob.
//
// Compilation can be done ahead of time and the result serialized, so startup
// only pays for instantiation. MarshalBinary produces a portable blob; Load
// accepts either a .wago blob or raw wasm. Run:
//
//	go run ./examples/16-serialize
package main

import (
	"fmt"

	wago "github.com/wago-org/wago"
	"github.com/wago-org/wago/examples/internal/mods"
)

func main() {
	// Compile once...
	compiled, err := wago.Compile(nil, mods.Add())
	if err != nil {
		panic(err)
	}

	// ...serialize the precompiled module (write this to disk / cache it).
	blob, err := compiled.MarshalBinary()
	if err != nil {
		panic(err)
	}
	fmt.Printf("precompiled blob: %d bytes, is-wago-blob=%v\n", len(blob), wago.IsCompiled(blob))

	// Later, in a fresh process: Load accepts a blob or raw wasm transparently.
	loaded, err := wago.Load(blob)
	if err != nil {
		panic(err)
	}
	inst, _ := wago.Instantiate(loaded, wago.InstantiateOptions{})
	defer inst.Close()

	out, _ := inst.Invoke("add", wago.I32(19), wago.I32(23))
	fmt.Printf("loaded-from-blob add(19, 23) = %d\n", wago.AsI32(out[0]))
}
