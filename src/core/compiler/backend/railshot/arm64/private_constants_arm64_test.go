//go:build (linux || darwin) && arm64

package arm64

import (
	"encoding/binary"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	rt "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func runPrivateGlobalCells(t *testing.T, m *wasm.Module, opts CompileOptions, arg uint64, values ...uint64) (uint64, error) {
	t.Helper()
	cm, err := CompileModuleWith(m, opts)
	if err != nil {
		t.Fatal(err)
	}
	defer cm.CodeImage.Close()
	eng, err := rt.NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close()
	jm, err := rt.NewJobMemory(65536)
	if err != nil {
		t.Fatal(err)
	}
	defer jm.Close()
	clear(jm.CurrentBytes())
	ar, err := rt.NewArena(4096)
	if err != nil {
		t.Fatal(err)
	}
	defer ar.Close()
	code, entry, err := rt.MapCode(cm.Code)
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Unmap(code)
	table := ar.Alloc(8 * len(values))
	for i, v := range values {
		cell := ar.Alloc(8)
		binary.LittleEndian.PutUint64(cell, v)
		binary.LittleEndian.PutUint64(table[i*8:], uint64(uintptr(unsafe.Pointer(&cell[0]))))
	}
	jm.SetGlobalsPtr(uintptr(unsafe.Pointer(&table[0])))
	args, result, trap := ar.Alloc(16), ar.Alloc(16), ar.Alloc(rt.TrapBufferBytes)
	binary.LittleEndian.PutUint64(args, arg)
	err = eng.Call(entry+uintptr(cm.Entry[0]), args, jm.LinearMemory(), trap, result)
	return binary.LittleEndian.Uint64(result), err
}

func TestPrivateConstantsRespectVisibilityAndLocalDefinitions(t *testing.T) {
	old := privateConstantsEnabled
	t.Cleanup(func() { privateConstantsEnabled = old })
	for _, wide := range []bool{false, true} {
		vt, decl, cst, sub, add := wasm.I32, byte(0x7f), byte(0x41), byte(0x6b), byte(0x6a)
		if wide {
			vt, decl, cst, sub, add = wasm.I64, 0x7e, 0x42, 0x7d, 0x7c
		}
		for _, mode := range []string{"stable", "exported", "conditional-definition", "reassigned", "get-before-definition", "global-written"} {
			body := []byte{1, 1, decl}
			if mode == "get-before-definition" {
				body = append(body, 0x20, 1)
			}
			if mode == "global-written" {
				body = append(body, cst, 31, 0x24, 0)
			}
			if mode == "conditional-definition" {
				body = append(body, 0x20, 0, 0x04, 0x40)
			}
			body = append(body, 0x23, 0, cst, 7, sub, 0x21, 1)
			if mode == "conditional-definition" {
				body = append(body, 0x0b)
			}
			body = append(body, 0x02, 0x40, 0x20, 0, 0x04, 0x40)
			if mode == "reassigned" {
				body = append(body, 0x20, 1, cst, 3, add, 0x21, 1)
			}
			body = append(body, 0x0b, 0x0b, 0x20, 1)
			if mode == "get-before-definition" {
				body = append(body, add)
			}
			body = append(body, 0x0b)
			for _, literal := range []int64{0, 17, -1, 0x123456789abcdef} {
				m := modMem(t, 1, []wasm.ValType{wasm.I32}, []wasm.ValType{vt}, body)
				init := wasm.Instruction{Kind: wasm.InstrI32Const, I32: int32(literal)}
				if wide {
					init = wasm.Instruction{Kind: wasm.InstrI64Const, I64: literal}
				}
				m.Globals = []wasm.Global{{Type: wasm.GlobalType{Type: vt, Mutable: true}, Init: wasm.Expr{Instrs: []wasm.Instruction{init}}}}
				if mode == "exported" {
					m.Exports = append(m.Exports, wasm.Export{Name: "value", Index: wasm.ExternIdx{Kind: wasm.ExternGlobal, Index: 0}})
				}
				for _, enabled := range []bool{false, true} {
					privateConstantsEnabled = enabled
					for _, arg := range []uint64{0, 1, 0xfeedface00000000, 0xdeadbeef00000001} {
						cell := uint64(literal)
						if mode == "exported" {
							cell ^= 0xabc12345
						}
						want := cell - 7
						if mode == "global-written" {
							want = 24
						}
						if mode == "conditional-definition" && uint32(arg) == 0 {
							want = 0
						}
						if mode == "reassigned" && uint32(arg) != 0 {
							want += 3
						}
						if !wide {
							want = uint64(uint32(want))
						}
						var stats ModuleStats
						opts := CompileOptions{}
						if diagnosticsEnabled {
							opts.Stats = &stats
						}
						got, err := runPrivateGlobalCells(t, m, opts, arg, cell)
						if err != nil || got != want {
							t.Fatalf("mode=%s wide=%v enabled=%v literal=%x arg=%x got=%x want=%x err=%v", mode, wide, enabled, literal, arg, got, want, err)
						}
						if diagnosticsEnabled {
							global := enabled && mode != "exported" && mode != "global-written"
							local := global && mode != "conditional-definition"
							if (stats.Funcs[0].Peephole["private-local-constant-fact"] > 0) != local {
								t.Fatalf("mode=%s enabled=%v stats=%v", mode, enabled, stats.Funcs[0].Peephole)
							}
						}
					}
				}
			}
		}
	}
}

func TestPrivateConstantProofRejectsOpaqueAndWrittenState(t *testing.T) {
	old := privateConstantsEnabled
	privateConstantsEnabled = true
	t.Cleanup(func() { privateConstantsEnabled = old })
	for _, mode := range []string{"private", "exported", "written-unreachable", "multiple-functions", "unbacked", "prefix", "dependent-initializer"} {
		body := []byte{0, 0x23, 0, 0x0b}
		if mode == "written-unreachable" {
			body = []byte{0, 0x41, 0, 0x04, 0x40, 0x41, 7, 0x24, 0, 0x0b, 0x23, 0, 0x0b}
		}
		if mode == "prefix" {
			body = []byte{0, 0x41, 0, 0x41, 0, 0x41, 0, 0xfc, 0x0b, 0, 0x23, 0, 0x0b}
		}
		m := modMem(t, 1, nil, []wasm.ValType{wasm.I32}, body)
		m.Globals = []wasm.Global{{Type: wasm.GlobalType{Type: wasm.I32, Mutable: true}, Init: wasm.Expr{Instrs: []wasm.Instruction{{Kind: wasm.InstrI32Const, I32: 17}}}}}
		switch mode {
		case "exported":
			m.Exports = append(m.Exports, wasm.Export{Index: wasm.ExternIdx{Kind: wasm.ExternGlobal, Index: 0}})
		case "multiple-functions":
			m.Code = append(m.Code, m.Code[0])
		case "unbacked":
			m.Code[0].BodyBytes = nil
		case "dependent-initializer":
			m.Globals[0].Init = wasm.Expr{Instrs: []wasm.Instruction{{Kind: wasm.InstrGlobalGet, Index: 0}}}
		}
		f := fn{m: m, policy: currentCodegenPolicy(), classifier: wasm.NewModuleInstructionClassifier(m, true)}
		globals, _ := f.provePrivateConstants()
		if (globals != 0) != (mode == "private") {
			t.Fatalf("mode=%s globals=%x", mode, globals)
		}
	}
}

func TestPrivateConstantProofSignedImmediateWidths(t *testing.T) {
	for _, immediate := range []int64{-129, -65, -64, 63, 64, 127, 1 << 40} {
		body := []byte{1, 1, 0x7e, 0x23, 0, 0x42}
		body = append(body, wasmtest.SLEB64(immediate)...)
		body = append(body, 0x7c, 0x21, 0, 0x20, 0, 0x0b)
		m := modMem(t, 1, nil, []wasm.ValType{wasm.I64}, body)
		m.Globals = []wasm.Global{{Type: wasm.GlobalType{Type: wasm.I64, Mutable: true}, Init: wasm.Expr{Instrs: []wasm.Instruction{{Kind: wasm.InstrI64Const, I64: 11}}}}}
		f := fn{m: m, policy: currentCodegenPolicy(), classifier: wasm.NewModuleInstructionClassifier(m, true)}
		globals, locals := f.provePrivateConstants()
		if globals != 1 || locals != 1 || f.privateInitializerN != 1 || f.privateInitializers[0].value.cval != 11+immediate {
			t.Fatalf("immediate=%d globals=%x locals=%x initializers=%+v", immediate, globals, locals, f.privateInitializers[:f.privateInitializerN])
		}
	}
}
