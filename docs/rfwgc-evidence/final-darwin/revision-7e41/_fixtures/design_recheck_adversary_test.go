//go:build darwin && arm64 && !tinygo && !wago_target_tinygo

package wago

// Independent design re-review adversaries (final-review-design-recheck).
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

func recheckCfg() *RuntimeConfig { return NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3) }

// Storage-only provider P: exported externref table t[2] and scalar mutators.
func recheckProvider(t *testing.T, store *referenceStore) *Instance {
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
	c, err := Compile(recheckCfg(), raw)
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
func recheckPublisher(t *testing.T, store *referenceStore, table *Table) *Instance {
	t.Helper()
	imp := append(append(wasmtest.Name("p"), wasmtest.Name("t")...), 1, 0x6f, 0, 2)
	raw := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x5f, 1, 0x7f, 1}, []byte{0x60, 0, 0})),
		wasmtest.Section(2, wasmtest.Vec(imp)),
		wasmtest.Section(3, wasmtest.Vec([]byte{1})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("pub", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 0, 0x41, 9, 0xfb, 0, 0, 0xfb, 0x1b, 0x26, 0, 0x0b}))))
	c, err := Compile(recheckCfg(), raw)
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

func recheckReceipt(store *referenceStore, desc []byte, index uint64) *gcConversionReceipt {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.gcDomains == nil {
		return nil
	}
	c := store.gcDomains.conversionContainers[&desc[0]]
	if c == nil {
		return nil
	}
	return c.slots[index]
}

func recheckLiveReceipt(t *testing.T, r *gcConversionReceipt, word uint64, desc []byte) {
	t.Helper()
	if r == nil || r.released {
		t.Fatal("STOP receipt missing or released")
	}
	if ref, err := r.identity.domain.collector.CheckedTableSlot(r.root); err != nil || ref != r.identity.ref {
		t.Fatalf("STOP receipt root lost: %v", err)
	}
	if got := binary.LittleEndian.Uint64(desc[8:]); got != word {
		t.Fatalf("STOP storage %#x != receipt word %#x", got, word)
	}
}

// A cross-Runtime scalar relay admitted before any conversion publisher exists
// must not later clear or duplicate a converted slot without receipt authority.
// Ordinary null replacement through the same relay must remain available.
func TestDesignRecheckForeignRelayAfterLatePublisher(t *testing.T) {
	rt1, rt2 := NewRuntime(), NewRuntime()
	p := recheckProvider(t, rt1.refStore)
	defer p.Close()
	imports := []any{}
	for _, name := range []string{"clear0", "copy01", "nullput1"} {
		f, err := p.ExportedFunc(name)
		if err != nil {
			t.Fatal(err)
		}
		imports = append(imports, "p."+name, f)
	}
	ty := []byte{0x60, 0, 1, 0x7f}
	var imps [][]byte
	for _, name := range []string{"clear0", "copy01", "nullput1"} {
		imps = append(imps, append(append(wasmtest.Name("p"), wasmtest.Name(name)...), 0, 0))
	}
	raw := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(ty)),
		wasmtest.Section(2, wasmtest.Vec(imps...)),
		wasmtest.Section(3, wasmtest.Vec([]byte{0}, []byte{0}, []byte{0})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("clear0", 0, 3), wasmtest.ExportEntry("copy01", 0, 4), wasmtest.ExportEntry("nullput1", 0, 5))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x10, 0, 0x0b}), wasmtest.Code([]byte{0x10, 1, 0x0b}), wasmtest.Code([]byte{0x10, 2, 0x0b}))))
	cc, err := Compile(recheckCfg(), raw)
	if err != nil {
		t.Fatal(err)
	}
	defer cc.Close()
	c, err := instantiateCore(cc, InstantiateOptions{store: rt2.refStore, Imports: testImports(imports...)})
	if err != nil {
		t.Fatalf("foreign storage-only scalar import rejected before publication: %v", err)
	}
	defer c.Close()

	table, err := p.ExportedTable("t")
	if err != nil {
		t.Fatal(err)
	}
	q := recheckPublisher(t, rt1.refStore, table)
	if _, err = q.Invoke("pub"); err != nil {
		t.Fatalf("publisher: %v", err)
	}
	word := binary.LittleEndian.Uint64(table.desc[8:])
	r := recheckReceipt(rt1.refStore, table.desc, 0)
	recheckLiveReceipt(t, r, word, table.desc)

	if out, err := c.Invoke("nullput1"); err != nil || len(out) != 1 || out[0] != 7 {
		t.Fatalf("ordinary foreign relay null store: %v %v", out, err)
	}
	clearOut, clearErr := c.Invoke("clear0")
	t.Logf("foreign clear of converted slot: out=%v err=%v", clearOut, clearErr)
	if clearErr == nil {
		if binary.LittleEndian.Uint64(table.desc[8:]) != 0 || !r.released {
			t.Fatal("STOP foreign clear accepted without releasing receipt")
		}
	} else {
		recheckLiveReceipt(t, recheckReceipt(rt1.refStore, table.desc, 0), word, table.desc)
	}
	copyOut, copyErr := c.Invoke("copy01")
	t.Logf("foreign copy of converted slot: out=%v err=%v", copyOut, copyErr)
	if copyErr == nil && clearErr != nil {
		r1 := recheckReceipt(rt1.refStore, table.desc, 1)
		if binary.LittleEndian.Uint64(table.desc[16:]) == word && (r1 == nil || r1.released) {
			t.Fatal("STOP foreign copy duplicated converted word without receipt")
		}
	}
	if copyErr != nil && binary.LittleEndian.Uint64(table.desc[16:]) != 0 {
		t.Fatal("STOP rejected copy changed destination")
	}
	// The relay and provider remain usable after rejection.
	if out, err := c.Invoke("nullput1"); err != nil || len(out) != 1 || out[0] != 7 {
		t.Fatalf("relay unusable after rejection: %v %v", out, err)
	}
	if clearErr != nil {
		// Publisher retirement while the receipt remains: authority survives.
		if err = q.Close(); err != nil {
			t.Fatal(err)
		}
		recheckLiveReceipt(t, recheckReceipt(rt1.refStore, table.desc, 0), word, table.desc)
		if out, err := p.Invoke("nonnull0"); err != nil || out[0] != 1 {
			t.Fatalf("receipt slot unreadable after retirement: %v %v", out, err)
		}
		// Same-Runtime owner clears it with receipt authority.
		if out, err := p.Invoke("clear0"); err != nil || out[0] != 7 {
			t.Fatalf("same-Runtime clear after retirement: %v %v", out, err)
		}
		if binary.LittleEndian.Uint64(table.desc[8:]) != 0 || !r.released {
			t.Fatal("STOP same-Runtime clear did not release receipt")
		}
	} else {
		_ = q.Close()
	}
}

// Topology writer liveness: a collector-free module whose only GC relation is
// externref storage spins in guest code. Instantiating an unrelated GC module
// in the same Runtime must not wait for that call (DR-1 consequence 2).
func TestDesignRecheckStorageOnlyCallTopologyWriter(t *testing.T) {
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
			uc, err := Compile(recheckCfg(), wasmtest.Module(
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

// A host callback inside a conversion-only invocation binds a new publisher and
// retires an existing one. The resumed lease and its release must match the
// changed authority exactly: no leaked gate and no unleased publication.
func TestDesignRecheckCallbackChangesPublisherAuthority(t *testing.T) {
	rt := NewRuntime()
	p := recheckProvider(t, rt.refStore)
	defer p.Close()
	table, err := p.ExportedTable("t")
	if err != nil {
		t.Fatal(err)
	}
	q1 := recheckPublisher(t, rt.refStore, table)
	if _, err = q1.Invoke("pub"); err != nil {
		t.Fatal(err)
	}
	q1Domain := q1.gcInvocationDomain()
	var q2 *Instance
	var cbErr error
	host := slotHostFunc(func(_ HostModule, _, out []uint64) {
		out[0] = 7
		if cbErr = q1.Close(); cbErr != nil {
			return
		}
		ch := make(chan error, 1)
		go func() {
			var e error
			q2c, e := Compile(recheckCfg(), wasmtest.Module(
				wasmtest.Section(1, wasmtest.Vec([]byte{0x5f, 1, 0x7f, 1}, []byte{0x60, 0, 0})),
				wasmtest.Section(2, wasmtest.Vec(append(append(wasmtest.Name("p"), wasmtest.Name("t")...), 1, 0x6f, 0, 2))),
				wasmtest.Section(3, wasmtest.Vec([]byte{1})),
				wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("pub", 0, 0))),
				wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 1, 0x41, 9, 0xfb, 0, 0, 0xfb, 0x1b, 0x26, 0, 0x0b})))))
			if e == nil {
				q2, e = instantiateCore(q2c, InstantiateOptions{store: rt.refStore, Imports: testImports("p.t", table), GC: GCConfig{NurseryBytes: 4096}})
			}
			if e == nil {
				_, e = q2.Invoke("pub")
			}
			ch <- e
		}()
		select {
		case cbErr = <-ch:
		case <-time.After(3 * time.Second):
			cbErr = context.DeadlineExceeded
		}
	})
	// Relay R (storage-only, same Runtime) imports h and p.clear0: callback, then
	// clears the slot whose receipt outlived q1.
	raw := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x60, 0, 1, 0x7f})),
		wasmtest.Section(2, wasmtest.Vec(append(append(wasmtest.Name("h"), wasmtest.Name("f")...), 0, 0), append(append(wasmtest.Name("p"), wasmtest.Name("clear0")...), 0, 0))),
		wasmtest.Section(3, wasmtest.Vec([]byte{0})),
		wasmtest.Section(6, wasmtest.Vec([]byte{0x6f, 1, 0xd0, 0x6f, 0x0b})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 2))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x10, 0, 0x1a, 0x10, 1, 0x0b}))))
	rc, err := Compile(recheckCfg(), raw)
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	clear0, _ := p.ExportedFunc("clear0")
	r, err := instantiateCore(rc, InstantiateOptions{store: rt.refStore, Imports: testImports("h.f", host, "p.clear0", clear0)})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	word0 := binary.LittleEndian.Uint64(table.desc[8:])
	r0 := recheckReceipt(rt.refStore, table.desc, 0)
	recheckLiveReceipt(t, r0, word0, table.desc)
	done := make(chan error, 1)
	go func() {
		out, e := r.Invoke("run")
		if e == nil && (len(out) != 1 || out[0] != 7) {
			e = context.Canceled
		}
		done <- e
	}()
	select {
	case err = <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("STOP conversion-only invocation deadlocked across callback authority change")
	}
	if cbErr != nil {
		t.Fatalf("callback: %v", cbErr)
	}
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if binary.LittleEndian.Uint64(table.desc[8:]) != 0 || !r0.released {
		t.Fatal("STOP retired-publisher receipt not cleared under resumed authority")
	}
	word1 := binary.LittleEndian.Uint64(table.desc[16:])
	recheckLiveReceipt(t, recheckReceipt(rt.refStore, table.desc, 1), word1, table.desc[8:])
	// No leaked gates: every remaining domain is leasable now.
	for _, d := range []*Instance{q2, r, p} {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		lease, lerr := d.lockGCInvocationContext(ctx, newInvocationID())
		cancel()
		if lerr != nil {
			t.Fatalf("STOP leaked invocation gate: %v", lerr)
		}
		lease.unlock()
	}
	rt.refStore.mu.Lock()
	linked := false
	if rt.refStore.gcDomains != nil {
		for d := rt.refStore.gcDomains.first; d != nil; d = d.next {
			linked = linked || d == q1Domain
		}
	}
	rt.refStore.mu.Unlock()
	if linked {
		t.Fatal("retired publisher domain still linked after final claim drain")
	}
	q2.Close()
}

// Same-Runtime nested scalar call: relay -> P.clear0 clears a converted slot
// whose receipt authority the relay's root lease already holds. Valid Wasm; the
// write must either succeed with receipt release or (if rejected) leave state intact.
func TestDesignRecheckNestedSameRuntimeClear(t *testing.T) {
	for _, relayKind := range []string{"direct", "scalar-relay", "storage-relay"} {
		t.Run(relayKind, func(t *testing.T) {
			rt := NewRuntime()
			p := recheckProvider(t, rt.refStore)
			defer p.Close()
			table, err := p.ExportedTable("t")
			if err != nil {
				t.Fatal(err)
			}
			q := recheckPublisher(t, rt.refStore, table)
			defer q.Close()
			if _, err = q.Invoke("pub"); err != nil {
				t.Fatal(err)
			}
			word := binary.LittleEndian.Uint64(table.desc[8:])
			r0 := recheckReceipt(rt.refStore, table.desc, 0)
			recheckLiveReceipt(t, r0, word, table.desc)
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
				rc, err := Compile(recheckCfg(), wasmtest.Module(sections...))
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
				recheckLiveReceipt(t, recheckReceipt(rt.refStore, table.desc, 0), word, table.desc)
				t.Errorf("valid same-Runtime nested clear rejected: %v", err)
				return
			}
			if binary.LittleEndian.Uint64(table.desc[8:]) != 0 || !r0.released {
				t.Fatal("STOP clear accepted without receipt release")
			}
		})
	}
}

// Same-Runtime nested GC publication: a scalar relay calls Q.pub, which converts
// and stores into P's table. The relay root leases Q's domain transitively.
func TestDesignRecheckNestedPublish(t *testing.T) {
	rt := NewRuntime()
	p := recheckProvider(t, rt.refStore)
	defer p.Close()
	table, err := p.ExportedTable("t")
	if err != nil {
		t.Fatal(err)
	}
	q := recheckPublisher(t, rt.refStore, table)
	defer q.Close()
	pub, _ := q.ExportedFunc("pub")
	rc, err := Compile(recheckCfg(), wasmtest.Module(
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
