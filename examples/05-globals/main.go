// Example 05: exported globals.
//
// Modules can export mutable globals; the host can read and write them by name,
// typed. Run:
//
//	go run ./examples/05-globals
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

	// Counter exports a mutable i32 global "count" and inc() -> i32.
	mod, err := rt.Compile(mods.Counter())
	if err != nil {
		panic(err)
	}
	ctx := context.Background()
	inst, err := rt.Instantiate(ctx, mod)
	if err != nil {
		panic(err)
	}
	defer inst.Close()

	// Each inc() bumps the global.
	for i := 0; i < 3; i++ {
		out, _ := inst.Call(ctx, "inc")
		fmt.Printf("inc() = %d\n", out[0].I32())
	}

	// Read the global directly, typed.
	v, _ := inst.GlobalValue("count")
	fmt.Printf("count global = %d\n", v.I32())

	// Set it from the host, then observe the guest continue from there.
	_ = inst.SetGlobalValue("count", wago.ValueI32(100))
	out, _ := inst.Call(ctx, "inc")
	fmt.Printf("after set to 100, inc() = %d\n", out[0].I32())
}
