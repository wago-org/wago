// Example 12: processes and mailboxes (the actor model).
//
// Spawn runs a module on its own goroutine as a process with a mailbox. Send
// delivers a message; the guest blocks on recv until it arrives. Monitor reports
// the process's exit. Run:
//
//	go run ./examples/12-actors
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

	// Worker imports wago_process.self + wago_mailbox.recv; its main() blocks on
	// recv and returns the recv status code. Spawn injects those imports per-process.
	mod, _ := rt.Compile(mods.Worker())
	class, _ := rt.Class(mod, wago.ClassOptions{Pool: wago.PoolOptions{MaxInstances: 16}})
	defer class.Close()

	ctx := context.Background()
	pid, err := rt.Spawn(ctx, class, wago.SpawnOptions{Entry: "main"})
	if err != nil {
		panic(err)
	}
	fmt.Printf("spawned worker as pid %d\n", pid)

	// Watch for the worker's exit.
	exit, _ := rt.Monitor(ctx, pid)

	// The worker is blocked on recv; sending a message unblocks it and it exits.
	if err := rt.Send(ctx, pid, []byte("ping")); err != nil {
		panic(err)
	}

	ev := <-exit
	fmt.Printf("worker exited: normal=%v, recv status=%d\n",
		ev.Reason.Normal, statusOf(ev.Reason.Results))
}

func statusOf(results []wago.Value) int32 {
	if len(results) == 0 {
		return -1
	}
	return results[0].I32()
}
