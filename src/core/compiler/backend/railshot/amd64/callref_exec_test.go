//go:build linux && amd64

package amd64

import (
	"encoding/binary"
	"strings"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/testutil/wasmtest"
)

func callRefModule(t *testing.T) *wasm.Module {
	t.Helper()
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.FuncRef}, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1), wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("call", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0x00, 0x20, 0x01, 0x14, 0x00, 0x0b}),
			wasmtest.Code([]byte{0x20, 0x00, 0x0b}),
		)),
	)
	m, err := wasm.DecodeModule(mod)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func runCallRefRaw(t *testing.T, m *wasm.Module, value uint64, descriptor bool, sigKey uint64) ([]byte, error) {
	t.Helper()
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatalf("validate: %v", err)
	}
	cm, err := CompileModule(m)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	eng, err := coreruntime.NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close()
	jm, err := coreruntime.NewJobMemory(65536)
	if err != nil {
		t.Fatal(err)
	}
	defer jm.Close()
	arena, err := coreruntime.NewArena(4096)
	if err != nil {
		t.Fatal(err)
	}
	defer arena.Close()
	code, base, err := coreruntime.MapCode(cm.Code)
	if err != nil {
		t.Fatal(err)
	}
	defer coreruntime.Unmap(code)

	var ref uint64
	if descriptor {
		descs := arena.Alloc(2 * coreruntime.FuncRefDescBytes)
		context := arena.Alloc(coreruntime.InstanceContextBytes)
		contextPtr := uint64(uintptr(unsafe.Pointer(&context[0])))
		binary.LittleEndian.PutUint64(descs[coreruntime.FuncRefContextOffset:], contextPtr)
		desc := descs[coreruntime.FuncRefDescBytes:]
		binary.LittleEndian.PutUint64(desc[coreruntime.TableEntryCodePtrOffset:], uint64(base)+uint64(cm.InternalEntry[1]))
		binary.LittleEndian.PutUint64(desc[coreruntime.TableEntrySigKeyOffset:], sigKey)
		binary.LittleEndian.PutUint64(desc[coreruntime.TableEntryHomeLinMemOffset:], uint64(jm.LinMemBase())|uint64(1)<<63)
		binary.LittleEndian.PutUint64(desc[coreruntime.FuncRefContextOffset:], contextPtr)
		jm.SetFuncRefDesc(uintptr(unsafe.Pointer(&descs[0])))
		ref = uint64(uintptr(unsafe.Pointer(&desc[0])))
	}
	args := arena.Alloc(16)
	binary.LittleEndian.PutUint64(args, value)
	binary.LittleEndian.PutUint64(args[8:], ref)
	results := arena.Alloc(8)
	trap := arena.Alloc(8)
	err = eng.Call(base+uintptr(cm.Entry[0]), args, jm.LinearMemory(), trap, results)
	return append([]byte(nil), results...), err
}

func returnCallRefModule(t *testing.T, null bool) *wasm.Module {
	t.Helper()
	body := []byte{
		0x20, 0x00, 0x45, 0x04, 0x7f, 0x41, 0x07, 0x05,
		0x20, 0x00, 0x41, 0x01, 0x6b,
		0xd2, 0x00,
		0x15, 0x00,
		0x0b, 0x0b,
	}
	var elem []byte
	if null {
		body = []byte{0x20, 0x00, 0xd0, 0x00, 0x15, 0x00, 0x0b}
	} else {
		elem = wasmtest.Section(9, wasmtest.Vec(append([]byte{0x03, 0x00}, wasmtest.Vec(wasmtest.ULEB(0))...)))
	}
	sections := [][]byte{
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
	}
	if len(elem) != 0 {
		sections = append(sections, elem)
	}
	sections = append(sections, wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))))
	m, err := wasm.DecodeModule(wasmtest.Module(sections...))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func runReturnCallRefRaw(t *testing.T, m *wasm.Module, n uint64, sigKey uint64, internal bool) ([]byte, error) {
	t.Helper()
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatalf("validate: %v", err)
	}
	cm, err := CompileModule(m)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	eng, err := coreruntime.NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close()
	jm, err := coreruntime.NewJobMemory(65536)
	if err != nil {
		t.Fatal(err)
	}
	defer jm.Close()
	arena, err := coreruntime.NewArena(4096)
	if err != nil {
		t.Fatal(err)
	}
	defer arena.Close()
	code, base, err := coreruntime.MapCode(cm.Code)
	if err != nil {
		t.Fatal(err)
	}
	defer coreruntime.Unmap(code)

	descs := arena.Alloc(2 * coreruntime.FuncRefDescBytes)
	context := arena.Alloc(coreruntime.InstanceContextBytes)
	contextPtr := uint64(uintptr(unsafe.Pointer(&context[0])))
	binary.LittleEndian.PutUint64(descs[coreruntime.FuncRefContextOffset:], contextPtr)
	entry := descs[coreruntime.FuncRefDescBytes:]
	entryOff := cm.Entry[0]
	home := uint64(jm.LinMemBase())
	if internal {
		entryOff = cm.InternalEntry[0]
		home |= uint64(1) << 63
	}
	binary.LittleEndian.PutUint64(entry[coreruntime.TableEntryCodePtrOffset:], uint64(base)+uint64(entryOff))
	binary.LittleEndian.PutUint64(entry[coreruntime.TableEntrySigKeyOffset:], sigKey)
	binary.LittleEndian.PutUint64(entry[coreruntime.TableEntryHomeLinMemOffset:], home)
	binary.LittleEndian.PutUint64(entry[coreruntime.FuncRefContextOffset:], contextPtr)
	jm.SetFuncRefDesc(uintptr(unsafe.Pointer(&descs[0])))

	args := arena.Alloc(8)
	binary.LittleEndian.PutUint64(args, n)
	results := arena.Alloc(8)
	trap := arena.Alloc(8)
	err = eng.Call(base+uintptr(cm.Entry[0]), args, jm.LinearMemory(), trap, results)
	return append([]byte(nil), results...), err
}

func TestCallRefInvokesIndexedTypedDescriptor(t *testing.T) {
	m := callRefModule(t)
	m.Types[1].SubTypes[0].Comp.Params[1] = wasm.RefVal(wasm.Ref(false, wasm.IndexedHeap(wasm.TypeIdx{Index: 0}), false))
	wantSig := m.StructuralTypeKey(0)
	out, err := runCallRefRaw(t, m, 73, true, wantSig)
	if err != nil {
		t.Fatal(err)
	}
	if got := binary.LittleEndian.Uint32(out); got != 73 {
		t.Fatalf("result = %d, want 73", got)
	}
}

func TestCallRefInvokesLocalDescriptorAndMatchesTraps(t *testing.T) {
	m := callRefModule(t)
	m.Types[1].SubTypes[0].Comp.Params[1] = wasm.RefVal(wasm.Ref(true, wasm.IndexedHeap(wasm.TypeIdx{Index: 0}), false))
	wantSig := m.StructuralTypeKey(0)
	out, err := runCallRefRaw(t, m, 42, true, wantSig)
	if err != nil {
		t.Fatal(err)
	}
	if got := binary.LittleEndian.Uint32(out); got != 42 {
		t.Fatalf("result = %d, want 42", got)
	}

	if _, err := runCallRefRaw(t, m, 42, false, wantSig); err == nil || !strings.Contains(err.Error(), "indirect call out of bounds") {
		t.Fatalf("null call_ref trap = %v", err)
	}
	if _, err := runCallRefRaw(t, m, 42, true, wantSig+1); err == nil || !strings.Contains(err.Error(), "wrong signature") {
		t.Fatalf("wrong-signature call_ref trap = %v", err)
	}
}

func TestReturnCallRefReusesFrameAndFailsClosed(t *testing.T) {
	m := returnCallRefModule(t, false)
	wantSig := m.StructuralTypeKey(0)
	out, err := runReturnCallRefRaw(t, m, 1_000_000, wantSig, true)
	if err != nil {
		t.Fatalf("million-deep return_call_ref recursion trapped: %v", err)
	}
	if got := binary.LittleEndian.Uint32(out); got != 7 {
		t.Fatalf("result = %d, want 7", got)
	}

	if _, err := runReturnCallRefRaw(t, m, 1, wantSig+1, true); err == nil || !strings.Contains(err.Error(), "wrong signature") {
		t.Fatalf("wrong-signature return_call_ref trap = %v", err)
	}
	if _, err := runReturnCallRefRaw(t, m, 1, wantSig, false); err == nil || !strings.Contains(err.Error(), "unsupported context switch") {
		t.Fatalf("wrapper return_call_ref trap = %v", err)
	}

	nullModule := returnCallRefModule(t, true)
	if _, err := runReturnCallRefRaw(t, nullModule, 1, nullModule.StructuralTypeKey(0), true); err == nil || !strings.Contains(err.Error(), "indirect call out of bounds") {
		t.Fatalf("null return_call_ref trap = %v", err)
	}
}
