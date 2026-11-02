//go:build darwin && arm64 && !tinygo && !wago_target_tinygo

package wago

import (
	"encoding/binary"
	"fmt"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"runtime"
	"testing"
	"time"
)

func TestDesignRepairWaitNotifier(t *testing.T) {
	memory, err := NewSharedMemory(1, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer memory.Close()
	rt := NewRuntime()
	imp := append(append(wasmtest.Name("env"), wasmtest.Name("memory")...), 2, 3, 1, 1)
	waiterWasm := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x60, 3, 0x7f, 0x7f, 0x7e, 1, 0x7f})),
		wasmtest.Section(2, wasmtest.Vec(imp)), wasmtest.Section(3, wasmtest.Vec([]byte{0})),
		wasmtest.Section(6, wasmtest.Vec([]byte{0x6f, 1, 0xd0, 0x6f, 0x0b})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("wait", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0x20, 1, 0x20, 2, 0xfe, 1, 2, 0, 0x0b}))))
	notifierWasm := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x5f, 1, 0x7f, 1}, []byte{0x60, 2, 0x7f, 0x7f, 1, 0x7f})),
		wasmtest.Section(2, wasmtest.Vec(imp)), wasmtest.Section(3, wasmtest.Vec([]byte{1})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("notify", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0xfb, 1, 0, 0x1a, 0x20, 0, 0x20, 1, 0xfe, 0, 2, 0, 0x0b}))))
	makeInstance := func(raw []byte) *Instance {
		c, e := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3|CoreFeatureThreads).WithBoundsChecks(BoundsChecksExplicit), raw)
		if e != nil {
			t.Fatal(e)
		}
		t.Cleanup(func() { c.Close() })
		in, e := instantiateCore(c, InstantiateOptions{store: rt.refStore, Imports: testImports("env.memory", memory)})
		if e != nil {
			t.Fatal(e)
		}
		return in
	}
	waiter, notifier := makeInstance(waiterWasm), makeInstance(notifierWasm)
	defer waiter.Close()
	defer notifier.Close()
	if notifier.gc == nil || waiter.gc != nil {
		t.Fatal("incorrect domain fixture")
	}
	done := make(chan error, 1)
	go func() {
		out, e := waiter.Invoke("wait", 0, 0, uint64(5*time.Second))
		if e == nil && (len(out) != 1 || out[0] != 0) {
			e = fmt.Errorf("wait result %v", out)
		}
		done <- e
	}()
	waitForMemoryWaiters(t, memory, 1)

	notified := make(chan error, 1)
	go func() {
		out, err := notifier.Invoke("notify", 0, 1)
		if err == nil && (len(out) != 1 || out[0] != 1) {
			err = fmt.Errorf("notify result %v", out)
		}
		notified <- err
	}()
	select {
	case err := <-notified:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("notifier blocked while guest wait held")
	}

	if err := <-done; err != nil {
		t.Fatal(err)
	}
	assertNoMemoryWaiters(t, memory)
}

func TestDesignRepairConversionAdmissionLifecycle(t *testing.T) {
	rt := NewRuntime()
	publisher, provider, table, word := h2ReceiptFixture(t, rt.refStore, 0)
	domain := publisher.gcInvocationDomain()
	contains := func() bool {
		v, topology := provider.gcInvocationDomainsForInspection()
		if topology != nil {
			defer topology.RUnlock()
		}
		for i := 0; i < v.len(); i++ {
			if v.at(i) == domain {
				return true
			}
		}
		return false
	}
	if !contains() {
		t.Fatal("STOP publisher authority not pre-admitted")
	}
	h2TableSet(t, table, 0, word)
	h2AssertReceipt(t, table.conversionContainers[0], 0)
	if err := publisher.Close(); err != nil {
		t.Fatal(err)
	}
	if publisher.hasPhysicalResources() {
		t.Fatal("STOP publisher retained")
	}
	if !contains() {
		t.Fatal("STOP receipt authority absent after publisher retirement")
	}
	exported, err := provider.ExportedTable("slots")
	if err != nil {
		t.Fatal(err)
	}
	if err = exported.SetValue(0, ValueExternRef(NullExternRef())); err != nil {
		t.Fatal(err)
	}
	if binary.LittleEndian.Uint64(exported.desc[8:]) != 0 {
		t.Fatal("clear not published")
	}
	if contains() {
		t.Fatal("retired receipt domain still admitted")
	}
	provider.Close()
}

func TestDesignRepairScalarRelayOrdinaryStorageWithConversion(t *testing.T) {
	rt := NewRuntime()
	publisher, provider, _, _ := h2ReceiptFixture(t, rt.refStore, 0)
	defer publisher.Close()
	defer provider.Close()
	cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3)
	producerRaw := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x60, 0, 1, 0x7f})), wasmtest.Section(2, wasmtest.Vec(append(append(wasmtest.Name("h"), wasmtest.Name("f")...), 0, 0))),
		wasmtest.Section(3, wasmtest.Vec([]byte{0})),
		wasmtest.Section(6, wasmtest.Vec([]byte{0x6f, 1, 0xd0, 0x6f, 0x0b})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0xd0, 0x6f, 0x24, 0, 0x10, 0, 0x0b}))))
	pc, err := Compile(cfg, producerRaw)
	if err != nil {
		t.Fatal(err)
	}
	defer pc.Close()
	producer, err := instantiateCore(pc, InstantiateOptions{store: rt.refStore, Imports: testImports("h.f", slotHostFunc(func(_ HostModule, _, out []uint64) { out[0] = 7 }))})
	if err != nil {
		t.Fatal(err)
	}
	defer producer.Close()
	f, err := producer.ExportedFunc("f")
	if err != nil {
		t.Fatal(err)
	}
	imp := append(append(wasmtest.Name("p"), wasmtest.Name("f")...), 0, 0)
	raw := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec([]byte{0x60, 0, 1, 0x7f})), wasmtest.Section(2, wasmtest.Vec(imp)), wasmtest.Section(3, wasmtest.Vec([]byte{0})), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x10, 0, 0x0b}))))
	cc, err := Compile(cfg, raw)
	if err != nil {
		t.Fatal(err)
	}
	defer cc.Close()
	consumer, err := instantiateCore(cc, InstantiateOptions{store: NewRuntime().refStore, Imports: testImports("p.f", f)})
	if err != nil {
		t.Fatal(err)
	}
	defer consumer.Close()
	if v := consumer.gcInvocationDomains(); v.len() != 0 {
		t.Fatal("foreign scalar relay copied unrelated conversion authority")
	}
	out, err := consumer.Invoke("run")
	if err != nil || len(out) != 1 || out[0] != 7 {
		t.Fatalf("ordinary scalar relay: %v %v", out, err)
	}
}

// The public publisher owns its complete domain lease before native entry.
// Physical publisher retirement must wait until the new receipt is committed.
func TestDesignRepairConcurrentPublicationRetirement(t *testing.T) {
	rt := NewRuntime()
	publisher, provider, _, _ := h2ReceiptFixture(t, rt.refStore, 0)
	host, err := rt.NewExternRefTable(1, 1)
	if err != nil {
		t.Fatal(err)
	}
	token := h2HostToken(t, publisher)
	domain := publisher.gcInvocationDomain()
	nativeExecutionMu.Lock()
	held := true
	defer func() {
		if held {
			nativeExecutionMu.Unlock()
		}
	}()
	published := make(chan error, 1)
	go func() { published <- host.SetValue(0, token) }()
	await := func(what string, ready func() bool) {
		t.Helper()
		deadline := time.After(5 * time.Second)
		for !ready() {
			select {
			case <-deadline:
				t.Fatalf("STOP waiting for %s", what)
			default:
				runtime.Gosched()
			}
		}
	}
	await("publication pre-admission", func() bool {
		domain.invocationState.Lock()
		defer domain.invocationState.Unlock()
		return domain.invocationOwner != 0
	})
	retired := make(chan error, 1)
	go func() { retired <- publisher.Close() }()
	topology := rt.refStore.ensureGCTopology()
	await("retirement topology writer", func() bool { topology.mu.Lock(); defer topology.mu.Unlock(); return topology.writers != 0 })
	nativeExecutionMu.Unlock()
	held = false
	if err = <-published; err != nil {
		t.Fatalf("STOP publication: %v", err)
	}
	if err = <-retired; err != nil {
		t.Fatalf("STOP retirement: %v", err)
	}
	if publisher.hasPhysicalResources() {
		t.Fatal("STOP publisher retained")
	}
	receipt := h2HostReceipt(t, rt.refStore, host.desc, 0)
	if receipt.identity.domain != domain {
		t.Fatal("STOP wrong receipt authority")
	}
	if err = host.SetValue(0, ValueExternRef(NullExternRef())); err != nil {
		t.Fatal(err)
	}
	if err = host.SetValue(0, token); err == nil {
		t.Fatal("STOP retired identity accepted")
	}
	host.Close()
	provider.Close()
	rt.Close()
}
