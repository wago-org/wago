// Example 09: capabilities and policy.
//
// Plugins declare capabilities; a Policy decides which a given instance may use,
// and can cap resources like memory. A module needing a disallowed capability is
// rejected before it runs. Run:
//
//	go run ./examples/09-capabilities-policy
package main

import (
	"context"
	"errors"
	"fmt"

	wago "github.com/wago-org/wago"
	"github.com/wago-org/wago/examples/internal/mods"
	"github.com/wago-org/wago/plugins/timer"
)

func main() {
	rt := wago.NewRuntime()
	defer rt.Close()
	_ = rt.Use(timer.Ext()) // provides wago_timer.*, requires the timer.read capability

	mod, _ := rt.Compile(mods.ImportCaller("wago_timer", "now_unix_ms", "now", []byte{mods.I64}))
	fmt.Println("module requires capabilities:", mod.RequiredCapabilities())

	ctx := context.Background()

	// A policy that does NOT allow timer.read rejects the module.
	_, err := rt.Instantiate(ctx, mod, wago.WithPolicy(wago.Policy{
		AllowedCapabilities: []wago.Capability{wago.CapMetricsWrite}, // not timer.read
	}))
	fmt.Println("denied instantiate:", errors.Is(err, wago.ErrPermissionDenied), "-", err)

	// Allowing timer.read (and bounding memory) lets it run.
	inst, err := rt.Instantiate(ctx, mod, wago.WithPolicy(wago.Policy{
		AllowedCapabilities: []wago.Capability{timer.CapRead},
		MaxMemoryBytes:      16 << 20, // 16 MiB ceiling
	}))
	if err != nil {
		panic(err)
	}
	defer inst.Close()
	out, _ := inst.Call(ctx, "now")
	fmt.Printf("allowed: now() = %d ms\n", out[0].I64())
}
