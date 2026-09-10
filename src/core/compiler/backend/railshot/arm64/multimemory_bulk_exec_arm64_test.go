//go:build (linux || darwin) && arm64

package arm64

import (
	"encoding/binary"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/src/core/runtime/abi"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func multiMemoryFillModuleArm64(t testing.TB) *wasm.Module {
	t.Helper()
	body := []byte{
		0x00,
		0x20, 0x00, 0x20, 0x01, 0x41, 0x05, // dst, value, n=5
		0xfc, 0x0b, 0x01, // memory.fill 1
		0x41, 0x00, 0x0b,
	}
	entry := append(wasmtest.ULEB(uint32(len(body))), body...)
	memory0 := append([]byte{0x00}, wasmtest.ULEB(uint32(1))...)
	memory1 := append([]byte{0x00}, wasmtest.ULEB(uint32(2))...)
	b := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(
			[]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32},
		))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec(memory0, memory1)),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(entry)),
	)
	m, err := wasm.DecodeModule(b)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	return m
}

func TestConstBulkChecksIndexedMemorySizeArm64(t *testing.T) {
	const (
		dst       = 1 << 16
		fillValue = 0x5a
		fillLen   = 5
	)
	m := multiMemoryFillModuleArm64(t)
	var stats ModuleStats
	cm, err := CompileModuleWith(m, CompileOptions{Stats: &stats})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if cm.CodeImage != nil {
		defer cm.CodeImage.Close()
	}
	if stats.Funcs[0].Peephole["memfill-unroll"] != 1 {
		t.Fatalf("constant fill fast path did not fire: %v", stats.Funcs[0].Peephole)
	}
	eng, err := runtime.NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close()
	primary, err := runtime.NewJobMemory(1 << 16)
	if err != nil {
		t.Fatal(err)
	}
	defer primary.Close()
	secondary, err := runtime.NewJobMemory(2 << 16)
	if err != nil {
		t.Fatal(err)
	}
	defer secondary.Close()
	ar, err := runtime.NewArena(4096)
	if err != nil {
		t.Fatal(err)
	}
	defer ar.Close()

	dir := ar.Alloc(2 * abi.MemoryDirEntryBytes)
	for i, jm := range []*runtime.JobMemory{primary, secondary} {
		entry := dir[i*abi.MemoryDirEntryBytes:]
		binary.LittleEndian.PutUint64(entry[abi.MemoryDirBaseOffset:], uint64(jm.LinMemBase()))
		binary.LittleEndian.PutUint64(entry[abi.MemoryDirCurrentBytesOffset:], uint64(len(jm.CurrentBytes())))
		binary.LittleEndian.PutUint32(entry[abi.MemoryDirCurrentPagesOffset:], jm.CurrentPages())
	}
	primary.SetMemoryDirPtr(uintptr(unsafe.Pointer(&dir[0])))

	code, entry, err := runtime.MapCode(cm.Code)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Unmap(code)
	serArgs := ar.Alloc(16)
	binary.LittleEndian.PutUint64(serArgs, dst)
	binary.LittleEndian.PutUint64(serArgs[8:], fillValue)
	err = eng.Call(entry+uintptr(cm.Entry[0]), serArgs, primary.LinearMemory(), ar.Alloc(runtime.TrapBufferBytes), ar.Alloc(16))
	if err != nil {
		t.Fatalf("in-bounds fill in memory 1 trapped against memory 0 size: %v", err)
	}
	for i := dst; i < dst+fillLen; i++ {
		if got := secondary.CurrentBytes()[i]; got != fillValue {
			t.Fatalf("memory 1 byte %d = %#x, want %#x", i, got, fillValue)
		}
	}
}
