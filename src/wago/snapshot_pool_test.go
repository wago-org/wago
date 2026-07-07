//go:build linux && amd64 && !tinygo && !wago_guardpage

// Snapshots are explicit-bounds only (Capture rejects signals-based modules), so
// this suite is excluded from the guard-page build, where the default config is
// signals-based. See Snapshot's doc comment for the scope.

package wago

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
)

// counterWAT: a mutable global "counter" and a linear memory. bump(n) adds n and
// mirrors the total to memory[0]; get() returns the counter. Enough mutable state
// to exercise snapshot capture, restore, and pool reset.
const counterWAT = `(module
  (memory (export "mem") 1)
  (global $counter (export "counter") (mut i32) (i32.const 0))
  (func (export "bump") (param $n i32) (result i32)
    (global.set $counter (i32.add (global.get $counter) (local.get $n)))
    (i32.store (i32.const 0) (global.get $counter))
    (global.get $counter))
  (func (export "get") (result i32) (global.get $counter)))`

func compileCounter(t *testing.T) *Compiled {
	t.Helper()
	c, err := Compile(nil, watToWasmCA(t, counterWAT))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	return c
}

// Capture(SnapshotInit) then Instantiate reproduces initial state (counter 0) and
// runs forward from there.
func TestCaptureInitAndInstantiate(t *testing.T) {
	c := compileCounter(t)
	snap, err := Capture(c, SnapshotOptions{Kind: SnapshotInit})
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	in, err := Instantiate(snap, InstantiateOptions{})
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer in.Close()
	if g, _ := in.Global("counter"); g != 0 {
		t.Fatalf("counter = %d, want 0", g)
	}
	if out, _ := in.Invoke("bump", 7); out[0] != 7 {
		t.Fatalf("bump = %d, want 7", out[0])
	}
}

// A warm snapshot runs the warm function before capturing, so restored instances
// start from post-warm state. Here "bump" is the warm func with arg 100.
func TestCaptureWarmExplicitFunc(t *testing.T) {
	c := compileCounter(t)
	snap, err := Capture(c, SnapshotOptions{Kind: SnapshotWarm, WarmFunc: "bump", WarmArgs: []uint64{100}})
	if err != nil {
		t.Fatalf("capture warm: %v", err)
	}
	in, err := Instantiate(snap, InstantiateOptions{})
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer in.Close()
	if g, _ := in.Global("counter"); g != 100 {
		t.Fatalf("warm counter = %d, want 100", g)
	}
	if got := in.Memory().Bytes()[0]; got != 100 {
		t.Fatalf("warm memory[0] = %d, want 100", got)
	}
}

// Default warm-func resolution: no WarmFunc set, module exports _start.
func TestCaptureWarmDefaultStart(t *testing.T) {
	wat := `(module
      (global $g (export "counter") (mut i32) (i32.const 0))
      (func (export "_start") (global.set $g (i32.const 55)))
      (func (export "get") (result i32) (global.get $g)))`
	c, err := Compile(nil, watToWasmCA(t, wat))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	snap, err := Capture(c, SnapshotOptions{Kind: SnapshotWarm}) // resolves _start
	if err != nil {
		t.Fatalf("capture warm: %v", err)
	}
	in, err := Instantiate(snap, InstantiateOptions{})
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer in.Close()
	if g, _ := in.Global("counter"); g != 55 {
		t.Fatalf("counter = %d, want 55 (_start ran once at capture)", g)
	}
}

// Blob round-trip through a file, plus IsSnapshot detection.
func TestSnapshotBlobFile(t *testing.T) {
	c := compileCounter(t)
	snap, err := Capture(c, SnapshotOptions{Kind: SnapshotWarm, WarmFunc: "bump", WarmArgs: []uint64{9}})
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	blob, err := snap.MarshalBinary()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !IsSnapshot(blob) || IsCompiled(blob) {
		t.Fatalf("IsSnapshot=%v IsCompiled=%v, want true/false", IsSnapshot(blob), IsCompiled(blob))
	}
	path := filepath.Join(t.TempDir(), "s.wgsnap")
	if err := snap.WriteFile(path); err != nil {
		t.Fatalf("write: %v", err)
	}
	loaded, err := ReadSnapshotFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	in, err := Instantiate(loaded, InstantiateOptions{})
	if err != nil {
		t.Fatalf("instantiate loaded: %v", err)
	}
	defer in.Close()
	if g, _ := in.Global("counter"); g != 9 {
		t.Fatalf("loaded counter = %d, want 9", g)
	}
}

// Instantiate still dispatches correctly on a *Compiled with imports/GC options.
func TestInstantiateCompiledDispatch(t *testing.T) {
	c := compileCounter(t)
	in, err := Instantiate(c, InstantiateOptions{})
	if err != nil {
		t.Fatalf("instantiate compiled: %v", err)
	}
	defer in.Close()
	if out, _ := in.Invoke("bump", 3); out[0] != 3 {
		t.Fatalf("bump = %d, want 3", out[0])
	}
}

// Pool.Invoke must isolate state between calls: each call starts from the
// snapshot, so two bump(5) calls both return 5 (no accumulation).
func TestPoolInvokeIsolation(t *testing.T) {
	c := compileCounter(t)
	snap, err := Capture(c, SnapshotOptions{})
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	pool, err := Pool(snap, SnapshotPoolOptions{MinIdle: 2, MaxInstances: 4})
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	defer pool.Close()
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		out, err := pool.Invoke(ctx, "bump", 5)
		if err != nil {
			t.Fatalf("invoke %d: %v", i, err)
		}
		if out[0] != 5 {
			t.Fatalf("invoke %d bump = %d, want 5 (state must reset each lease)", i, out[0])
		}
	}
	if st := pool.Stats(); st.Reused == 0 {
		t.Fatalf("expected some idle reuse, Stats=%+v", st)
	}
}

// Manual lease: mutate through the lease, release (which resets), re-acquire and
// confirm the next tenant sees clean state.
func TestPoolLeaseResetsBetweenTenants(t *testing.T) {
	c := compileCounter(t)
	snap, err := Capture(c, SnapshotOptions{})
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	pool, err := Pool(snap, SnapshotPoolOptions{MinIdle: 1, MaxInstances: 1}) // force reuse of the one instance
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	defer pool.Close()
	ctx := context.Background()

	l1, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire1: %v", err)
	}
	if _, err := l1.Instance.Invoke("bump", 40); err != nil {
		t.Fatalf("bump: %v", err)
	}
	if _, err := l1.Instance.Invoke("bump", 2); err != nil { // counter now 42
		t.Fatalf("bump: %v", err)
	}
	l1.Release() // resets to snapshot

	l2, err := pool.Acquire(ctx) // same underlying instance (MaxInstances=1)
	if err != nil {
		t.Fatalf("acquire2: %v", err)
	}
	defer l2.Release()
	if g, _ := l2.Instance.Global("counter"); g != 0 {
		t.Fatalf("reused instance counter = %d, want 0 (reset on release)", g)
	}
}

// MaxInstances caps concurrent leases; an Acquire past the cap blocks until a
// release, and honors context cancellation.
func TestPoolMaxInstancesBlocksAndCancels(t *testing.T) {
	c := compileCounter(t)
	snap, err := Capture(c, SnapshotOptions{})
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	pool, err := Pool(snap, SnapshotPoolOptions{MaxInstances: 1})
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	defer pool.Close()

	l1, err := pool.Acquire(context.Background())
	if err != nil {
		t.Fatalf("acquire1: %v", err)
	}
	// Second acquire must block (cap reached); ctx cancellation unblocks it.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := pool.Acquire(ctx); err == nil {
		t.Fatalf("acquire2 should have failed on cancelled ctx")
	}
	if st := pool.Stats(); st.Live != 1 || st.InUse != 1 {
		t.Fatalf("Stats=%+v, want Live=1 InUse=1", st)
	}
	l1.Release()
	// Now capacity is free again.
	l2, err := pool.Acquire(context.Background())
	if err != nil {
		t.Fatalf("acquire3: %v", err)
	}
	l2.Release()
}

// Discard tears the instance down instead of reusing it; Stats.Discarded reflects it.
func TestPoolDiscard(t *testing.T) {
	c := compileCounter(t)
	snap, err := Capture(c, SnapshotOptions{})
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	pool, err := Pool(snap, SnapshotPoolOptions{MaxInstances: 2})
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	defer pool.Close()
	l, err := pool.Acquire(context.Background())
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	l.Discard()
	if st := pool.Stats(); st.Discarded != 1 || st.Live != 0 {
		t.Fatalf("Stats=%+v, want Discarded=1 Live=0", st)
	}
	// Double Release/Discard is a no-op.
	l.Release()
	l.Discard()
	if st := pool.Stats(); st.Discarded != 1 {
		t.Fatalf("Stats=%+v, double-return should not change counters", st)
	}
}

// Concurrent pool usage — run under `go test -race` to catch data races in the
// acquire/release/reset paths.
func TestPoolConcurrent(t *testing.T) {
	c := compileCounter(t)
	snap, err := Capture(c, SnapshotOptions{})
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	pool, err := Pool(snap, SnapshotPoolOptions{MinIdle: 4, MaxIdle: 8, MaxInstances: 16})
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	defer pool.Close()

	var wg sync.WaitGroup
	ctx := context.Background()
	for g := 0; g < 32; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 20; i++ {
				out, err := pool.Invoke(ctx, "bump", 1)
				if err != nil {
					t.Errorf("invoke: %v", err)
					return
				}
				if out[0] != 1 { // isolation: always starts from snapshot
					t.Errorf("bump = %d, want 1", out[0])
					return
				}
			}
		}()
	}
	wg.Wait()
	if st := pool.Stats(); st.Live > 16 {
		t.Fatalf("Live=%d exceeded MaxInstances=16", st.Live)
	}
}
