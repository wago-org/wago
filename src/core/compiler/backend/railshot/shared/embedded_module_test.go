package shared

import (
	"bytes"
	"encoding/binary"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime/embedded32"
	"github.com/wago-org/wago/testutil/wasmtest"
)

func embeddedTestModule(t *testing.T, types, funcs, code [][]byte, extra ...[]byte) *wasm.Module {
	t.Helper()
	sections := [][]byte{
		wasmtest.Section(1, wasmtest.Vec(types...)),
		wasmtest.Section(3, wasmtest.Vec(funcs...)),
	}
	sections = append(sections, extra...)
	sections = append(sections, wasmtest.Section(10, wasmtest.Vec(code...)))
	m, err := wasm.DecodeModule(wasmtest.Module(sections...))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestCompileEmbeddedI32ModuleLayoutAndPreflight(t *testing.T) {
	m := embeddedTestModule(t,
		[][]byte{wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}), wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32})},
		[][]byte{{0}, {1}},
		[][]byte{wasmtest.Code([]byte{0x41, 0x2a, 0x0b}), wasmtest.Code([]byte{0x20, 0x00, 0x41, 0x01, 0x6a, 0x0b})},
	)
	var bodies [][]byte
	fake := func(params int, body []byte) ([]byte, error) {
		bodies = append(bodies, append([]byte(nil), body...))
		return bytes.Repeat([]byte{0xaa}, 4+params*4), nil
	}
	cm, err := CompileEmbeddedI32Module(m, EmbeddedModuleOptions{}, "test32", 4, 8, []byte{0, 0, 0, 0}, fake)
	if err != nil {
		t.Fatal(err)
	}
	if len(cm.Entry) != 2 || cm.Entry[0] != 0 || cm.Entry[1] != 16 {
		t.Fatalf("entry=%v", cm.Entry)
	}
	if len(cm.Functions) != 2 || cm.Functions[1].Offset != 16 || cm.Functions[1].Size != 8 {
		t.Fatalf("metadata=%+v", cm.Functions)
	}
	if len(bodies) != 2 || !bytes.Equal(bodies[0], []byte{0, 0x41, 0x2a, 0x0b}) {
		t.Fatalf("reconstructed bodies=%x", bodies)
	}
	if cm.RequiredCodeBytes <= uint32(len(cm.Code)) {
		t.Fatalf("required=%d code=%d", cm.RequiredCodeBytes, len(cm.Code))
	}
	_, err = CompileEmbeddedI32Module(m, EmbeddedModuleOptions{CodeCapacity: cm.RequiredCodeBytes - 1}, "test32", 4, 8, []byte{0, 0, 0, 0}, fake)
	if err == nil || !strings.Contains(err.Error(), "preflight requirement") {
		t.Fatalf("capacity error=%v", err)
	}
}

func TestCompileEmbeddedModuleInitializesSerializedWideGlobals(t *testing.T) {
	f32 := make([]byte, 6)
	f32[0] = 0x43
	binary.LittleEndian.PutUint32(f32[1:], 0x11223344)
	f32[5] = 0x0b
	f64 := make([]byte, 10)
	f64[0] = 0x44
	binary.LittleEndian.PutUint64(f64[1:], 0x8877665544332211)
	f64[9] = 0x0b
	v128 := append([]byte{0xfd, 0x0c}, []byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15}...)
	v128 = append(v128, 0x0b)
	i64 := append([]byte{0x42}, wasmtest.SLEB64(0x112233445566778)...)
	i64 = append(i64, 0x0b)
	m, err := wasm.DecodeModule(wasmtest.Module(
		wasmtest.Section(6, wasmtest.Vec(
			wasmtest.GlobalEntry(wasm.I32, false, []byte{0x41, 7, 0x0b}),
			wasmtest.GlobalEntry(wasm.I64, true, i64),
			wasmtest.GlobalEntry(wasm.F32, false, f32),
			wasmtest.GlobalEntry(wasm.F64, true, f64),
			wasmtest.GlobalEntry(wasm.V128, false, v128),
		)),
	))
	if err != nil {
		t.Fatal(err)
	}
	cm, err := CompileEmbeddedModule(m, EmbeddedModuleOptions{}, "test", 1, []byte{0}, func(int, *wasm.CompType, []wasm.LocalRun, []byte) ([]byte, error) {
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	cells := make([]uint32, 10)
	if err := cm.InstantiateGlobals(cells); err != nil {
		t.Fatal(err)
	}
	want := []uint32{7, 0x45566778, 0x01122334, 0x11223344, 0x44332211, 0x88776655, 0x03020100, 0x07060504, 0x0b0a0908, 0x0f0e0d0c}
	if !slices.Equal(cells, want) {
		t.Fatalf("global cells = %#v, want %#v", cells, want)
	}
	for i, slot := range []uint32{0, 1, 3, 4, 6} {
		if cm.Globals[i].Slot != slot {
			t.Fatalf("global %d slot = %d, want %d", i, cm.Globals[i].Slot, slot)
		}
	}
}

func TestCompileEmbeddedModuleBindsImportedGlobalsAndInitializers(t *testing.T) {
	m, err := wasm.DecodeModule(wasmtest.Module(
		wasmtest.Section(2, wasmtest.Vec(
			wasmtest.GlobalImportEntry("env", "wide", wasm.I64, false),
			wasmtest.GlobalImportEntry("env", "word", wasm.I32, true),
		)),
		wasmtest.Section(6, wasmtest.Vec(
			wasmtest.GlobalEntry(wasm.I64, false, []byte{0x23, 0, 0x0b}),
			wasmtest.GlobalEntry(wasm.I32, true, []byte{0x41, 7, 0x0b}),
		)),
	))
	if err != nil {
		t.Fatal(err)
	}
	cm, err := CompileEmbeddedModule(m, EmbeddedModuleOptions{}, "test", 1, []byte{0}, func(int, *wasm.CompType, []wasm.LocalRun, []byte) ([]byte, error) {
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(cm.ImportedGlobals) != 2 || cm.ImportedGlobals[0].Type != wasm.I64 || cm.ImportedGlobals[1].Type != wasm.I32 || !cm.ImportedGlobals[1].Mutable {
		t.Fatalf("imported globals=%+v", cm.ImportedGlobals)
	}
	if len(cm.Globals) != 2 || !cm.Globals[0].HasInitGlobal || cm.Globals[0].InitGlobal != 0 || cm.Globals[0].Slot != 0 || cm.Globals[1].Slot != 2 {
		t.Fatalf("local globals=%+v", cm.Globals)
	}
	cells := []uint32{9, 9, 9}
	if err := cm.InstantiateGlobals(cells); err == nil || !slices.Equal(cells, []uint32{9, 9, 9}) {
		t.Fatalf("missing import cells error=%v cells=%v", err, cells)
	}
	if err := cm.InstantiateGlobalsWithImports(cells, [][]uint32{{0x55667788, 0x11223344}, {5}}); err != nil {
		t.Fatal(err)
	}
	want := []uint32{0x55667788, 0x11223344, 7}
	if !slices.Equal(cells, want) {
		t.Fatalf("global cells=%#v want=%#v", cells, want)
	}
}

func TestCompileEmbeddedModuleRetainsImportContracts(t *testing.T) {
	functionImport := append(wasmtest.Name("host"), wasmtest.Name("call")...)
	functionImport = append(functionImport, 0, 0)
	tableImport := append(wasmtest.Name("host"), wasmtest.Name("table")...)
	tableImport = append(tableImport, 1, 0x70, 1, 1, 3)
	memoryImport := append(wasmtest.Name("host"), wasmtest.Name("memory")...)
	memoryImport = append(memoryImport, 2, 1, 1, 2)
	m, err := wasm.DecodeModule(wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I64}, []wasm.ValType{wasm.F64}))),
		wasmtest.Section(2, wasmtest.Vec(
			functionImport,
			tableImport,
			memoryImport,
			wasmtest.GlobalImportEntry("host", "global", wasm.I64, true),
		)),
	))
	if err != nil {
		t.Fatal(err)
	}
	cm, err := CompileEmbeddedModule(m, EmbeddedModuleOptions{}, "test", 1, []byte{0}, func(int, *wasm.CompType, []wasm.LocalRun, []byte) ([]byte, error) {
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(cm.Imports) != 4 {
		t.Fatalf("imports=%+v", cm.Imports)
	}
	function := cm.Imports[0]
	if function.Module != "host" || function.Name != "call" || function.Kind != wasm.ExternFunc || function.Index != 0 || !slices.Equal(function.Params, []wasm.ValType{wasm.I32, wasm.I64}) || !slices.Equal(function.Results, []wasm.ValType{wasm.F64}) {
		t.Fatalf("function import=%+v", function)
	}
	table := cm.Imports[1]
	if table.Kind != wasm.ExternTable || table.Index != 0 || table.Reference != wasm.FuncRef.Ref || table.Minimum != 1 || !table.HasMaximum || table.Maximum != 3 {
		t.Fatalf("table import=%+v", table)
	}
	memory := cm.Imports[2]
	if memory.Kind != wasm.ExternMem || memory.Index != 0 || memory.Minimum != 1 || !memory.HasMaximum || memory.Maximum != 2 {
		t.Fatalf("memory import=%+v", memory)
	}
	global := cm.Imports[3]
	if global.Kind != wasm.ExternGlobal || global.Index != 0 || global.Type != wasm.I64 || !global.Mutable {
		t.Fatalf("global import=%+v", global)
	}
}

func TestCompileEmbeddedModuleInitializesActiveFunctionTable(t *testing.T) {
	m := embeddedTestModule(t,
		[][]byte{wasmtest.FuncType(nil, nil)},
		[][]byte{{0}},
		[][]byte{wasmtest.Code([]byte{0x0b})},
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 1, 3, 5})),
		wasmtest.Section(9, wasmtest.Vec([]byte{0, 0x41, 1, 0x0b, 1, 0})),
	)
	cm, err := CompileEmbeddedModule(m, EmbeddedModuleOptions{}, "test", 1, []byte{0}, func(int, *wasm.CompType, []wasm.LocalRun, []byte) ([]byte, error) {
		return []byte{0}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if cm.Table == nil || cm.Table.Minimum != 3 || !cm.Table.HasMaximum || cm.Table.Maximum != 5 {
		t.Fatalf("table=%+v", cm.Table)
	}
	entries := []uint32{9, 9, 9}
	if err := cm.InstantiateTable(entries); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(entries, []uint32{0, 1, 0}) {
		t.Fatalf("entries=%v", entries)
	}
	short := []uint32{7, 7}
	if err := cm.InstantiateTable(short); !errors.Is(err, embedded32.ErrArenaCapacity) || !slices.Equal(short, []uint32{7, 7}) {
		t.Fatalf("short entries=%v err=%v", short, err)
	}
}

func TestEmbeddedModuleRetainsExportsAndStart(t *testing.T) {
	m := embeddedTestModule(t,
		[][]byte{wasmtest.FuncType(nil, nil)},
		[][]byte{{0}},
		[][]byte{wasmtest.Code([]byte{0x0b})},
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 0))),
		wasmtest.Section(8, wasmtest.ULEB(0)),
	)
	cm, err := CompileEmbeddedModule(m, EmbeddedModuleOptions{}, "test32", 8, []byte{0, 0, 0, 0}, func(int, *wasm.CompType, []wasm.LocalRun, []byte) ([]byte, error) {
		return []byte{0, 0, 0, 0}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if cm.Start == nil || *cm.Start != 0 || len(cm.Exports) != 1 || cm.Exports[0] != (EmbeddedExport{Name: "run", Kind: wasm.ExternFunc, Index: 0}) {
		t.Fatalf("start=%v exports=%+v", cm.Start, cm.Exports)
	}
	published, err := PublishEmbeddedModule(embedded32.NewCodeArena(make([]byte, 32)), cm, nil)
	if err != nil {
		t.Fatal(err)
	}
	if published.Start == nil || *published.Start != 0 || len(published.Exports) != 1 {
		t.Fatalf("published start=%v exports=%+v", published.Start, published.Exports)
	}
}

func TestPublishEmbeddedModuleIsTransactional(t *testing.T) {
	module := &EmbeddedModule{
		Code:      []byte{1, 2, 3, 4},
		Entry:     []int{0},
		Functions: []EmbeddedFunctionMetadata{{FuncIndex: 0, Size: 4}},
	}
	arena := embedded32.NewCodeArena(make([]byte, 64))
	publishErr := errors.New("cache sync")
	if _, err := PublishEmbeddedModule(arena, module, func(uint32, []byte) error { return publishErr }); !errors.Is(err, embedded32.ErrPublish) {
		t.Fatalf("publish error=%v", err)
	}
	if arena.Used() != 0 || arena.Published() != 0 {
		t.Fatalf("failed publish retained used=%d published=%d", arena.Used(), arena.Published())
	}
	published, err := PublishEmbeddedModule(arena, module, func(offset uint32, code []byte) error {
		if offset != 0 || !bytes.Equal(code, module.Code) {
			t.Fatalf("publisher offset=%d code=%x", offset, code)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if published.Block.Offset != 0 || len(published.Entry) != 1 || published.Entry[0] != 0 || published.Functions[0].Offset != 0 {
		t.Fatalf("published=%+v", published)
	}
	if arena.Used() != 4 || arena.Published() != 4 {
		t.Fatalf("successful publish used=%d published=%d", arena.Used(), arena.Published())
	}
}

func TestEmbeddedModuleDataInstantiation(t *testing.T) {
	active := append([]byte{0, 0x41, 4, 0x0b}, wasmtest.ULEB(3)...)
	active = append(active, 'a', 'b', 'c')
	passive := append([]byte{1}, wasmtest.ULEB(3)...)
	passive = append(passive, 'x', 'y', 'z')
	m, err := wasm.DecodeModule(wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{0})),
		wasmtest.Section(5, wasmtest.Vec([]byte{0, 1})),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 0, 0x0b}))),
		wasmtest.Section(11, wasmtest.Vec(active, passive)),
	))
	if err != nil {
		t.Fatal(err)
	}
	cm, err := CompileEmbeddedI32Module(m, EmbeddedModuleOptions{}, "test32", 4, 8, []byte{0, 0, 0, 0}, func(int, []byte) ([]byte, error) { return []byte{0, 0, 0, 0}, nil })
	if err != nil {
		t.Fatal(err)
	}
	memory, _ := embedded32.NewLinearMemory(make([]byte, embedded32.WasmPageSize), 1, 1)
	store, err := cm.InstantiateData(memory)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(memory.Bytes()[4:7]); got != "abc" {
		t.Fatalf("active data=%q", got)
	}
	if trap := store.Init(memory, 1, 8, 0, 3); trap != embedded32.TrapNone || string(memory.Bytes()[8:11]) != "xyz" {
		t.Fatalf("passive init trap=%d bytes=%q", trap, memory.Bytes()[8:11])
	}
	if trap := store.Init(memory, 0, 0, 0, 1); trap != embedded32.TrapMemoryOutOfBounds {
		t.Fatalf("active segment remained available: %d", trap)
	}

	transactional := &EmbeddedModule{Data: []EmbeddedDataSegment{{Offset: 0, Bytes: []byte("ok")}, {Offset: embedded32.WasmPageSize, Bytes: []byte("bad")}}}
	clear(memory.Bytes())
	if _, err := transactional.InstantiateData(memory); err == nil || memory.Bytes()[0] != 0 {
		t.Fatalf("failed active preflight mutated memory: err=%v byte=%d", err, memory.Bytes()[0])
	}
}

func TestCompileEmbeddedI32ModuleReconstructsLocals(t *testing.T) {
	localBody := []byte{1, 1, 0x7f, 0x41, 7, 0x21, 0, 0x20, 0, 0x0b}
	code := append(wasmtest.ULEB(uint32(len(localBody))), localBody...)
	m := embeddedTestModule(t,
		[][]byte{wasmtest.FuncType(nil, []wasm.ValType{wasm.I32})},
		[][]byte{{0}}, [][]byte{code},
	)
	var got []byte
	_, err := CompileEmbeddedI32Module(m, EmbeddedModuleOptions{}, "test32", 4, 8, []byte{0, 0, 0, 0}, func(_ int, body []byte) ([]byte, error) {
		got = append([]byte(nil), body...)
		return []byte{0, 0, 0, 0}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, localBody) {
		t.Fatalf("body=%x want=%x", got, localBody)
	}
}

func TestCompileEmbeddedI32ModuleRejectsIncompatibleModules(t *testing.T) {
	validCode := [][]byte{wasmtest.Code([]byte{0x41, 0, 0x0b})}
	tests := []struct {
		name string
		m    func(*testing.T) *wasm.Module
		want string
	}{
		{"nil", func(*testing.T) *wasm.Module { return nil }, "nil module"},
		{"i64 signature", func(t *testing.T) *wasm.Module {
			return embeddedTestModule(t, [][]byte{wasmtest.FuncType(nil, []wasm.ValType{wasm.I64})}, [][]byte{{0}}, [][]byte{wasmtest.Code([]byte{0x42, 0, 0x0b})})
		}, "result signature"},
		{"externref table", func(t *testing.T) *wasm.Module {
			table := []byte{0x6f, 0, 0}
			return embeddedTestModule(t, [][]byte{wasmtest.FuncType(nil, []wasm.ValType{wasm.I32})}, [][]byte{{0}}, validCode, wasmtest.Section(4, wasmtest.Vec(table)))
		}, "table type"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := CompileEmbeddedI32Module(tc.m(t), EmbeddedModuleOptions{}, "test32", 4, 8, []byte{0, 0, 0, 0}, func(int, []byte) ([]byte, error) { return []byte{0, 0, 0, 0}, nil })
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error=%v want %q", err, tc.want)
			}
		})
	}
}
