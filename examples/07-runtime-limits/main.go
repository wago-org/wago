// Example 07: runtime instance limits.
//
// A closed instance returns its admission budget to the runtime. Run:
//
//	go run ./examples/07-runtime-limits
package main

import (
	"context"
	"errors"
	"fmt"

	wago "github.com/wago-org/wago"
	"github.com/wago-org/wago/examples/internal/mods"
)

func main() {
	config := wago.NewRuntimeConfig().WithInstanceLimits(1, 0)
	if err := config.Validate(); err != nil {
		panic(err)
	}

	runtime := wago.NewRuntime(wago.WithRuntimeConfig(config))
	defer runtime.Close()
	module, err := runtime.Compile(mods.Counter())
	if err != nil {
		panic(err)
	}
	defer module.Close()

	first, err := runtime.Instantiate(context.Background(), module)
	if err != nil {
		panic(err)
	}

	_, err = runtime.Instantiate(context.Background(), module)
	fmt.Println("second instance rejected:", errors.Is(err, wago.ErrPermissionDenied))

	stats := runtime.ResourceStats()
	fmt.Printf("live instances: %d of %d\n", stats.DirectInstances, stats.MaxDirectInstances)

	if err := first.Close(); err != nil {
		panic(err)
	}
	replacement, err := runtime.Instantiate(context.Background(), module)
	if err != nil {
		panic(err)
	}
	defer replacement.Close()
	fmt.Println("budget reused: true")
}
