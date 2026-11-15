//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"encoding/binary"
	"errors"
	"testing"
	"unsafe"

	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func dense16ExecutionModule() []byte {
	imp := append(wasmtest.Name("env"), wasmtest.Name("callback")...)
	imp = append(imp, 0, 5)
	types := [][]byte{
		{0x50, 0, 0x5f, 1, 0x7f, 1},
		{0x4f, 1, 0, 0x5f, 1, 0x7f, 1},
		{0x4f, 1, 0, 0x5f, 2, 0x7f, 1, 0x7f, 1},
		{0x60, 0, 2, 0x6d, 0x6d},
		{0x60, 1, 0x6d, 1, 0x7f},
		{0x60, 0, 0},
	}
	bodies := [][]byte{wasmtest.Code([]byte{0xfb, 1, 1, 0xfb, 1, 2, 0x0b})}
	funcs := [][]byte{{3}}
	exports := [][]byte{wasmtest.ExportEntry("objects", 0, 1)}
	for i, body := range [][]byte{
		{0x20, 0, 0xfb, 22, 1, 0xd1, 0x45, 0x0b},
		{0x20, 0, 0xfb, 22, 0, 0xd1, 0x45, 0x0b},
		{0x20, 0, 0xfb, 22, 2, 0xd1, 0x45, 0x0b},
		{0x20, 0, 0xfb, 20, 2, 0x0b},
		{0x10, 0, 0x20, 0, 0xfb, 22, 1, 0xd1, 0x45, 0x0b},
	} {
		funcs = append(funcs, []byte{4})
		bodies = append(bodies, wasmtest.Code(body))
		exports = append(exports, wasmtest.ExportEntry([]string{"identity", "open", "wrong", "test_wrong", "callback"}[i], 0, uint32(i+2)))
	}
	return wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(types...)), wasmtest.Section(2, wasmtest.Vec(imp)), wasmtest.Section(3, wasmtest.Vec(funcs...)), wasmtest.Section(7, wasmtest.Vec(exports...)), wasmtest.Section(10, wasmtest.Vec(bodies...)))
}

func TestGCDense16PublicEntryDiscardsPriorEvidence(t *testing.T) {
	if !gc.NativeTypeCarriersEnabled() {
		t.Skip("requires WAGO_AMD64_DENSE16_REF=1")
	}
	code, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcNativeDefinedTypeModule())
	if err != nil {
		t.Fatal(err)
	}
	defer code.Close()
	in, err := Instantiate(code, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	original := readGlobalObject(in.globalCells[0], in.c.Globals[0].Type)
	ref := gc.Ref(uint32(original))
	view := in.gc.NativeView()
	handle := unsafe.Slice((*byte)(offHeapPtr(view.Handles+uintptr(ref>>1)*gc.NativeHandleStride)), gc.NativeHandleStride)
	header := unsafe.Slice((*byte)(offHeapPtr(view.Spaces[handle[gc.NativeHandleSpaceOffset]].Base+uintptr(binary.LittleEndian.Uint32(handle)))), gc.HeaderSize)
	originalType := binary.LittleEndian.Uint32(header)
	defer binary.LittleEndian.PutUint32(header, originalType)
	defer writeGlobalObject(in.globalCells[0], in.c.Globals[0].Type, original)
	sibling, ok := in.gcDomainType(2)
	if !ok {
		t.Fatal("missing sibling type")
	}
	// Both numeric-only object layouts fit the existing 24-byte object. The
	// raw control verifies reachability before adding prior type evidence.
	binary.LittleEndian.PutUint32(header, uint32(sibling))
	if result, err := in.Invoke("cast_sibling"); err != nil || len(result) != 1 || result[0] != 1 {
		t.Fatalf("raw control=%v/%v, want sibling cast success", result, err)
	}
	binary.LittleEndian.PutUint32(header, originalType)
	carried := in.gc.TryNativeReferenceWord(ref)
	if carried&gc.NativeTypedRefKnown == 0 {
		t.Fatal("fixture did not mint prior type evidence")
	}
	writeGlobalObject(in.globalCells[0], in.c.Globals[0].Type, carried)
	binary.LittleEndian.PutUint32(header, uint32(sibling))
	if result, err := in.Invoke("cast_sibling"); err != nil || len(result) != 1 || result[0] != 1 {
		t.Fatalf("prior carrier disagrees with current valid header: %v/%v", result, err)
	}
}

func TestGCDense16IngressRetainsNativeViewChecks(t *testing.T) {
	code, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), dense16ExecutionModule())
	if err != nil {
		t.Fatal(err)
	}
	defer code.Close()
	in, err := Instantiate(code, testImports("env.callback", slotHostFunc(func(HostModule, []uint64, []uint64) {})))
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	objects, err := in.Invoke("objects")
	if err != nil || len(objects) != 2 {
		t.Fatalf("objects=%v/%v", objects, err)
	}
	token := objects[0]
	view := in.gc.NativeView()
	count := view.HandleCount
	view.HandleCount = 0
	defer func() { view.HandleCount = count }()
	_, err = in.Invoke("identity", token)
	var trap *TrapError
	if !errors.As(err, &trap) || trap.Code != TrapCastFailure {
		t.Fatalf("zero native handle count rejection=%v, want cast failure", err)
	}
}

func TestGCDense16AuthenticatedIngressAndCallback(t *testing.T) {
	if !gc.NativeTypeCarriersEnabled() {
		t.Skip("requires WAGO_AMD64_DENSE16_REF=1")
	}
	code, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), dense16ExecutionModule())
	if err != nil {
		t.Fatal(err)
	}
	defer code.Close()
	calls := 0
	in, err := Instantiate(code, testImports("env.callback", slotHostFunc(func(HostModule, []uint64, []uint64) { calls++ })))
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	objects, err := in.Invoke("objects")
	if err != nil || len(objects) != 2 {
		t.Fatalf("objects=%v/%v", objects, err)
	}
	token := objects[0]
	for _, tc := range []struct {
		name string
		want uint64
	}{{"identity", 1}, {"open", 1}, {"test_wrong", 0}, {"callback", 1}} {
		before := in.gc.NativeTypeEpoch() + 1
		result, err := in.Invoke(tc.name, token)
		if err != nil || len(result) != 1 || result[0] != tc.want {
			t.Fatalf("%s=%v/%v want%d", tc.name, result, err, tc.want)
		}
		word := binary.LittleEndian.Uint64(in.serArgs)
		if word&gc.NativeTypedRefKnown == 0 || uint32(word>>gc.NativeTypedRefEpochShift) != before {
			t.Fatalf("%s did not carry authenticated type evidence: %#x, epoch%d", tc.name, word, before)
		}
		if tc.name == "callback" && (calls != 1 || in.gc.NativeTypeEpoch() != before+1) {
			t.Fatalf("callback calls%d epoch%d want1/%d", calls, in.gc.NativeTypeEpoch(), before+1)
		}
	}
	_, err = in.Invoke("wrong", token)
	var trap *TrapError
	if !errors.As(err, &trap) || trap.Code != TrapCastFailure {
		t.Fatalf("wrong type rejection=%v, want cast failure", err)
	}
}
