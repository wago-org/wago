//go:build ((linux && (amd64 || arm64)) || (darwin && arm64)) && !tinygo && !wago_target_tinygo && !wago_guardpage

package wago

import (
	"context"
	"encoding/binary"
	"testing"

	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestExternStoreOrdinaryNonNullDispatchAvoidsStoreLock(t *testing.T) {
	rt := NewRuntime(WithRuntimeConfig(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3)))
	defer rt.Close()
	compiled, err := rt.Compile(benchExternrefTableModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	in, err := rt.Instantiate(context.Background(), compiled)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	ref := issueExternref(t, rt, "ordinary non-null")
	store := in.refStore
	if store == nil || store.conversionDomainEverEnabled.Load() {
		t.Fatal("ordinary Store unexpectedly enabled conversion")
	}

	// Holding store.mu across the real helper dispatch is intentional: this
	// completes only when the conversion-free route performs no Store lookup.
	store.mu.Lock()
	in.dispatchConversionStoreInvocation(in, newInvocationID(), gcConversionTableSet, []uint64{0, ref.token, 0}, nil)
	store.mu.Unlock()
	desc, _ := in.conversionTableStorage(0)
	if got := binary.LittleEndian.Uint64(desc[8:]); got != ref.token {
		t.Fatalf("stored word = %#x, want %#x", got, ref.token)
	}
}

func TestExternStoreBulkDispatchAvoidsStoreLockAndStaging(t *testing.T) {
	rt := NewRuntime()
	defer rt.Close()
	compiled, err := rt.Compile(watToWasm(t, `(module
		(table 2 4 externref)
		(func (export "seed") (param externref) (drop (local.get 0)))
	)`))
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	in, err := rt.Instantiate(context.Background(), compiled)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	store := in.refStore
	if store == nil || store.conversionDomainEverEnabled.Load() {
		t.Fatal("ordinary Store unexpectedly enabled conversion")
	}
	ref := issueExternref(t, rt, "bulk ordinary").token

	store.mu.Lock()
	in.dispatchConversionStoreInvocation(in, newInvocationID(), 55, []uint64{0, ref, 2, 0}, nil)
	// The source and destination alias; the direct path must retain memmove
	// semantics rather than overwrite the second source before reading it.
	in.dispatchConversionStoreInvocation(in, newInvocationID(), 56, []uint64{1, 0, 1, 0, 0}, nil)
	var grow [1]uint64
	in.dispatchConversionStoreInvocation(in, newInvocationID(), 57, []uint64{ref, 1, 0}, grow[:])
	store.mu.Unlock()
	desc, _ := in.conversionTableStorage(0)
	if grow[0] != 2 || binary.LittleEndian.Uint32(desc) != 3 {
		t.Fatalf("grow result/length = %d/%d, want 2/3", grow[0], binary.LittleEndian.Uint32(desc))
	}
	for i := uint64(0); i < 3; i++ {
		if got := binary.LittleEndian.Uint64(desc[8+i*8:]); got != ref {
			t.Fatalf("slot %d = %#x, want %#x", i, got, ref)
		}
	}
}

func TestExternStoreLaterConversionUsesReceiptOnEarlierStorage(t *testing.T) {
	rt := NewRuntime()
	defer rt.Close()
	ordinaryCompiled, err := rt.Compile(watToWasm(t, `(module
		(table (export "t") 1 externref)
		(func (export "set_and_get") (param externref) (result externref)
			(i32.const 0) (local.get 0) table.set
			(i32.const 0) table.get)
	)`))
	if err != nil {
		t.Fatal(err)
	}
	defer ordinaryCompiled.Close()
	ordinary, err := rt.Instantiate(context.Background(), ordinaryCompiled)
	if err != nil {
		t.Fatal(err)
	}
	defer ordinary.Close()
	if ordinary.refStore.conversionDomainEverEnabled.Load() {
		t.Fatal("conversion epoch enabled before a conversion module exists")
	}
	table, err := ordinary.ExportedTable("t")
	if err != nil {
		t.Fatal(err)
	}

	conversionBytes := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5f, 0x01, 0x7f, 0x00},
			wasmtest.FuncType(nil, nil),
		)),
		wasmtest.Section(2, wasmtest.Vec(append(append(wasmtest.Name("env"), wasmtest.Name("t")...), 0x01, 0x6f, 0x00, 0x01))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
			0x41, 0x00, // i32.const 0
			0x41, 0x2a, // i32.const 42
			0xfb, 0x1c, // ref.i31
			0xfb, 0x1b, // extern.convert_any
			0x26, 0x00, // table.set 0
			0x0b,
		}))),
	)
	conversionCompiled, err := rt.Compile(conversionBytes)
	if err != nil {
		t.Fatal(err)
	}
	defer conversionCompiled.Close()
	conversion, err := rt.Instantiate(context.Background(), conversionCompiled, WithImports(NewImports().Table("env", "t", table)))
	if err != nil {
		t.Fatal(err)
	}
	defer conversion.Close()
	if !ordinary.refStore.conversionDomainEverEnabled.Load() {
		t.Fatal("conversion epoch was not published")
	}
	if _, err = conversion.Invoke("run"); err != nil {
		t.Fatal(err)
	}
	desc := table.desc
	h2WithAuthority(t, ordinary.refStore, func() {
		container := ordinary.refStore.gcDomains.conversionContainers[&desc[0]]
		if container == nil || container.slots[0] == nil {
			t.Fatal("converted value in earlier storage has no physical receipt")
		}
	})
}

func TestExternStoreByteBackedInitializerRetainsReceipt(t *testing.T) {
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x60, 0, 0})),
		wasmtest.Section(3, wasmtest.Vec([]byte{0})),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x6f, 0, 1})),
		wasmtest.Section(6, wasmtest.Vec([]byte{0x6f, 0, 0x41, 1, 0xfb, 0x1c, 0xfb, 0x1b, 0x0b})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 0, 0x23, 0, 0x26, 0, 0x0b}))),
	)
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), module)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	in, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if _, err = in.Invoke("run"); err != nil {
		t.Fatal(err)
	}
	desc, _ := in.conversionTableStorage(0)
	if word := binary.LittleEndian.Uint64(desc[8:]); word == 0 {
		t.Fatal("initializer produced null")
	}
	h2WithAuthority(t, in.refStore, func() {
		container := in.refStore.gcDomains.conversionContainers[&desc[0]]
		if container == nil || container.slots[0] == nil {
			t.Fatal("byte-backed initializer product has no physical receipt")
		}
	})
}
