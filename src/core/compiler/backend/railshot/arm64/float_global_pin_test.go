//go:build arm64

package arm64

import (
	"bytes"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	encoder "github.com/wago-org/wago/src/core/encoder/arm64"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func TestFloatGlobalValuePinSelection(t *testing.T) {
	requireCompilerDiagnostics(t)
	for _, wide := range []bool{false, true} {
		for _, regABI := range []bool{false, true} {
			constant, add, reinterpret, result := byte(0x43), byte(0x92), byte(0xbc), wasm.I32
			zero := []byte{0, 0, 0, 0}
			one := []byte{0, 0, 128, 63}
			if wide {
				constant, add, reinterpret, result = 0x44, 0xa0, 0xbd, wasm.I64
				zero = make([]byte, 8)
				one = []byte{0, 0, 0, 0, 0, 0, 240, 63}
			}
			body := []byte{1, 1, 0x7f, 0x03, 0x40, 0x23, 0, constant}
			body = append(body, one...)
			body = append(body, add, 0x24, 0, 0x20, 1, 0x41, 1, 0x6a, 0x21, 1, 0x20, 1, 0x20, 0, 0x49, 0x0d, 0, 0x0b, 0x23, 0, reinterpret, 0x0b)
			global := []byte{byte(0x7d), 1, constant}
			if wide {
				global[0] = 0x7c
			}
			global = append(global, zero...)
			global = append(global, 0x0b)
			m, err := wasm.DecodeModule(wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{result}))), wasmtest.Section(3, wasmtest.Vec([]byte{0})), wasmtest.Section(6, wasmtest.Vec(global)), wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...)))))
			if err != nil {
				t.Fatal(err)
			}
			var stats ModuleStats
			c, err := CompileModuleWith(m, CompileOptions{Stats: &stats, Optimizations: map[string]bool{"reg-abi": regABI}})
			if err != nil {
				t.Fatal(err)
			}
			c.CodeImage.Close()
			want := 0
			if regABI {
				want = 1
			}
			if stats.Funcs[0].PinnedGlobalsValue != want {
				t.Fatalf("wide=%t regabi=%t pinned=%d want=%d", wide, regABI, stats.Funcs[0].PinnedGlobalsValue, want)
			}
		}
	}
}

func TestFloatGlobalValuePinRejectsImportedAndCallingFunctions(t *testing.T) {
	requireCompilerDiagnostics(t)
	for _, calling := range []bool{false, true} {
		body := []byte{1, 1, 0x7f, 0x03, 0x40}
		if calling {
			body = append(body, 0x10, 0)
		}
		body = append(body, 0x23, 0, 0x43, 0, 0, 128, 63, 0x92, 0x24, 0, 0x20, 1, 0x41, 1, 0x6a, 0x21, 1, 0x20, 1, 0x20, 0, 0x49, 0x0d, 0, 0x0b, 0x23, 0, 0xbc, 0x0b)
		types := wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}), wasmtest.FuncType(nil, nil)))
		imported := append(wasmtest.Name("env"), wasmtest.Name("value")...)
		var globals []byte
		if calling {
			imported = append(imported, 0, 1)
			globals = wasmtest.Section(6, wasmtest.Vec([]byte{0x7d, 1, 0x43, 0, 0, 0, 0, 0x0b}))
		} else {
			imported = append(imported, 3, 0x7d, 1)
		}
		sections := [][]byte{types, wasmtest.Section(2, wasmtest.Vec(imported)), wasmtest.Section(3, wasmtest.Vec([]byte{0}))}
		if calling {
			sections = append(sections, globals)
		}
		sections = append(sections, wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))))
		m, err := wasm.DecodeModule(wasmtest.Module(sections...))
		if err != nil {
			t.Fatal(err)
		}
		var stats ModuleStats
		c, err := CompileModuleWith(m, CompileOptions{Stats: &stats})
		if err != nil {
			t.Fatal(err)
		}
		c.CodeImage.Close()
		if stats.Funcs[0].Peephole["float-global-value-pin"] != 0 {
			t.Fatalf("calling=%t selected unsafe float pin", calling)
		}
	}
}

func TestFloatGlobalPinTrapStoresAndEntryReloads(t *testing.T) {
	for _, wide := range []bool{false, true} {
		for _, shared := range []bool{false, true} {
			typ := wasm.F32
			if wide {
				typ = wasm.F64
			}
			f := &fn{a: &encoder.Asm{}, sc: &scratch{}, m: &wasm.Module{Globals: []wasm.Global{{Type: wasm.GlobalType{Type: typ, Mutable: true}}}}, globalReg: []Reg{Reg(9) | globalRegFloat | globalRegDirty}}
			f.trapAlways(trapInterrupted)
			f.entryTrapEnd = f.a.Len()
			f.prepareEntryTrapPins()
			entryCode := append([]byte(nil), f.a.B...)
			found := false
			for _, cell := range gpAlloc {
				expected := &encoder.Asm{}
				expected.LdrF(Reg(9), cell, 0, wide)
				found = found || bytes.Contains(entryCode, expected.B)
			}
			if !found {
				t.Fatalf("wide=%t: entry trap does not reload the FP value pin", wide)
			}
			start := f.a.Len()
			if shared {
				f.emitSharedTrapStubs()
			} else {
				f.emitTrapStubs()
			}
			expected := &encoder.Asm{}
			if wide {
				expected.StrD(X16, 0, Reg(9))
			} else {
				expected.StrS(X16, 0, Reg(9))
			}
			if !bytes.Contains(f.a.B[start:], expected.B) {
				t.Fatalf("wide=%t shared=%t: trap does not store FP value pin", wide, shared)
			}
		}
	}
}
