//go:build linux && (amd64 || arm64) && !tinygo && !wago_target_tinygo

package wago

// Independent RR3 retirement re-review probe (rr3-retirement-recheck).
// Archived probe: copied into an immutable source export to execute. It is
// self-contained so it builds at 5847df30f, 9ad40bce4 and d2464100f.

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	"github.com/wago-org/wago/tests/support/wasmtest"
)

func rr3xCompile(t *testing.T, cfg *RuntimeConfig, raw []byte) *Compiled {
	t.Helper()
	c, err := Compile(cfg, raw)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

// P: storage-only provider. Exported externref table t[2]; nullput1 stores null
// into t[1] and returns 7. It never touches t[0].
func rr3xProvider(t *testing.T, store *referenceStore) *Instance {
	t.Helper()
	c := rr3xCompile(t, NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x60, 0, 1, 0x7f})),
		wasmtest.Section(3, wasmtest.Vec([]byte{0})),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x6f, 0, 2})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("t", 1, 0), wasmtest.ExportEntry("nullput1", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 1, 0xd0, 0x6f, 0x26, 0, 0x41, 7, 0x0b})))))
	in, err := instantiateCore(c, InstantiateOptions{store: store})
	if err != nil {
		t.Fatal(err)
	}
	return in
}

// Q: GC conversion publisher importing P's table; pub converts a fresh struct
// into t[0]. Instantiation binds its conversion core (a topology writer).
func rr3xPublisher(t *testing.T, store *referenceStore, table *Table) (*Instance, error) {
	imp := append(append(wasmtest.Name("p"), wasmtest.Name("t")...), 1, 0x6f, 0, 2)
	c := rr3xCompile(t, NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x5f, 1, 0x7f, 1}, []byte{0x60, 0, 0})),
		wasmtest.Section(2, wasmtest.Vec(imp)),
		wasmtest.Section(3, wasmtest.Vec([]byte{1})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("pub", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 0, 0x41, 9, 0xfb, 0, 0, 0xfb, 0x1b, 0x26, 0, 0x0b})))))
	return instantiateCore(c, InstantiateOptions{store: store, Imports: testImports("p.t", table), GC: GCConfig{NurseryBytes: 4096, SurvivorBytes: 4096}})
}

// Q2: unrelated GC conversion publisher with its own externref table; it
// imports nothing from P or R. pub converts a fresh struct into its own t[0].
func rr3xUnrelatedPublisher(t *testing.T, store *referenceStore) (*Instance, error) {
	c := rr3xCompile(t, NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x5f, 1, 0x7f, 1}, []byte{0x60, 0, 0})),
		wasmtest.Section(3, wasmtest.Vec([]byte{1})),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x6f, 0, 2})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("pub", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 0, 0x41, 9, 0xfb, 0, 0, 0xfb, 0x1b, 0x26, 0, 0x0b})))))
	return instantiateCore(c, InstantiateOptions{store: store, GC: GCConfig{NurseryBytes: 4096, SurvivorBytes: 4096}})
}

// R: relay root. Stores 1 at shared [4], spins until [0] != 0, then makes an
// ordinary direct-import call to P.nullput1 and returns its result (7).
func rr3xRelay(t *testing.T, store *referenceStore, memory *Memory, nullput1 any) *Instance {
	t.Helper()
	c := rr3xCompile(t, NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3|CoreFeatureThreads).WithBoundsChecks(BoundsChecksExplicit), wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x60, 0, 1, 0x7f})),
		wasmtest.Section(2, wasmtest.Vec(
			append(append(wasmtest.Name("env"), wasmtest.Name("memory")...), 2, 3, 1, 1),
			append(append(wasmtest.Name("p"), wasmtest.Name("nullput1")...), 0, 0))),
		wasmtest.Section(3, wasmtest.Vec([]byte{0})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 4, 0x41, 1, 0xfe, 0x17, 2, 0,
			0x03, 0x40, 0x41, 0, 0xfe, 0x10, 2, 0, 0x45, 0x0d, 0, 0x0b, 0x10, 0, 0x0b})))))
	in, err := instantiateCore(c, InstantiateOptions{store: store, Imports: testImports("env.memory", memory, "p.nullput1", nullput1)})
	if err != nil {
		t.Fatal(err)
	}
	return in
}

func rr3xEntryFlags(in *Instance) (dynamic, conversionOnly bool) {
	dynamic = in.executionFlags.Load()&executionFlagDynamicGCDomain != 0
	in.refStore.mu.Lock()
	defer in.refStore.mu.Unlock()
	if entry := in.refStore.instances[in]; entry != nil {
		conversionOnly = entry.conversionOnlyInvocationDomains
	}
	return
}

// A valid nested direct-import call from a conversion-only root into a
// conversion-only provider must succeed whether or not an unrelated GC
// conversion publisher is instantiated (and publishes) while the root runs.
// The nested callee touches only t[1]; nothing from the late domain.
func TestRR3XNestedCallAfterLatePublisher(t *testing.T) {
	for _, mode := range []string{"none", "before", "during", "during-pub", "unrelated-during", "unrelated-during-pub"} {
		t.Run(mode, func(t *testing.T) {
			rt := NewRuntime()
			p := rr3xProvider(t, rt.refStore)
			defer p.Close()
			table, err := p.ExportedTable("t")
			if err != nil {
				t.Fatal(err)
			}
			var q *Instance
			if mode == "before" {
				if q, err = rr3xPublisher(t, rt.refStore, table); err != nil {
					t.Fatal(err)
				}
			}
			memory, err := NewSharedMemory(1, 1)
			if err != nil {
				t.Fatal(err)
			}
			defer memory.Close()
			nullput1, err := p.ExportedFunc("nullput1")
			if err != nil {
				t.Fatal(err)
			}
			r := rr3xRelay(t, rt.refStore, memory, nullput1)
			defer r.Close()
			mem := memory.UnsafeBytes()
			flag := (*uint32)(unsafe.Pointer(&mem[0]))
			entered := (*uint32)(unsafe.Pointer(&mem[4]))
			type result struct {
				out []uint64
				err error
			}
			rdone := make(chan result, 1)
			ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
			defer cancel()
			go func() { out, e := r.InvokeContext(ctx, "run"); rdone <- result{append([]uint64(nil), out...), e} }()
			deadline := time.Now().Add(3 * time.Second)
			for atomic.LoadUint32(entered) == 0 {
				if time.Now().After(deadline) {
					atomic.StoreUint32(flag, 1)
					t.Fatal("relay never entered")
				}
				time.Sleep(time.Millisecond)
			}
			qBlocked := false
			var qdone chan error
			if mode != "none" && mode != "before" {
				qdone = make(chan error, 1)
				go func() {
					var in *Instance
					var e error
					if mode == "during" || mode == "during-pub" {
						in, e = rr3xPublisher(t, rt.refStore, table)
					} else {
						in, e = rr3xUnrelatedPublisher(t, rt.refStore)
					}
					if e == nil {
						q = in
						if mode == "during-pub" || mode == "unrelated-during-pub" {
							_, e = in.Invoke("pub")
						}
					}
					qdone <- e
				}()
				select {
				case e := <-qdone:
					if e != nil {
						atomic.StoreUint32(flag, 1)
						t.Fatalf("late publisher: %v", e)
					}
				case <-time.After(2 * time.Second):
					qBlocked = true
				}
			}
			atomic.StoreUint32(flag, 1)
			res := <-rdone
			if qBlocked {
				if e := <-qdone; e != nil {
					t.Fatalf("late publisher after root: %v", e)
				}
			}
			if q != nil {
				defer q.Close()
			}
			rd, rc := rr3xEntryFlags(r)
			pd, pc := rr3xEntryFlags(p)
			t.Logf("mode=%s root(dynamic=%t conversionOnly=%t) provider(dynamic=%t conversionOnly=%t) publisherWaitedForRoot=%t out=%v err=%v", mode, rd, rc, pd, pc, qBlocked, res.out, res.err)
			if res.err != nil || len(res.out) != 1 || res.out[0] != 7 {
				t.Errorf("valid nested call failed: out=%v err=%v", res.out, res.err)
			}
		})
	}
}
