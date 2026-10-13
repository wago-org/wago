//go:build linux && !tinygo

package riscv64

import (
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime/abi"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestPrivateNativeTableThunkBounds(t *testing.T) {
	if nativeTableFrameBytes%16 != 0 || nativeTableFrameBytes != 5424 {
		t.Fatal("native table frame layout", nativeTableFrameBytes)
	}
	for _, key := range []uint64{0, 0xfedcba9876543210} {
		for _, slots := range []int{0, 1, 64, 255, 256} {
			for _, results := range []int{0, 1, 256} {
				code, err := nativeTableThunkAt(key, slots, results, MaxFunctions-1, 0xfffffffe)
				if err != nil || len(code) == 0 || len(code)%4 != 0 || len(code)+16 > nativeTableCallCodeBound(slots, results) {
					t.Fatal("bounded table transport emission", key, slots, results, len(code), err)
				}
			}
		}
	}
	for _, limits := range [][3]int{{-1, 0, 0}, {257, 0, 0}, {0, -1, 0}, {0, 257, 0}, {0, 0, MaxFunctions}} {
		if code, err := nativeTableThunk(0, limits[0], limits[1], uint32(limits[2])); code != nil || err == nil {
			t.Fatal("unbounded table transport emitted", limits, err)
		}
	}
}

func TestPrivateNativeTableThunkFrameWalkMetadata(t *testing.T) {
	for _, invocation := range []bool{false, true} {
		image, err := nativeTableThunkImageAtInvocation(7, 2, 3, 1, 9, invocation)
		if err != nil {
			t.Fatal(err)
		}
		if image.targetReturnOffset < 4 || int(image.targetReturnOffset) >= len(image.code) || image.frameBytes%16 != 0 {
			t.Fatalf("invalid bridge metadata: return=%d frame=%d code=%d", image.targetReturnOffset, image.frameBytes, len(image.code))
		}
		want := uint32(nativeTableFrameBytes)
		if invocation {
			want += uint32(abi.RV64InvocationViewBytes + 16)
		}
		if image.frameBytes != want {
			t.Fatalf("invocation=%t frame=%d, want %d", invocation, image.frameBytes, want)
		}
	}
}

// Repeated void sites exposed the missing helper charge: the ordinary SWAR
// estimate allowed only 1024 bytes per site, smaller than the embedded bridge.
func TestPrivateNativeTableCallRepeatedAdmission(t *testing.T) {
	for _, arity := range []int{0, 1} {
		for _, count := range []int{1, 128, maxDirectCalls, maxDirectCalls + 1} {
			t.Run(fmt.Sprintf("arity%d/sites%d", arity, count), func(t *testing.T) {
				var params, results []wasm.ValType
				leaf := []byte{0x0b}
				if arity == 1 {
					params, results = []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}
					leaf = []byte{0x20, 0, 0x0b}
				}
				var body []byte
				for i := 0; i < count; i++ {
					if arity == 1 {
						body = append(body, 0x41, 1)
					}
					body = append(body, 0x41, 0, 0x11, 0, 0)
					if arity == 1 {
						body = append(body, 0x1a)
					}
				}
				body = append(body, 0x0b)
				source := wasmtest.Module(
					wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(params, results), wasmtest.FuncType(nil, nil))),
					wasmtest.Section(3, []byte{2, 0, 1}),
					wasmtest.Section(4, []byte{1, 0x70, 1, 1, 1}),
					wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(leaf), wasmtest.Code(body))),
				)
				m, err := wasm.DecodeModule(source)
				if err != nil {
					t.Fatal(err)
				}
				if err := wasm.ValidateModuleWithConfig(m, wasm.ValidationFeatures{}, 1, wasm.ValidationLimits{MaxFunctionLocals: 256}); err != nil {
					t.Fatal(err)
				}
				types := make([]*wasm.CompType, len(m.Types))
				keys := make([]uint64, len(types))
				functions := make([]*wasm.CompType, len(m.Code))
				for i := range types {
					types[i] = &m.Types[i].SubTypes[0].Comp
					var ok bool
					keys[i], ok = m.StructuralTypeKeyChecked(uint32(i))
					if !ok {
						t.Fatal("type key")
					}
				}
				for i := range functions {
					functions[i], _ = m.LocalFuncType(i)
				}
				fn, ft, table := &m.Code[1], functions[1], &m.Tables[0].Type
				mode := emissionMode{production: true, interruptible: true, functionIndex: 1, entryPC: uint32(fn.LocalDeclBytes), foreignTableCalls: true}
				ordinary, ordinaryErr := walkSWARPairsWithImports(fn, ft, false, types, functions, nil, 0, table, keys, 0, 0, nil)
				foreign, foreignErr := walkSWARPairsWithImportMode(fn, ft, false, types, functions, nil, 0, table, keys, 0, 0, true, false, nil)
				code, _, _, swarErr := compileSWARPairsWithImports(fn, ft, nil, types, functions, nil, 0, table, keys, 0, 0, mode)
				state, err := admitStateMode(m, true)
				if err != nil {
					t.Fatal(err)
				}
				summary, scalarErr := walkInteger(fn, ft, &state, nil)
				var scalar []byte
				if scalarErr == nil {
					scalar, _, _, scalarErr = compileIntegerMode(fn, ft, &state, summary, mode)
				}
				if count > maxDirectCalls {
					if ordinaryErr == nil || foreignErr == nil || swarErr == nil || scalarErr == nil || code != nil || scalar != nil {
						t.Fatal("call-count bound escaped")
					}
					return
				}
				charge := count * nativeTableCallCodeBound(arity, arity)
				if ordinaryErr != nil || foreignErr != nil || swarErr != nil || scalarErr != nil || foreign.codeBound-ordinary.codeBound != charge || summary.tableBridgeBytes != charge || len(code) > foreign.codeBound || len(scalar) > MaxCodeBytes {
					t.Fatal("foreign admission/encoding", ordinary, foreign, summary, len(code), len(scalar), ordinaryErr, foreignErr, swarErr, scalarErr)
				}
			})
		}
	}
}
