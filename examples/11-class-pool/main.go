// Example 11: Class and instance pooling.
//
// A Class is "compile once, instantiate many": a module plus a pool of reusable
// instances. Acquire leases one (reusing a warm instance or creating a new one
// under the cap); Release resets it to the module's initial state and returns it.
// Run:
//
//	go run ./examples/11-class-pool
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

	// Counter has a mutable global; observing it reset between leases proves the
	// pool hands out fresh state.
	mod, _ := rt.Compile(mods.Counter())
	class, err := rt.Class(mod, wago.ClassOptions{
		Name: "counter",
		Pool: wago.PoolOptions{MinInstances: 2, MaxInstances: 8, Reset: wago.ResetReinstantiate},
	})
	if err != nil {
		panic(err)
	}
	defer class.Close()

	ctx := context.Background()

	// Lease an instance, mutate it, release it.
	lease, _ := class.Acquire(ctx)
	out, _ := lease.Instance().Call(ctx, "inc")
	fmt.Printf("first lease:  inc() = %d\n", out[0].I32())
	out, _ = lease.Instance().Call(ctx, "inc")
	fmt.Printf("first lease:  inc() = %d\n", out[0].I32())
	_ = lease.Release()

	// The next lease starts from a reset state (count back to 0).
	lease2, _ := class.Acquire(ctx)
	out, _ = lease2.Instance().Call(ctx, "inc")
	fmt.Printf("second lease: inc() = %d  (state was reset)\n", out[0].I32())
	_ = lease2.Release()
}
