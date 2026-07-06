// Example 02: runtime + typed Call.
//
// The high-level Runtime wraps compile/instantiate and gives you a typed,
// context-aware Call where arguments and results are checked against the export's
// signature. Run:
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

	// Call takes typed Values and returns typed Values — no manual slot encoding.
	// The context is honored for cancellation.
	out, err := inst.Call(ctx, "add", wago.ValueI32(2), wago.ValueI32(3))
	if err != nil {
		panic(err)
	}
	fmt.Printf("add(2, 3) = %d (type %s)\n", out[0].I32(), out[0].Type())

	// Inspect the module.
	fmt.Println("exports:", mod.Exports())
}
