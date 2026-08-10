//go:build amd64

package amd64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

func deadGCConstructorTreeModule(t *testing.T) *wasm.Module {
	t.Helper()
	arrayType := []byte{0x5e, 0x7f, 0x01} // (array (mut i32))
	structType := []byte{0x5f}
	structType = append(structType, wasmtest.Vec(
		[]byte{0x63, 0x00, 0x00}, // immutable (ref null 0)
		[]byte{0x7f, 0x00},       // immutable i32
	)...)
	body := []byte{
		0x41, 0x2a, // hidden function result: 42
		0x41, 0x01, 0x41, 0x02,
		0xfb, 0x08, 0x00, 0x02, // array.new_fixed type 0, two elements
		0x41, 0x16,
		0xfb, 0x00, 0x01, // struct.new type 1
		0x1a, // drop the complete constructor tree
		0x0b,
	}
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			arrayType,
			structType,
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(2))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
	m, err := wasm.DecodeModule(data)
	if err != nil {
		t.Fatalf("decode dead constructor tree: %v", err)
	}
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatalf("validate dead constructor tree: %v", err)
	}
	return m
}

func checkedDeadGCArrayModule(t *testing.T, uniform, nested bool) *wasm.Module {
	t.Helper()
	arrayType := []byte{0x5e, 0x7f, 0x01} // (array (mut i32))
	types := [][]byte{arrayType}
	params := []wasm.ValType{wasm.I32}
	var body []byte
	if uniform {
		params = []wasm.ValType{wasm.I32, wasm.I32}
		body = append(body, 0x20, 0x00, 0x20, 0x01, 0xfb, 0x06, 0x00)
	} else {
		body = append(body, 0x20, 0x00, 0xfb, 0x07, 0x00)
	}
	if nested {
		structType := []byte{0x5f}
		structType = append(structType, wasmtest.Vec(
			[]byte{0x63, 0x00, 0x00}, // immutable (ref null 0)
			[]byte{0x7f, 0x00},       // immutable i32
		)...)
		types = append(types, structType)
		body = append(body, 0x41, 0x09, 0xfb, 0x00, 0x01, 0x1a)
	} else {
		body = append(body, 0x1a)
	}
	body = append(body, 0x41, 0x07, 0x0b)
	types = append(types, wasmtest.FuncType(params, []wasm.ValType{wasm.I32}))
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(types...)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(uint32(len(types)-1)))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
	m, err := wasm.DecodeModule(data)
	if err != nil {
		t.Fatalf("decode checked dead array: %v", err)
	}
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatalf("validate checked dead array: %v", err)
	}
	return m
}

func checkedDeadGCSegmentArrayModule(t *testing.T, elem bool) *wasm.Module {
	t.Helper()
	arrayType := []byte{0x5e, 0x78, 0x00} // immutable i8 array
	op := byte(0x09)
	segments := []byte(nil)
	segmentSection := byte(11)
	if elem {
		arrayType = []byte{0x5e, 0x6c, 0x00} // immutable i31ref array
		op = 0x0a
		segmentSection = 9
		expr := []byte{0xd0, 0x6c, 0x0b}
		entry := append([]byte{0x05, 0x6c}, wasmtest.Vec(expr)...)
		segments = wasmtest.Vec(entry)
	} else {
		entry := append([]byte{0x01}, wasmtest.ULEB(4)...)
		entry = append(entry, 1, 2, 3, 4)
		segments = wasmtest.Vec(entry)
	}
	body := []byte{
		0x20, 0x00, 0x20, 0x01, 0xfb, op, 0x00, 0x00, 0x1a,
		0x41, 0x07, 0x0b,
	}
	sections := [][]byte{
		wasmtest.Section(1, wasmtest.Vec(arrayType, wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
	}
	if elem {
		sections = append(sections, wasmtest.Section(segmentSection, segments))
	} else {
		sections = append(sections, wasmtest.Section(12, wasmtest.ULEB(1)))
	}
	sections = append(sections, wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))))
	if !elem {
		sections = append(sections, wasmtest.Section(segmentSection, segments))
	}
	data := wasmtest.Module(sections...)
	m, err := wasm.DecodeModule(data)
	if err != nil {
		t.Fatalf("decode checked dead segment array: %v", err)
	}
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatalf("validate checked dead segment array: %v", err)
	}
	return m
}

func TestCheckedDeadGCArrayConstructorsPreservePreflight(t *testing.T) {
	saved := deadGCNewEnabled
	defer func() { deadGCNewEnabled = saved }()
	for _, tc := range []struct {
		name            string
		uniform, nested bool
		segment         int
		wantOnCalls     int
		wantOffCalls    int
		wantDead        int
	}{
		{name: "default", wantOnCalls: 1, wantOffCalls: 1, wantDead: 1},
		{name: "uniform", uniform: true, wantOnCalls: 1, wantOffCalls: 1, wantDead: 1},
		{name: "nested-default", nested: true, wantOnCalls: 1, wantOffCalls: 2, wantDead: 2},
		{name: "data", segment: 1, wantOnCalls: 1, wantOffCalls: 1, wantDead: 1},
		{name: "elem", segment: 2, wantOnCalls: 1, wantOffCalls: 1, wantDead: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			compile := func(enabled bool) *CodegenStats {
				deadGCNewEnabled = enabled
				var stats ModuleStats
				m := checkedDeadGCArrayModule(t, tc.uniform, tc.nested)
				if tc.segment != 0 {
					m = checkedDeadGCSegmentArrayModule(t, tc.segment == 2)
				}
				if _, err := CompileModuleWith(m, CompileOptions{
					GCStructHelpers: true, GCArrayHelpers: true, Stats: &stats,
				}); err != nil {
					t.Fatal(err)
				}
				return stats.Funcs[0]
			}
			on, off := compile(true), compile(false)
			if got := on.Peephole["gc-dead-new"]; got != tc.wantDead {
				t.Fatalf("enabled gc-dead-new = %d, want %d (all: %v)", got, tc.wantDead, on.Peephole)
			}
			if got := on.Peephole["gc-dead-new-checked"]; got != 1 {
				t.Fatalf("enabled gc-dead-new-checked = %d, want 1", got)
			}
			if got := on.Calls[callKindHostSync]; got != tc.wantOnCalls {
				t.Fatalf("enabled helper calls = %d, want %d", got, tc.wantOnCalls)
			}
			if got := off.Calls[callKindHostSync]; got != tc.wantOffCalls {
				t.Fatalf("disabled helper calls = %d, want %d", got, tc.wantOffCalls)
			}
			if on.GCCodeBytes.Allocation == 0 || on.GCCodeBytes.Allocation >= off.GCCodeBytes.Allocation {
				t.Fatalf("constructor-family bytes enabled/disabled = %d/%d, want a nonzero reduction", on.GCCodeBytes.Allocation, off.GCCodeBytes.Allocation)
			}
			t.Logf("checked dead %s constructor-family bytes: enabled=%d disabled=%d", tc.name, on.GCCodeBytes.Allocation, off.GCCodeBytes.Allocation)
		})
	}
}

func TestDeadGCConstructorDeepStructArrayTreeElimination(t *testing.T) {
	innerArray := []byte{0x5e, 0x7f, 0x01}
	outerArray := []byte{0x5e, 0x63, 0x00, 0x00} // immutable (ref null 0) array
	wrapper := []byte{0x5f}
	wrapper = append(wrapper, wasmtest.Vec([]byte{0x63, 0x01, 0x00})...)
	body := []byte{
		0x41, 0x01, 0xfb, 0x08, 0x00, 0x01,
		0xfb, 0x08, 0x01, 0x01,
		0xfb, 0x00, 0x02, 0x1a,
		0x41, 0x07, 0x0b,
	}
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(innerArray, outerArray, wrapper, wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(3))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
	m, err := wasm.DecodeModule(data)
	if err != nil {
		t.Fatal(err)
	}
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	var stats ModuleStats
	if _, err := CompileModuleWith(m, CompileOptions{GCStructHelpers: true, GCArrayHelpers: true, Stats: &stats}); err != nil {
		t.Fatal(err)
	}
	got := stats.Funcs[0]
	if got.Peephole["gc-dead-new"] != 3 || got.Calls[callKindHostSync] != 0 {
		t.Fatalf("deep constructor tree stats = dead %d, calls %d (all: %v)", got.Peephole["gc-dead-new"], got.Calls[callKindHostSync], got.Peephole)
	}
}

func TestDeadGCConstructorTreeElimination(t *testing.T) {
	m := deadGCConstructorTreeModule(t)
	compile := func() *CodegenStats {
		var stats ModuleStats
		if _, err := CompileModuleWith(m, CompileOptions{
			GCStructHelpers: true,
			GCArrayHelpers:  true,
			Stats:           &stats,
		}); err != nil {
			t.Fatalf("compile dead constructor tree: %v", err)
		}
		return stats.Funcs[0]
	}

	saved := deadGCNewEnabled
	defer func() { deadGCNewEnabled = saved }()
	deadGCNewEnabled = true
	on := compile()
	if got := on.Peephole["gc-dead-new"]; got != 2 {
		t.Fatalf("gc-dead-new = %d, want 2 (all: %v)", got, on.Peephole)
	}
	if got := on.Calls[callKindHostSync]; got != 0 {
		t.Fatalf("optimized synchronous helper calls = %d, want 0", got)
	}

	deadGCNewEnabled = false
	off := compile()
	if got := off.Peephole["gc-dead-new"]; got != 0 {
		t.Fatalf("disabled gc-dead-new = %d, want 0", got)
	}
	if got := off.Calls[callKindHostSync]; got != 2 {
		t.Fatalf("unoptimized synchronous helper calls = %d, want 2", got)
	}
}
