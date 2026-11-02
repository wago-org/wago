//go:build linux && (amd64 || arm64) && !tinygo && !wago_target_tinygo

package wago

// Baseline-portable controls for the design re-review adversaries (final-review-design-recheck).
// Archived probe: copied into an immutable source export to execute.

import (
	"context"
	"encoding/binary"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	"github.com/wago-org/wago/tests/support/wasmtest"
)

func recheckBaseCfg() *RuntimeConfig { return NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3) }

// Storage-only provider P: exported externref table t[2] and scalar mutators.
func recheckBaseProvider(t *testing.T, store *referenceStore) *Instance {
	t.Helper()
	raw := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x60, 0, 1, 0x7f})),
		wasmtest.Section(3, wasmtest.Vec([]byte{0}, []byte{0}, []byte{0}, []byte{0})),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x6f, 0, 2})),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("t", 1, 0),
			wasmtest.ExportEntry("clear0", 0, 0),
			wasmtest.ExportEntry("copy01", 0, 1),
			wasmtest.ExportEntry("nullput1", 0, 2),
			wasmtest.ExportEntry("nonnull0", 0, 3))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x41, 0, 0xd0, 0x6f, 0x26, 0, 0x41, 7, 0x0b}),
			wasmtest.Code([]byte{0x41, 1, 0x41, 0, 0x41, 1, 0xfc, 14, 0, 0, 0x41, 7, 0x0b}),
			wasmtest.Code([]byte{0x41, 1, 0xd0, 0x6f, 0x26, 0, 0x41, 7, 0x0b}),
			wasmtest.Code([]byte{0x41, 0, 0x25, 0, 0xd1, 0x45, 0x0b}))))
	c, err := Compile(recheckBaseCfg(), raw)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	in, err := instantiateCore(c, InstantiateOptions{store: store})
	if err != nil {
		t.Fatal(err)
	}
	return in
}

// GC publisher Q importing P's table; pub converts a fresh struct into t[0].
func recheckBasePublisher(t *testing.T, store *referenceStore, table *Table) *Instance {
	t.Helper()
	imp := append(append(wasmtest.Name("p"), wasmtest.Name("t")...), 1, 0x6f, 0, 2)
	raw := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x5f, 1, 0x7f, 1}, []byte{0x60, 0, 0})),
		wasmtest.Section(2, wasmtest.Vec(imp)),
		wasmtest.Section(3, wasmtest.Vec([]byte{1})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("pub", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 0, 0x41, 9, 0xfb, 0, 0, 0xfb, 0x1b, 0x26, 0, 0x0b}))))
	c, err := Compile(recheckBaseCfg(), raw)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	in, err := instantiateCore(c, InstantiateOptions{store: store, Imports: testImports("p.t", table), GC: GCConfig{NurseryBytes: 4096, SurvivorBytes: 4096, VerifyAfterCollect: true}})
	if err != nil {
		t.Fatal(err)
	}
	return in
}

// Topology writer liveness: a collector-free module whose only GC relation is
// externref storage spins in guest code. Instantiating an unrelated GC module
// in the same Runtime must not wait for that call (DR-1 consequence 2).
func TestDesignRecheckBaseStorageOnlyCallTopologyWriter(t *testing.T) {
	for _, storage := range []string{"none", "global", "table"} {
		t.Run(storage, func(t *testing.T) {
			memory, err := NewSharedMemory(1, 1)
			if err != nil {
				t.Fatal(err)
			}
			defer memory.Close()
			rt := NewRuntime()
			imp := append(append(wasmtest.Name("env"), wasmtest.Name("memory")...), 2, 3, 1, 1)
			sections := [][]byte{
				wasmtest.Section(1, wasmtest.Vec([]byte{0x60, 0, 1, 0x7f})),
				wasmtest.Section(2, wasmtest.Vec(imp)),
				wasmtest.Section(3, wasmtest.Vec([]byte{0})),
			}
			if storage == "table" {
				sections = append(sections, wasmtest.Section(4, wasmtest.Vec([]byte{0x6f, 0, 1})))
			}
			if storage == "global" {
				sections = append(sections, wasmtest.Section(6, wasmtest.Vec([]byte{0x6f, 1, 0xd0, 0x6f, 0x0b})))
			}
			// store 1 at [4]; loop while atomic.load [0] == 0; return 7
			body := []byte{0x41, 4, 0x41, 1, 0xfe, 0x17, 2, 0,
				0x03, 0x40, 0x41, 0, 0xfe, 0x10, 2, 0, 0x45, 0x0d, 0, 0x0b, 0x41, 7, 0x0b}
			sections = append(sections, wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("spin", 0, 0))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))))
			sc, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3|CoreFeatureThreads).WithBoundsChecks(BoundsChecksExplicit), wasmtest.Module(sections...))
			if err != nil {
				t.Fatal(err)
			}
			defer sc.Close()
			s, err := instantiateCore(sc, InstantiateOptions{store: rt.refStore, Imports: testImports("env.memory", memory)})
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			// An existing GC tenant so the Runtime has topology.
			uc, err := Compile(recheckBaseCfg(), wasmtest.Module(
				wasmtest.Section(1, wasmtest.Vec([]byte{0x5f, 1, 0x7f, 1}, []byte{0x60, 0, 0})),
				wasmtest.Section(3, wasmtest.Vec([]byte{1})),
				wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("a", 0, 0))),
				wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 1, 0xfb, 0, 0, 0x1a, 0x0b})))))
			if err != nil {
				t.Fatal(err)
			}
			defer uc.Close()
			u1, err := instantiateCore(uc, InstantiateOptions{store: rt.refStore, GC: GCConfig{NurseryBytes: 8192}})
			if err != nil {
				t.Fatal(err)
			}
			defer u1.Close()
			mem := memory.UnsafeBytes()
			flag := (*uint32)(unsafe.Pointer(&mem[0]))
			entered := (*uint32)(unsafe.Pointer(&mem[4]))
			done := make(chan error, 1)
			go func() { _, e := s.Invoke("spin"); done <- e }()
			deadline := time.Now().Add(5 * time.Second)
			for atomic.LoadUint32(entered) == 0 {
				if time.Now().After(deadline) {
					atomic.StoreUint32(flag, 1)
					t.Fatal("spin never entered")
				}
				time.Sleep(time.Millisecond)
			}
			type result struct {
				in  *Instance
				err error
			}
			inst := make(chan result, 1)
			go func() {
				in, e := instantiateCore(uc, InstantiateOptions{store: rt.refStore, GC: GCConfig{NurseryBytes: 8192}})
				inst <- result{in, e}
			}()
			var blocked bool
			var r result
			select {
			case r = <-inst:
			case <-time.After(2 * time.Second):
				blocked = true
			}
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			lease, lerr := u1.lockGCInvocationContext(ctx, newInvocationID())
			cancel()
			if lerr == nil {
				lease.unlock()
			}
			atomic.StoreUint32(flag, 1)
			if err := <-done; err != nil {
				t.Fatalf("spin: %v", err)
			}
			if blocked {
				r = <-inst
			}
			if r.err != nil {
				t.Fatal(r.err)
			}
			r.in.Close()
			t.Logf("storage=%s dynamic=%t instantiateBlocked=%t unrelatedLeaseErr=%v", storage, s.executionFlags.Load()&executionFlagDynamicGCDomain != 0, blocked, lerr)
			if lerr != nil {
				t.Errorf("unrelated GC lease blocked by storage-only call: %v", lerr)
			}
			if blocked {
				t.Errorf("GC instantiation in the same Runtime waited for a storage-only guest call")
			}
		})
	}
}

// Same-Runtime nested scalar call: relay -> P.clear0 clears a converted slot
// whose receipt authority the relay's root lease already holds. Valid Wasm; the
// write must either succeed with receipt release or (if rejected) leave state intact.
func TestDesignRecheckBaseNestedSameRuntimeClear(t *testing.T) {
	for _, relayKind := range []string{"direct", "scalar-relay", "storage-relay"} {
		t.Run(relayKind, func(t *testing.T) {
			rt := NewRuntime()
			p := recheckBaseProvider(t, rt.refStore)
			defer p.Close()
			table, err := p.ExportedTable("t")
			if err != nil {
				t.Fatal(err)
			}
			q := recheckBasePublisher(t, rt.refStore, table)
			defer q.Close()
			if _, err = q.Invoke("pub"); err != nil {
				t.Fatal(err)
			}
			word := binary.LittleEndian.Uint64(table.desc[8:])
			if word == 0 {
				t.Fatal("publisher did not store")
			}
			caller := p
			name := "clear0"
			if relayKind != "direct" {
				sections := [][]byte{
					wasmtest.Section(1, wasmtest.Vec([]byte{0x60, 0, 1, 0x7f})),
					wasmtest.Section(2, wasmtest.Vec(append(append(wasmtest.Name("p"), wasmtest.Name("clear0")...), 0, 0))),
					wasmtest.Section(3, wasmtest.Vec([]byte{0})),
				}
				if relayKind == "storage-relay" {
					sections = append(sections, wasmtest.Section(6, wasmtest.Vec([]byte{0x6f, 1, 0xd0, 0x6f, 0x0b})))
				}
				sections = append(sections, wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x10, 0, 0x0b}))))
				rc, err := Compile(recheckBaseCfg(), wasmtest.Module(sections...))
				if err != nil {
					t.Fatal(err)
				}
				defer rc.Close()
				clear0, _ := p.ExportedFunc("clear0")
				caller, err = instantiateCore(rc, InstantiateOptions{store: rt.refStore, Imports: testImports("p.clear0", clear0)})
				if err != nil {
					t.Fatal(err)
				}
				defer caller.Close()
				name = "run"
			}
			out, err := caller.Invoke(name)
			t.Logf("%s clear: out=%v err=%v", relayKind, out, err)
			if err != nil {
				t.Errorf("valid same-Runtime nested clear rejected: %v", err)
				return
			}
			if binary.LittleEndian.Uint64(table.desc[8:]) != 0 {
				t.Fatal("clear not stored")
			}
		})
	}
}

// Same-Runtime nested GC publication: a scalar relay calls Q.pub, which converts
// and stores into P's table. The relay root leases Q's domain transitively.
func TestDesignRecheckBaseNestedPublish(t *testing.T) {
	rt := NewRuntime()
	p := recheckBaseProvider(t, rt.refStore)
	defer p.Close()
	table, err := p.ExportedTable("t")
	if err != nil {
		t.Fatal(err)
	}
	q := recheckBasePublisher(t, rt.refStore, table)
	defer q.Close()
	pub, _ := q.ExportedFunc("pub")
	rc, err := Compile(recheckBaseCfg(), wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x60, 0, 0})),
		wasmtest.Section(2, wasmtest.Vec(append(append(wasmtest.Name("q"), wasmtest.Name("pub")...), 0, 0))),
		wasmtest.Section(3, wasmtest.Vec([]byte{0})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x10, 0, 0x0b})))))
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	r, err := instantiateCore(rc, InstantiateOptions{store: rt.refStore, Imports: testImports("q.pub", pub)})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	_, err = r.Invoke("run")
	t.Logf("relay publish: err=%v word=%#x", err, binary.LittleEndian.Uint64(table.desc[8:]))
	if err != nil {
		if binary.LittleEndian.Uint64(table.desc[8:]) != 0 {
			t.Fatal("STOP rejected publication changed storage")
		}
		t.Errorf("valid same-Runtime nested publication rejected: %v", err)
	} else if binary.LittleEndian.Uint64(table.desc[8:]) == 0 {
		t.Fatal("publication not stored")
	}
}
