//go:build arm64

package arm64

import (
	"fmt"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func tailConstantTableFixture(slot byte) []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0, 4})),
		wasmtest.Section(9, wasmtest.Vec([]byte{0, 0x41, 2, 0x0b, 1, 0})),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
			0x20, 0, 0x45, 0x04, 0x7f, 0x20, 1, 0x05,
			0x20, 0, 0x41, 1, 0x6b, 0x20, 1, 0x41, 17, 0x6a, 0x41, slot, 0x13, 0, 0, 0x0b, 0x0b,
		}))),
	)
}

func TestTailImmutableConstantSlotNoDispatch(t *testing.T) {
	requireCompilerDiagnostics(t)
	m, err := wasm.DecodeModule(tailConstantTableFixture(2))
	if err != nil {
		t.Fatal(err)
	}
	var stats ModuleStats
	c, err := CompileModuleWith(m, CompileOptions{Stats: &stats})
	if err != nil {
		t.Fatal(err)
	}
	defer c.CodeImage.Close()
	want := 1
	if profileEnabled {
		want = 0
	}
	if got := stats.Funcs[0].Peephole["tail-constant-private-table"]; got != want {
		t.Fatalf("constant immutable tail target selections = %d, want %d", got, want)
	}
}

func TestTailImmutableConstantSlotKeepsUnprovedChecks(t *testing.T) {
	requireCompilerDiagnostics(t)
	for _, tc := range []struct {
		name   string
		slot   byte
		mutate func(*wasm.Module)
		opts   map[string]bool
	}{
		{name: "null", slot: 1}, {name: "OOB", slot: 4},
		{name: "exported", slot: 2, mutate: func(m *wasm.Module) {
			m.Exports = append(m.Exports, wasm.Export{Name: "t", Index: wasm.ExternIdx{Kind: wasm.ExternTable, Index: 0}})
		}},
		{name: "imported", slot: 2, mutate: func(m *wasm.Module) {
			m.Imports = append(m.Imports, wasm.Import{Module: "env", Name: "t", Type: wasm.NewTableExternType(m.Tables[0].Type)})
			m.Tables = nil
		}},
		{name: "mutated", slot: 2, mutate: func(m *wasm.Module) {
			m.FuncTypes = append(m.FuncTypes, m.FuncTypes[0])
			m.Code = append(m.Code, wasm.Func{BodyBytes: []byte{0x41, 2, 0xd0, 0x70, 0x26, 0, 0x41, 0, 0x0b}})
		}},
		{name: "table-proof-off", slot: 2, opts: map[string]bool{"immutable-table": false}},
		{name: "type-proof-off", slot: 2, opts: map[string]bool{"immutable-table-type": false}},
		{name: "loop-off", slot: 2, opts: map[string]bool{"tail-self-loop": false}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, err := wasm.DecodeModule(tailConstantTableFixture(tc.slot))
			if err != nil {
				t.Fatal(err)
			}
			if tc.mutate != nil {
				tc.mutate(m)
			}
			var stats ModuleStats
			c, err := CompileModuleWith(m, CompileOptions{Stats: &stats, Optimizations: tc.opts})
			if err != nil {
				t.Fatal(err)
			}
			defer c.CodeImage.Close()
			if got := stats.Funcs[0].Peephole["tail-constant-private-table"]; got != 0 {
				t.Fatalf("unproved selection=%d", got)
			}
		})
	}
}

func TestTailTableConstantOffsetSyntax(t *testing.T) {
	for _, tc := range []struct {
		name  string
		e     wasm.Expr
		value uint32
		ok    bool
	}{
		{"byte-backed", wasm.Expr{BodyBytes: []byte{0x41, 2, 0x0b}}, 2, true},
		{"unsigned-offset", wasm.Expr{BodyBytes: []byte{0x41, 0x7e, 0x0b}}, 0xfffffffe, true},
		{"constructed", wasm.Expr{Instrs: []wasm.Instruction{{Kind: wasm.InstrI32Const, I32: 2}}}, 2, true},
		{"global", wasm.Expr{BodyBytes: []byte{0x23, 0, 0x0b}}, 0, false},
		{"extended", wasm.Expr{BodyBytes: []byte{0x41, 1, 0x41, 1, 0x6a, 0x0b}}, 0, false},
		{"wide", wasm.Expr{BodyBytes: []byte{0x42, 2, 0x0b}}, 0, false},
		{"trailing", wasm.Expr{BodyBytes: []byte{0x41, 2, 0x0b, 1}}, 0, false},
		{"truncated", wasm.Expr{BodyBytes: []byte{0x41, 2}}, 0, false},
		{"empty", wasm.Expr{}, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v, ok := tailTableConstantOffset(tc.e)
			if ok != tc.ok || ok && v != tc.value {
				t.Fatalf("offset=%d,%t want=%d,%t", v, ok, tc.value, tc.ok)
			}
		})
	}
}

func TestTailImmutableConstantSlotInventoryLimit(t *testing.T) {
	requireCompilerDiagnostics(t)
	for _, count := range []int{2, 64, 65} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			m, err := wasm.DecodeModule(tailConstantTableFixture(2))
			if err != nil {
				t.Fatal(err)
			}
			e := m.Elements[0]
			for len(m.Elements) < count {
				m.Elements = append(m.Elements, e)
			}
			var stats ModuleStats
			c, err := CompileModuleWith(m, CompileOptions{Stats: &stats})
			if err != nil {
				t.Fatal(err)
			}
			defer c.CodeImage.Close()
			want := 0
			if count <= 64 && !profileEnabled {
				want = 1
			}
			if got := stats.Funcs[0].Peephole["tail-constant-private-table"]; got != want {
				t.Fatalf("selection=%d want=%d", got, want)
			}
		})
	}
}
