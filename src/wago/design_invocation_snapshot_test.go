//go:build linux && (amd64 || arm64) && !tinygo && !wago_target_tinygo

package wago

import (
	"bytes"
	"context"
	"encoding/binary"
	"testing"
	"time"

	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

// Complements the byte-identical final-review-design-recheck adversaries with
// admission/lifetime checkpoints; deadlines are liveness bounds, not timings.
func TestDesignInvocationSnapshotCancellation(t *testing.T) {
	for _, overflow := range []bool{false, true} {
		name := "partial-lock-cancel"
		if overflow {
			name = "pin-overflow"
		}
		t.Run(name, func(t *testing.T) {
			store := newReferenceStore(false)
			topology := store.ensureGCTopology()
			var set gcInvocationDomainSet
			for i := 0; i < 6; i++ {
				d := &gcStoreDomain{id: newGCDomainIdentity(), collector: new(gc.Collector), refs: 1, conversionPublishers: 1}
				set.add(d)
				topology.appendDomainLocked(d)
			}
			in := &Instance{refStore: store}
			store.instances = map[*Instance]*referenceStoreInstance{in: {dynamicInvocationDomains: true, conversionOnlyInvocationDomains: true}}
			in.executionFlags.Store(executionFlagDynamicGCDomain | executionFlagImportedGCDomain)
			if overflow {
				set.at(4).claims = ^uint32(0)
				if _, err := in.lockGCInvocationContext(context.Background(), newInvocationID()); err == nil {
					t.Fatal("overflow admitted")
				}
				for i := 0; i < set.len(); i++ {
					if i != 4 && set.at(i).claims != 0 {
						t.Fatal("partial pin leaked")
					}
				}
				if len(topology.invocations) != 0 {
					t.Fatal("failed pin published a snapshot")
				}
				return
			}
			held := gcInvocationDomainView{local: set.at(4)}
			id := newInvocationID()
			held.lock()
			held.claim(id)
			defer func() { held.release(id); held.unlock() }()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			go func() {
				lease, err := in.lockGCInvocationContext(ctx, newInvocationID())
				if err == nil {
					lease.unlock()
				}
				done <- err
			}()
			awaitGCLifecycle(t, func() bool { return set.at(4).invocationMu.state.Load()&invocationGateWaiters != 0 })
			// All pins exist, but a queued topology writer can progress before cancel.
			if !topology.TryLock() {
				cancel()
				<-done
				t.Fatal("domain wait retained topology reader")
			}
			store.mu.Lock()
			pinned := len(topology.invocations) == 1
			for i := 0; i < set.len(); i++ {
				pinned = pinned && set.at(i).claims == 1
			}
			store.mu.Unlock()
			topology.Unlock()
			cancel()
			if err := <-done; err != context.Canceled {
				t.Fatalf("cancel: %v", err)
			}
			if !pinned {
				t.Fatal("incomplete lifetime pins while waiting")
			}
			if len(topology.invocations) != 0 {
				t.Fatal("cancel retained authority")
			}
			for i := 0; i < set.len(); i++ {
				d := set.at(i)
				if d.claims != 0 {
					t.Fatal("cancel leaked pin")
				}
				if i != 4 && (d.invocationMu.state.Load()&invocationGateHeld != 0 || d.invocationOwner != 0) {
					t.Fatal("cancel leaked partial admission")
				}
			}
		})
	}
}

func TestDesignInvocationSnapshotLatePublisher(t *testing.T) {
	rt := NewRuntime()
	p := recheckProvider(t, rt.refStore)
	defer p.Close()
	table, err := p.ExportedTable("t")
	if err != nil {
		t.Fatal(err)
	}
	id := newInvocationID()
	lease := p.lockGCInvocation(id)
	if lease.snapshot == nil || lease.domains.len() != 0 {
		lease.unlock()
		t.Fatal("expected empty conversion snapshot")
	}
	q := recheckPublisher(t, rt.refStore, table)
	defer q.Close()
	if _, err = q.Invoke("pub"); err != nil {
		lease.unlock()
		t.Fatal(err)
	}
	word := binary.LittleEndian.Uint64(table.desc[8:])
	receipt := recheckReceipt(rt.refStore, table.desc, 0)
	recheckLiveReceipt(t, receipt, word, table.desc)
	// Native serialization is held, but late registration is not authority.
	nativeExecutionMu.Lock()
	for _, bulk := range []bool{false, true} {
		before := append([]byte(nil), table.desc...)
		published := false
		var e error
		a := gcConversionInvocation{root: p, id: id}
		if bulk {
			e = p.publishConversionRangeInvocation(a, p, table.desc, 0, 2, []uint64{0, word}, func() { published = true })
		} else {
			e = p.publishConversionStoreInvocation(a, p, table.desc, 0, 0, func() error { published = true; return nil })
		}
		if e == nil || published || !bytes.Equal(before, table.desc) || receipt.released {
			nativeExecutionMu.Unlock()
			lease.unlock()
			t.Fatal("late old-domain authority widened snapshot")
		}
		// Incoming late domain into an ordinary slot must fail as well.
		if bulk {
			e = p.publishConversionRangeInvocation(a, p, table.desc, 1, 2, []uint64{word}, func() { published = true })
		} else {
			e = p.publishConversionStoreInvocation(a, p, table.desc, 1, word, func() error { published = true; return nil })
		}
		if e == nil || published {
			nativeExecutionMu.Unlock()
			lease.unlock()
			t.Fatal("late new-domain authority widened snapshot")
		}
	}
	nativeExecutionMu.Unlock()
	lease.unlock()
	// Next public entry legitimately admits the new domain, including bulk copy.
	if _, err = p.Invoke("copy01"); err != nil {
		t.Fatal(err)
	}
	if _, err = p.Invoke("clear0"); err != nil {
		t.Fatal(err)
	}
	if err = table.SetValue(1, ValueExternRef(NullExternRef())); err != nil {
		t.Fatal(err)
	}
}

func TestDesignInvocationSnapshotRetirement(t *testing.T) {
	rt := NewRuntime()
	p := recheckProvider(t, rt.refStore)
	defer p.Close()
	table, err := p.ExportedTable("t")
	if err != nil {
		t.Fatal(err)
	}
	q := recheckPublisher(t, rt.refStore, table)
	d := q.gcInvocationDomain()
	lease := p.lockGCInvocation(newInvocationID())
	if lease.snapshot == nil || lease.domains.len() != 1 {
		lease.unlock()
		t.Fatal("publisher not snapshotted")
	}
	done := make(chan error, 1)
	go func() { done <- q.Close() }()
	select {
	case err = <-done:
	case <-time.After(3 * time.Second):
		lease.unlock()
		t.Fatal("publisher Close froze behind conversion snapshot")
	}
	if err != nil {
		lease.unlock()
		t.Fatal(err)
	}
	rt.refStore.mu.Lock()
	valid := rt.refStore.gcDomains.domainForCollectorLocked(d.collector) == d && d.refs == 0 && d.claims == 1
	rt.refStore.mu.Unlock()
	if !valid || q.hasPhysicalResources() {
		lease.unlock()
		t.Fatal("STOP collector lifetime was not independent of retired publisher")
	}
	lease.unlock()
	rt.refStore.mu.Lock()
	retired := rt.refStore.gcDomains.domainForCollectorLocked(d.collector) == nil && len(rt.refStore.gcDomains.invocations) == 0
	rt.refStore.mu.Unlock()
	if !retired {
		t.Fatal("last invocation pin did not retire collector")
	}
}

func TestDesignInvocationSnapshotAuthenticatedDispatch(t *testing.T) {
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
	lease := p.lockGCInvocation(newInvocationID())
	defer lease.unlock()
	nativeExecutionMu.Lock()
	defer nativeExecutionMu.Unlock()
	// A stale callee field cannot grant authority, and cannot override the
	// authenticated native loop context. Exercise single and bulk dispatch.
	state := p.ensurePluginState()
	saved := state.invocationID
	state.invocationID = newInvocationID()
	defer func() { state.invocationID = saved }()
	caught := func(f func()) (yes bool) { defer func() { yes = recover() != nil }(); f(); return }
	for _, helper := range []uint32{53, 55} {
		args := []uint64{0, 0, 0}
		if helper == 55 {
			args = []uint64{0, 0, 1, 0}
		}
		before := append([]byte(nil), table.desc...)
		stale := hostLoopActivation{root: p, ctrl: offHeapSlicePtr(p.ctrl), invocation: hostInvocationContext{id: state.invocationID}}
		if !caught(func() { stale.dispatch(offHeapSlicePtr(p.ctrl), gcStructDispatchBit|helper, args, nil) }) || !bytes.Equal(before, table.desc) {
			t.Fatal("stale invocation published")
		}
		valid := hostLoopActivation{root: p, ctrl: offHeapSlicePtr(p.ctrl), invocation: hostInvocationContext{id: lease.owner}}
		if !caught(func() { valid.dispatch(1, gcStructDispatchBit|helper, args, nil) }) || !bytes.Equal(before, table.desc) {
			t.Fatal("unregistered native control published")
		}
	}
	valid := hostLoopActivation{root: p, ctrl: offHeapSlicePtr(p.ctrl), invocation: hostInvocationContext{id: lease.owner}}
	valid.dispatch(offHeapSlicePtr(p.ctrl), gcStructDispatchBit|56, []uint64{1, 0, 1, 0, 0}, nil)
	if recheckReceipt(rt.refStore, table.desc, 1) == nil {
		t.Fatal("valid root bulk copy failed")
	}
	valid.dispatch(offHeapSlicePtr(p.ctrl), gcStructDispatchBit|53, []uint64{0, 0, 0}, nil)
	valid.dispatch(offHeapSlicePtr(p.ctrl), gcStructDispatchBit|55, []uint64{1, 0, 1, 0}, nil)
	if binary.LittleEndian.Uint64(table.desc[8:]) != 0 || binary.LittleEndian.Uint64(table.desc[16:]) != 0 {
		t.Fatal("valid root clear failed")
	}
}

func TestDesignInvocationSnapshotCallbackTrap(t *testing.T) {
	for _, trap := range []bool{false, true} {
		name := "return"
		if trap {
			name = "trap"
		}
		t.Run(name, func(t *testing.T) {
			rt := NewRuntime()
			p := recheckProvider(t, rt.refStore)
			defer p.Close()
			table, err := p.ExportedTable("t")
			if err != nil {
				t.Fatal(err)
			}
			q := recheckPublisher(t, rt.refStore, table)
			defer q.Close()
			var r *Instance
			var callbackErr error
			host := slotHostFunc(func(_ HostModule, _, _ []uint64) {
				rt.refStore.mu.Lock()
				snapshot := rt.refStore.gcDomains.invocations[r.currentInvocationID()]
				released := snapshot != nil && !snapshot.active && !snapshot.domains.ownedBy(snapshot.owner)
				rt.refStore.mu.Unlock()
				if !released {
					callbackErr = context.Canceled
					return
				}
				_, callbackErr = q.Invoke("pub")
			})
			body := []byte{0x10, 0}
			if trap {
				body = append(body, 0)
			}
			body = append(body, 0x0b)
			raw := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec([]byte{0x60, 0, 0})), wasmtest.Section(2, wasmtest.Vec(append(append(wasmtest.Name("h"), wasmtest.Name("f")...), 0, 0))), wasmtest.Section(3, wasmtest.Vec([]byte{0})), wasmtest.Section(6, wasmtest.Vec([]byte{0x6f, 1, 0xd0, 0x6f, 0x0b})), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))))
			c, e := Compile(recheckCfg(), raw)
			if e != nil {
				t.Fatal(e)
			}
			defer c.Close()
			r, e = instantiateCore(c, InstantiateOptions{store: rt.refStore, Imports: testImports("h.f", host)})
			if e != nil {
				t.Fatal(e)
			}
			defer r.Close()
			_, e = r.Invoke("run")
			if callbackErr != nil || (e != nil) != trap {
				t.Fatalf("callback=%v invocation=%v trap=%v", callbackErr, e, trap)
			}
			rt.refStore.mu.Lock()
			remaining := len(rt.refStore.gcDomains.invocations)
			rt.refStore.mu.Unlock()
			if remaining != 0 {
				t.Fatal("return/trap leaked invocation snapshot")
			}
			if _, e = p.Invoke("clear0"); e != nil {
				t.Fatal(e)
			}
		})
	}
}
