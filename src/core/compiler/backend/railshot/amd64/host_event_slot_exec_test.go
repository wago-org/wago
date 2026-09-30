//go:build (linux || darwin || windows) && amd64

package amd64

import (
	"encoding/binary"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestDirectHostEventAboveLiveV128(t *testing.T) {
	const low = uint64(0x123456789abcdef0)
	body := []byte{0xfd, 0x0c}
	body = binary.LittleEndian.AppendUint64(body, low)
	body = binary.LittleEndian.AppendUint64(body, 0x1122334455667788)
	body = append(body, 0x41, 42, 0x10, 0, 0xfd, 0x1d, 0, 0x0b)
	imp := append(wasmtest.Name("env"), wasmtest.Name("event")...)
	imp = append(imp, 0, 0)
	moduleBytes := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, nil), wasmtest.FuncType(nil, []wasm.ValType{wasm.I64}))),
		wasmtest.Section(2, wasmtest.Vec(imp)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
	m, err := wasm.DecodeModule(moduleBytes)
	if err != nil {
		t.Fatal(err)
	}
	// The direct API uses the event-log path without dynamic import bindings.
	cm, err := CompileModule(m)
	if err != nil {
		t.Fatal(err)
	}
	if cm.CodeImage != nil {
		defer cm.CodeImage.Close()
	}
	eng, err := runtime.NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close()
	jm, err := runtime.NewJobMemory(65536)
	if err != nil {
		t.Fatal(err)
	}
	defer jm.Close()
	ar, err := runtime.NewArena(4096)
	if err != nil {
		t.Fatal(err)
	}
	defer ar.Close()
	code, entry, err := runtime.MapCode(cm.Code)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Unmap(code)
	args := ar.Alloc(128)
	results := ar.Alloc(128)
	trap := ar.Alloc(runtime.TrapBufferBytes)
	events := ar.Alloc(256)
	clear(events)
	jm.SetCustomCtx(uintptr(unsafe.Pointer(&events[0])))
	if err := eng.Call(entry+uintptr(cm.Entry[0]), args, jm.LinearMemory(), trap, results); err != nil {
		t.Fatal(err)
	}
	if got := binary.LittleEndian.Uint64(results); got != low {
		t.Errorf("live SIMD low lane = %#x, want %#x", got, low)
	}
	if got := binary.LittleEndian.Uint32(events); got != 1 {
		t.Fatalf("event count = %d, want 1", got)
	}
	if got := binary.LittleEndian.Uint32(events[8:]); got != 0 {
		t.Errorf("event import index = %d, want 0", got)
	}
	if got := binary.LittleEndian.Uint32(events[12:]); got != 42 {
		t.Errorf("event argument = %#x, want 42", got)
	}
}
