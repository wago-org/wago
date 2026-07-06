// Example 15: runtime configuration.
//
// A RuntimeConfig controls feature gating and the bounds-check strategy. Pass it
// to a Runtime with WithRuntimeConfig, or to the low-level CompileWithConfig. Run:
//
//	go run ./examples/15-config
package main

import (
	"context"
	"fmt"

	wago "github.com/wago-org/wago"
	"github.com/wago-org/wago/examples/internal/mods"
)

func main() {
	// Bounds-check modes: explicit (inline checks) vs signals-based (guard-page).
	// Here we ask for every access to be bounds-checked (no elision).
	cfg := wago.NewRuntimeConfig().WithDeferBoundsChecks(false)

	fmt.Println("features:", cfg.CoreFeatures())
	fmt.Println("bounds checks:", cfg.BoundsChecks())

	rt := wago.NewRuntime(wago.WithRuntimeConfig(cfg))
	defer rt.Close()

	mod, err := rt.Compile(mods.Add())
	if err != nil {
		panic(err)
	}
	ctx := context.Background()
	inst, _ := rt.Instantiate(ctx, mod)
	defer inst.Close()

	out, _ := inst.Call(ctx, "add", wago.ValueI32(1), wago.ValueI32(2))
	fmt.Printf("add(1, 2) = %d\n", out[0].I32())
}
