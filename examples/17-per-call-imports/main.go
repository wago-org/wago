// Example 17: per-call imports and plugin registry.
//
// Beyond plugins registered on the runtime, you can supply extra imports for a
// single Instantiate with WithImports. You can also resolve plugins by name from
// the compile-time registry (the mechanism behind `wago run --plugin=...`). Run:
//
//	go run ./examples/17-per-call-imports
package main

import (
	"context"
	"fmt"

	wago "github.com/wago-org/wago"
	"github.com/wago-org/wago/examples/internal/mods"
	"github.com/wago-org/wago/plugins/timer"
)

func init() {
	// Register a plugin under a short name so it can be enabled by name. Plugins
	// normally do this in their own init(); the CLI does it for the built-ins.
	wago.RegisterExtension("timer", func() wago.Extension { return timer.Ext() })
}

func main() {
	fmt.Println("plugins compiled in:", wago.RegisteredPluginNames())

	rt := wago.NewRuntime()
	defer rt.Close()

	// Enable a plugin by name (as a CLI would from --plugin=timer).
	if err := rt.UsePlugin("timer"); err != nil {
		panic(err)
	}

	// A module needing both a plugin import (wago_timer) and an ad-hoc one (host.mul).
	mod, _ := rt.Compile(mods.SquareViaHost())
	ctx := context.Background()

	// Supply host.mul just for this instance via WithImports.
	inst, err := rt.Instantiate(ctx, mod, wago.WithImports(wago.Imports{
		"host.mul": wago.HostFunc(func(_ wago.HostModule, p, r []uint64) {
			r[0] = wago.I32(wago.AsI32(p[0]) * wago.AsI32(p[1]))
		}),
	}))
	if err != nil {
		panic(err)
	}
	defer inst.Close()

	out, _ := inst.Call(ctx, "square", wago.ValueI32(7))
	fmt.Printf("square(7) = %d\n", out[0].I32())
}
