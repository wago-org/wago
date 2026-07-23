//go:build linux && amd64

package amd64

import (
	"encoding/binary"
	"math"
	"strings"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/testutil/wasmtest"
)

func indirectTailModule(t *testing.T) *wasm.Module {
	t.Helper()
	i32x2 := []wasm.ValType{wasm.I32, wasm.I32}
	elem := []byte{0x00, 0x41, 0x00, 0x0b, 0x02, 0x00, 0x01}
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(i32x2, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType(i32x2, []wasm.ValType{wasm.I64}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1))),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x00, 0x02})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))),
		wasmtest.Section(9, wasmtest.Vec(elem)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{
				0x20, 0x00, 0x45, 0x04, 0x7f, 0x41, 0x07, 0x05,
				0x20, 0x00, 0x41, 0x01, 0x6b, // n-1 (callee arg 0)
				0x20, 0x01, // selector (callee arg 1)
				0x20, 0x01, // selector (table index)
				0x13, 0x00, 0x00, // return_call_indirect type 0 table 0
				0x0b, 0x0b,
			}),
			wasmtest.Code([]byte{0x42, 0x09, 0x0b}),
		)),
	)
	m, err := wasm.DecodeModule(mod)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func runIndirectTail(t *testing.T, m *wasm.Module, tableFuncs []int, args ...uint64) ([]byte, error) {
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

	table := arena.Alloc(8 + len(tableFuncs)*coreruntime.TableEntryBytes)
	binary.LittleEndian.PutUint32(table, uint32(len(tableFuncs)))
	binary.LittleEndian.PutUint32(table[4:], uint32(len(tableFuncs)))
	for i, fidx := range tableFuncs {
		if fidx < 0 {
			continue
		}
		off := 8 + i*coreruntime.TableEntryBytes
		binary.LittleEndian.PutUint64(table[off+coreruntime.TableEntryCodePtrOffset:], uint64(base)+uint64(cm.InternalEntry[fidx]))
		typeIdx := m.FuncTypes[fidx].Index
		binary.LittleEndian.PutUint64(table[off+coreruntime.TableEntrySigKeyOffset:], m.StructuralTypeKey(typeIdx))
		binary.LittleEndian.PutUint64(table[off+coreruntime.TableEntryHomeLinMemOffset:], uint64(jm.LinMemBase())|uint64(1)<<63)
	}
	jm.SetTablePtr(uintptr(unsafe.Pointer(&table[0])))

	argBuf := arena.Alloc(128)
	resultBuf := arena.Alloc(128)
	trap := arena.Alloc(8)
	for i, arg := range args {
		binary.LittleEndian.PutUint64(argBuf[i*8:], arg)
	}
	err = eng.Call(base+uintptr(cm.Entry[0]), argBuf, jm.LinearMemory(), trap, resultBuf)
	return append([]byte(nil), resultBuf[:16]...), err
}

func TestReturnCallIndirectReusesFrameAndMatchesTraps(t *testing.T) {
	m := indirectTailModule(t)
	out, err := runIndirectTail(t, m, []int{0, 1}, 1_000_000, 0)
	if err != nil {
		t.Fatalf("million-deep indirect tail recursion trapped: %v", err)
	}
	if got := binary.LittleEndian.Uint32(out); got != 7 {
		t.Fatalf("result = %d, want 7", got)
	}

	for _, tc := range []struct {
		name    string
		entries []int
		index   uint64
		want    string
	}{
		{name: "out of bounds", entries: []int{0, 1}, index: 2, want: "indirect call out of bounds"},
		{name: "null", entries: []int{0, -1}, index: 1, want: "indirect call out of bounds"},
		{name: "signature", entries: []int{0, 1}, index: 1, want: "indirect call with wrong signature"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := runIndirectTail(t, m, tc.entries, 1, tc.index)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("trap = %v, want %q", err, tc.want)
			}
		})
	}

	var stats ModuleStats
	if _, err := CompileModuleWith(m, CompileOptions{Stats: &stats}); err != nil {
		t.Fatal(err)
	}
	if got := stats.Funcs[0].Calls["tail-indirect"]; got != 1 {
		t.Fatalf("tail-indirect count = %d, want 1", got)
	}
}

func TestReturnCallIndirectStagesMixedRegisterBanks(t *testing.T) {
	// (func (param i32 f64 i32) (result f64)
	//   local.get 0; i32.eqz
	//   if (result f64) local.get 1
	//   else
	//     local.get 0; i32.const 1; i32.sub
	//     local.get 1
	//     local.get 2
	//     local.get 2
	//     return_call_indirect (type 0) (table 0)
	//   end)
	params := []wasm.ValType{wasm.I32, wasm.F64, wasm.I32}
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(params, []wasm.ValType{wasm.F64}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x00, 0x01})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))),
		wasmtest.Section(9, wasmtest.Vec([]byte{0x00, 0x41, 0x00, 0x0b, 0x01, 0x00})),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
			0x20, 0x00, 0x45, 0x04, 0x7c, 0x20, 0x01, 0x05,
			0x20, 0x00, 0x41, 0x01, 0x6b,
			0x20, 0x01,
			0x20, 0x02,
			0x20, 0x02,
			0x13, 0x00, 0x00,
			0x0b, 0x0b,
		}))),
	)
	m, err := wasm.DecodeModule(mod)
	if err != nil {
		t.Fatal(err)
	}
	want := math.Float64bits(6.25)
	out, err := runIndirectTail(t, m, []int{0}, 1_000_000, want, 0)
	if err != nil {
		t.Fatalf("million-deep mixed indirect tail recursion trapped: %v", err)
	}
	if got := binary.LittleEndian.Uint64(out); got != want {
		t.Fatalf("f64 bits = %#x, want %#x", got, want)
	}
}

func TestReturnCallIndirectMilestoneRejectsExternallyMutableTable(t *testing.T) {
	m := indirectTailModule(t)
	m.Exports = append(m.Exports, wasm.Export{Name: "table", Index: wasm.ExternIdx{Kind: wasm.ExternTable, Index: 0}})
	if _, err := CompileModule(m); err == nil || !strings.Contains(err.Error(), "not a private immutable local funcref table") {
		t.Fatalf("compile error = %v", err)
	}
}
