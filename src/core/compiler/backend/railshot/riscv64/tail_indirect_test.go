//go:build linux && (amd64 || arm64 || riscv64) && !tinygo && !wago_target_tinygo

package riscv64

import (
	"encoding/binary"
	"errors"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func localIndirectTailSource(vector bool, funcs ...localTailFixture) []byte {
	var types, codes [][]byte
	indices := wasmtest.ULEB(uint32(len(funcs)))
	for i, f := range funcs {
		types = append(types, wasmtest.FuncType(f.params, f.results))
		indices = append(indices, wasmtest.ULEB(uint32(i))...)
		decls := []byte{0}
		if f.locals != 0 {
			decls = append([]byte{1}, wasmtest.ULEB(uint32(f.locals))...)
			decls = append(decls, 0x7e)
		}
		body := append(decls, f.body...)
		codes = append(codes, append(wasmtest.ULEB(uint32(len(body))), body...))
	}
	if vector {
		types = append(types, wasmtest.FuncType([]wasm.ValType{wasm.V128}, nil))
	}
	return wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(types...)), wasmtest.Section(3, indices), wasmtest.Section(4, []byte{1, 0x70, 1, 3, 3}), wasmtest.Section(9, []byte{1, 0, 0x41, 0, 0x0b, 2, 0, 1}), wasmtest.Section(10, wasmtest.Vec(codes...)))
}

func TestLocalIndirectTailAdmissionAndNonlinkEncoding(t *testing.T) {
	for _, vector := range []bool{false, true} {
		f := localTailFixture{params: []wasm.ValType{wasm.I32}, results: []wasm.ValType{wasm.I32}, body: []byte{0x20, 0, 0x41, 1, 0x13, 0, 0, 0x0b}}
		leaf := localTailFixture{params: f.params, results: f.results, body: []byte{0x20, 0, 0x0b}}
		m := localTailDecode(t, localIndirectTailSource(vector, f, leaf))
		image, err := CompileModuleWith(m, localTailOptions())
		if err != nil {
			t.Fatal(vector, err)
		}
		matches := 0
		for off := image.Entry[0] + 8; off+4 <= image.Entry[1]; off += 4 {
			// JALR x0,x28,0, preceded by ADD SP,SP,X30 and saved RA reload.
			if binary.LittleEndian.Uint32(image.Code[off:]) == 0x000e0067 {
				matches++
				if binary.LittleEndian.Uint32(image.Code[off-4:]) != 0x01e10133 {
					t.Fatal("indirect tail did not release frame", vector, off)
				}
			}
		}
		if matches != 1 {
			t.Fatal("missing genuine nonlink indirect tail", vector, matches)
		}
		for _, opts := range []CompileOptions{{Interruptible: true, DeferCodeMapping: true}, {localTailCalls: true, Interruptible: true, ForeignTableCalls: true, DeferCodeMapping: true}} {
			pressure := 0
			opts.MemoryPressure = func() { pressure++ }
			if image, err := CompileModuleWith(m, opts); image != nil || !errors.Is(err, ErrUnsupported) || pressure != 0 {
				t.Fatal("unsupported dynamic target staged", vector, err, pressure)
			}
		}
	}
}

func TestLocalIndirectTailDeadAdmissionAndBudget(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, count := range []int{1024, 1025} {
			body := []byte{0x00}
			for range count {
				body = append(body, 0x13, 0, 0)
			}
			body = append(body, 0x0b)
			m := localTailDecode(t, localIndirectTailSource(vector, localTailFixture{body: body}, localTailFixture{body: []byte{0x0b}}))
			pressure := 0
			opts := localTailOptions()
			opts.MemoryPressure = func() { pressure++ }
			image, err := CompileModuleWith(m, opts)
			if count == 1024 {
				if err != nil {
					t.Fatal("bounded dead indirect tails", vector, err)
				}
			} else if image != nil || !errors.Is(err, ErrUnsupported) || pressure != 0 {
				t.Fatal("dead tail budget staged", vector, err, pressure)
			}
		}
		// Raw walker probes exercise dead admission, including same-width
		// mismatched results and foreign/imported-target policy, before emission.
		ft := &wasm.CompType{Kind: wasm.CompFunc, Results: []wasm.ValType{wasm.I32}}
		wrong := &wasm.CompType{Kind: wasm.CompFunc, Results: []wasm.ValType{wasm.F32}}
		fn := &wasm.Func{BodyBytes: []byte{0x00, 0x13, 0, 0, 0x0b}}
		state := moduleState{production: true, localTailCalls: true, ownedTable: true, types: []functionSpec{{signature: wrong, results: []bool{false}}}}
		if _, err := walkInteger(fn, ft, &state, nil); !errors.Is(err, ErrUnsupported) {
			t.Fatal("wrong exact tail results", err)
		}
		state.types[0].signature = ft
		state.foreignTableCalls = true
		if _, err := walkInteger(fn, ft, &state, nil); !errors.Is(err, ErrUnsupported) {
			t.Fatal("foreign dead tail admitted", err)
		}
		state.foreignTableCalls = false
		state.importedFunctions = 1
		if _, err := walkInteger(fn, ft, &state, nil); !errors.Is(err, ErrUnsupported) {
			t.Fatal("imported dead tail admitted", err)
		}
	}
}
