//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/src/core/runtime/gc/native"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func genericGCTableHelperModule() []byte {
	body := []byte{
		0x00,
		0x41, 0x2a, 0xfb, 0x00, 0x00, 0x1a,
		0x41, 0x00,
		0x0b,
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5f, 0x01, 0x7f, 0x01},
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(4, []byte{0x02, 0x63, 0x00, 0x01, 0x02, 0x04, 0x63, 0x00, 0x01, 0x02, 0x04}),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func TestGenericGCTableBulkHelpers(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithBoundsChecks(BoundsChecksExplicit), genericGCTableHelperModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	if compiled.stagedGCStructProduct() != stagedGCStructGeneric {
		t.Fatalf("struct product = %s, want generic", compiled.stagedGCStructProduct())
	}
	instance, err := Instantiate(compiled, InstantiateOptions{GC: GCConfig{VerifyAfterCollect: true}})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	allocate := func(value uint64) gc.Ref {
		t.Helper()
		var result [1]uint64
		instance.dispatchGCHelper(gcStructAllocOne, []uint64{value, 0}, result[:])
		return gc.Ref(uint32(result[0]))
	}
	first, second := allocate(42), allocate(7)
	descriptor := instance.tableDescriptor(0)
	secondDescriptor := instance.tableDescriptor(1)
	instance.dispatchGCHelper(gcTableFill, []uint64{0, uint64(first), 1, 0}, nil)
	instance.dispatchGCHelper(gcTableCopy, []uint64{1, 0, 1, 0, 0}, nil)
	instance.dispatchGCHelper(gcTableCopy, []uint64{0, 0, 1, 1, 0}, nil)
	instance.dispatchGCHelper(gcTableFill, []uint64{0, 0, 1, 0}, nil)
	if got := gc.Ref(uint32(binary.LittleEndian.Uint64(descriptor[8+8:]))); got != first {
		t.Fatalf("copied table root = %v, want %v", got, first)
	}
	if got := gc.Ref(uint32(binary.LittleEndian.Uint64(secondDescriptor[8:]))); got != first {
		t.Fatalf("cross-table copied root = %v, want %v", got, first)
	}
	var result [1]uint64
	instance.dispatchGCHelper(gcTableGrow, []uint64{uint64(second), 1, 0}, result[:])
	if result[0] != 2 || binary.LittleEndian.Uint32(descriptor) != 3 || gc.Ref(uint32(binary.LittleEndian.Uint64(descriptor[8+2*8:]))) != second {
		t.Fatalf("table.grow result/descriptor = %d/%x", result[0], descriptor)
	}
	// Same-table overlap must behave like memmove: [null, first, second]
	// copied one slot right becomes [null, null, first].
	instance.dispatchGCHelper(gcTableCopy, []uint64{1, 0, 2, 0, 0}, nil)
	if binary.LittleEndian.Uint64(descriptor[8+8:]) != 0 || gc.Ref(uint32(binary.LittleEndian.Uint64(descriptor[8+2*8:]))) != first {
		t.Fatalf("overlapping table.copy descriptor = %x", descriptor)
	}
	beforeFailure := bytes.Clone(descriptor)
	instance.dispatchGCHelper(gcTableGrow, []uint64{0, 2, 0}, result[:])
	if result[0] != uint64(^uint32(0)) || !bytes.Equal(descriptor, beforeFailure) {
		t.Fatalf("failed table.grow result/descriptor = %#x/%x, before %x", result[0], descriptor, beforeFailure)
	}
	expectBoundsTrap := func(action func()) {
		t.Helper()
		var recovered any
		func() {
			defer func() { recovered = recover() }()
			action()
		}()
		trap, ok := recovered.(gcStructHelperTrap)
		if !ok || trap.code != coreruntime.TrapTableOutOfBounds {
			t.Fatalf("table helper panic = %#v, want table out-of-bounds trap", recovered)
		}
	}
	expectBoundsTrap(func() { instance.dispatchGCHelper(gcTableFill, []uint64{3, uint64(second), 1, 0}, nil) })
	expectBoundsTrap(func() { instance.dispatchGCHelper(gcTableCopy, []uint64{0, 3, 1, 0, 0}, nil) })
	if !bytes.Equal(descriptor, beforeFailure) {
		t.Fatalf("out-of-bounds mutation changed descriptor %x -> %x", beforeFailure, descriptor)
	}
	if err := instance.CollectGC(); err != nil {
		t.Fatal(err)
	}
	value, err := instance.gc.StructGet(first, 0)
	if err != nil || value.Bits != 42 {
		t.Fatalf("copied rooted object = %+v, %v", value, err)
	}
}
