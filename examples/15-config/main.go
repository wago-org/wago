// Example 15: runtime configuration.
//
// A RuntimeConfig controls feature gating, bounds checks, and compile-worker
// policy. Pass it to a Runtime with WithRuntimeConfig, or to the low-level
// CompileWithConfig. Run:
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
	// Compile workers are serial by default. Zero opts into the adaptive policy;
	// one forces serial compilation, and values above one are worker maxima. The
	// effective count is still capped by GOMAXPROCS and the local-function count.
	cfg := wago.NewRuntimeConfig().
		WithDeferBoundsChecks(false).
		WithCompileWorkers(0)

	fmt.Println("features:", cfg.CoreFeatures())
	fmt.Println("bounds checks:", cfg.BoundsChecks())
	fmt.Println("compile workers:", cfg.CompileWorkers())

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
