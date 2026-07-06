// Example 07: combining plugins (log + metrics).
//
// A runtime can use many plugins at once. The metrics plugin also exposes a
// host-side Go API to read back what the guest recorded. Run:
//
//	go run ./examples/07-plugins-log-metrics
package main

import (
	"context"
	"fmt"
	"os"

	wago "github.com/wago-org/wago"
	"github.com/wago-org/wago/examples/internal/mods"
	"github.com/wago-org/wago/plugins/log"
	"github.com/wago-org/wago/plugins/metrics"
)

func main() {
	rt := wago.NewRuntime()
	defer rt.Close()

	// Configure plugins with options: send logs to stdout, keep metrics in memory.
	m := metrics.Ext() // default in-memory sink, queryable below
	if err := rt.Use(log.Ext(log.WithWriter(os.Stdout))); err != nil {
		panic(err)
	}
	if err := rt.Use(m); err != nil {
		panic(err)
	}

	ctx := context.Background()

	// A guest that logs a line at WARN (level 2).
	logMod, _ := rt.Compile(mods.LogCaller(2, "hello from the guest"))
	logInst, _ := rt.Instantiate(ctx, logMod)
	defer logInst.Close()
	_, _ = logInst.Call(ctx, "run")

	// A guest that bumps the "requests" counter by 3, called twice.
	metMod, _ := rt.Compile(mods.MetricsCaller("requests", 3))
	metInst, _ := rt.Instantiate(ctx, metMod)
	defer metInst.Close()
	_, _ = metInst.Call(ctx, "run")
	_, _ = metInst.Call(ctx, "run")

	// Read the counter back from the host side of the plugin.
	fmt.Printf("metrics: requests counter = %d\n", m.Counter("requests"))
}
