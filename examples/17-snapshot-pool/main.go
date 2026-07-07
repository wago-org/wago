// Example 17: snapshots and instance pools.
//
// A snapshot captures a module's initialized (or warmed-up) memory + globals so
// fresh instances can be created in that exact state without re-running init.
// Snapshots stay in local memory by default and can be serialized to a blob for
// disk. An InstancePool restored from a snapshot leases instances and resets them
// to the captured state between uses — the fast path for per-request isolation.
//
//	go run ./examples/17-snapshot-pool
package main

import (
	"context"
	"fmt"

	wago "github.com/wago-org/wago"
	"github.com/wago-org/wago/examples/internal/mods"
)

func main() {
	// Compile a module with a mutable global `count`, bumped by `inc`.
	compiled, err := wago.Compile(nil, mods.Counter())
	if err != nil {
		panic(err)
	}

	// Warm snapshot: run `inc` twice at capture time, then snapshot the state.
	snap, err := wago.Capture(compiled, wago.SnapshotOptions{
		Kind:     wago.SnapshotWarm,
		WarmFunc: "inc",
	})
	if err != nil {
		panic(err)
	}

	// Every instance restored from the snapshot starts warm (count == 1 here,
	// since the warm func ran once) — no start/init replay.
	inst, err := wago.Instantiate(snap, wago.InstantiateOptions{})
	if err != nil {
		panic(err)
	}
	warm, _ := inst.Global("count")
	fmt.Printf("restored instance starts at count = %d\n", wago.AsI32(warm))
	inst.Close()

	// Snapshots are self-contained blobs: write to disk, reload elsewhere.
	blob, _ := snap.MarshalBinary()
	fmt.Printf("snapshot blob: %d bytes, is-snapshot=%v\n", len(blob), wago.IsSnapshot(blob))

	// A pool leases instances and resets them to the snapshot between uses, so
	// each Invoke sees the same warm starting state regardless of prior calls.
	pool, err := wago.Pool(snap, wago.SnapshotPoolOptions{MinIdle: 4, MaxInstances: 32})
	if err != nil {
		panic(err)
	}
	defer pool.Close()

	ctx := context.Background()
	for i := 0; i < 3; i++ {
		out, err := pool.Invoke(ctx, "inc") // each call: reset -> inc -> returns 2
		if err != nil {
			panic(err)
		}
		fmt.Printf("pool.Invoke #%d -> inc returned %d (isolated per lease)\n", i, wago.AsI32(out[0]))
	}

	st := pool.Stats()
	fmt.Printf("pool stats: live=%d idle=%d created=%d reused=%d\n", st.Live, st.Idle, st.Created, st.Reused)
}
