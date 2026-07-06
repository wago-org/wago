// Example 13: supervision.
//
// A supervisor spawns children and restarts them when they exit, per a strategy
// and a restart-intensity limit. This is the Erlang/OTP "let it crash" model. Run:
//
//	go run ./examples/13-supervisor
package main

import (
	"context"
	"fmt"
	"time"

	wago "github.com/wago-org/wago"
	"github.com/wago-org/wago/examples/internal/mods"
)

func main() {
	rt := wago.NewRuntime()
	defer rt.Close()

	mod, _ := rt.Compile(mods.Worker())
	class, _ := rt.Class(mod, wago.ClassOptions{Pool: wago.PoolOptions{MaxInstances: 16}})
	defer class.Close()

	ctx := context.Background()

	// OneForOne: only the child that exits is restarted. Up to 5 restarts / minute.
	sup, err := rt.Supervise(ctx,
		wago.SupervisorOptions{Strategy: wago.OneForOne, MaxRestarts: 5, Window: time.Minute},
		wago.ChildSpec{Name: "worker-a", Class: class, Spawn: wago.SpawnOptions{Entry: "main"}},
		wago.ChildSpec{Name: "worker-b", Class: class, Spawn: wago.SpawnOptions{Entry: "main"}},
	)
	if err != nil {
		panic(err)
	}
	defer sup.Stop()

	before := sup.Children()
	fmt.Printf("children: %v\n", before)

	// Kill worker-a; the supervisor restarts it with a new pid, leaving b alone.
	_ = rt.Kill(ctx, before[0], wago.ExitReason{})
	waitFor(func() bool {
		c := sup.Children()
		return c[0] != 0 && c[0] != before[0]
	})
	after := sup.Children()
	fmt.Printf("after killing worker-a: %v (a restarted, b unchanged=%v)\n",
		after, after[1] == before[1])
}

func waitFor(cond func() bool) {
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
}
