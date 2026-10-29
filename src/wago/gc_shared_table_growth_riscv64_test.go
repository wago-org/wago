//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"bytes"
	"testing"
)

func TestRV64RootedSharedTableGrowthCapacityAndAllocations(t *testing.T) {
	providerCode, err := Compile(rv64RootedScalarConfig(), rootedScalarSharedAggregateTableModule(false))
	if err != nil {
		t.Fatal(err)
	}
	defer providerCode.Close()
	// The importer accepts an external maximum of three, while the provider
	// reserves only two slots. Growth must use the provider's storage bound.
	source := rootedScalarSharedAggregateTableModule(true)
	oldType := []byte{0x63, 0, 1, 1, 2}
	if bytes.Count(source, oldType) != 1 {
		t.Fatal("shared table import type is not unique")
	}
	source = bytes.Replace(source, oldType, []byte{0x63, 0, 1, 1, 3}, 1)
	consumerCode, err := Compile(rv64RootedScalarConfig(), source)
	if err != nil {
		t.Fatal(err)
	}
	defer consumerCode.Close()
	store := newReferenceStore(false)
	defer store.closeRuntime()
	config := GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, VerifyAfterCollect: true}
	provider, err := instantiateCore(providerCode, InstantiateOptions{GC: config, store: store})
	if err != nil {
		t.Fatal(err)
	}
	defer provider.Close()
	table, err := provider.ExportedTable("objects")
	if err != nil {
		t.Fatal(err)
	}
	consumer, err := instantiateCore(consumerCode, InstantiateOptions{GC: config, store: store, Imports: testImports("provider.objects", table)})
	if err != nil {
		t.Fatal(err)
	}
	defer consumer.Close()
	for _, tc := range []struct{ delta, want uint64 }{{2, uint64(^uint32(0))}, {1, 1}, {1, uint64(^uint32(0))}, {0, 2}} {
		got, err := consumer.Invoke("grow", tc.delta)
		if err != nil || len(got) != 1 || got[0] != tc.want {
			t.Fatalf("grow(%d)=%v, %v; want %d", tc.delta, got, err, tc.want)
		}
	}
	if _, err := consumer.Invoke("set", 1, 77); err != nil {
		t.Fatal(err)
	}
	if err := provider.Close(); err != nil {
		t.Fatal(err)
	}
	if err := consumer.CollectGC(); err != nil {
		t.Fatal(err)
	}
	got, err := consumer.Invoke("read", 1)
	if err != nil || len(got) != 1 || got[0] != 77 {
		t.Fatalf("grown root after provider close=%v, %v", got, err)
	}
	args, results := [3]uint64{}, [1]uint64{}
	allocs := testing.AllocsPerRun(1000, func() {
		consumer.dispatchGCArrayHelper(gcTableGrow, args[:], results[:])
		if results[0] != 2 {
			panic("zero-delta growth failed")
		}
	})
	t.Logf("warm imported table.grow helper: %.0f allocations/run", allocs)
	if allocs != 0 {
		t.Fatalf("warm imported table.grow helper allocations=%v, want 0", allocs)
	}
}
