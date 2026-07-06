// Example 06: using a built-in plugin (timer).
//
// Plugins register host imports into a Runtime under a reserved module namespace.
// rt.Use(timer.Ext()) makes wago_timer.* available to any guest the runtime runs.
// Run:
//
//	go run ./examples/06-plugin-timer
package main

import (
	"context"
	"fmt"

	wago "github.com/wago-org/wago"
	"github.com/wago-org/wago/examples/internal/mods"
	"github.com/wago-org/wago/plugins/timer"
)

func main() {
	rt := wago.NewRuntime()
	defer rt.Close()

	// Enable the timer plugin. Its imports (wago_timer.now_unix_ms, etc.) are now
	// available to every module this runtime instantiates.
	if err := rt.Use(timer.Ext()); err != nil {
		panic(err)
	}

	// A guest that imports wago_timer.now_unix_ms() -> i64 and re-exports it as now().
	mod, err := rt.Compile(mods.ImportCaller("wago_timer", "now_unix_ms", "now", []byte{mods.I64}))
	if err != nil {
		panic(err)
	}
	ctx := context.Background()
	inst, err := rt.Instantiate(ctx, mod)
	if err != nil {
		panic(err)
	}
	defer inst.Close()

	out, _ := inst.Call(ctx, "now")
	fmt.Printf("guest sees wall-clock time: %d ms\n", out[0].I64())

	// Plugins can be inspected.
	for _, info := range rt.Extensions() {
		fmt.Printf("loaded plugin: %s %s\n", info.ID, info.Version)
	}
}
