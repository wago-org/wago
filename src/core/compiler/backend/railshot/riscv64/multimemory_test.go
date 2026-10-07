//go:build linux && (amd64 || arm64 || riscv64) && !tinygo && !wago_target_tinygo

package riscv64

import (
	"bytes"
	"errors"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func multiMemoryBackendSource(count int, params, results []wasm.ValType, body []byte) []byte {
	memories := make([][]byte, count)
	for i := range memories {
		memories[i] = []byte{1, 1, 2}
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(params, results))),
		wasmtest.Section(3, []byte{1, 0}),
		wasmtest.Section(5, wasmtest.Vec(memories...)),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
}

func multiMemoryBackendDecode(t *testing.T, source []byte) *wasm.Module {
	t.Helper()
	d, err := wasm.DecodeModuleByteBackedWithLimits(source, wasm.ValidationFeatures{MultiMemory: true}, wasm.DecodeLimits{})
	if err != nil {
		t.Fatal(err)
	}
	return d.Module
}

func TestProductionMultiMemoryCompleteInstructionAdmission(t *testing.T) {
	for _, vectorProfile := range []bool{false, true} {
		for op := byte(0x28); op <= 0x40; op++ {
			params, results := []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}
			body := []byte{0x20, 0}
			if op == 0x3f {
				params, body = nil, nil
			} else if op <= 0x3e {
				_, wide, _, store, _ := memoryOpcode(op)
				value := wasm.I32
				if wide {
					value = wasm.I64
				}
				if op == 0x2a || op == 0x38 {
					value = wasm.F32
				} else if op == 0x2b || op == 0x39 {
					value = wasm.F64
				}
				results = []wasm.ValType{value}
				if store {
					params, results = append(params, value), nil
					body = append(body, 0x20, 1)
				}
			}
			body = append(body, op)
			if op < 0x3f {
				body = append(body, 64, 99, 0) // explicit memory99, alignment0, offset0
			} else {
				body = append(body, 99)
			}
			body = append(body, 0x0b)
			source := multiMemoryBackendSource(MaxProductionMemories, params, results, body)
			m := multiMemoryBackendDecode(t, source)
			if vectorProfile {
				// An unused V128 signature selects the same complete SWAR walker.
				m.Types = append(m.Types, wasm.RecType{SubTypes: []wasm.SubType{{Final: true, Comp: wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.V128}}}}})
			}
			if err := AdmitProductionSource(source); err != nil {
				t.Fatal("public wire", op, err)
			}
			image, err := CompileModuleWith(m, CompileOptions{DeferCodeMapping: true, Interruptible: true})
			if err != nil || image == nil || image.CodeImage != nil || len(image.Entry) != 1 || len(image.Code) == 0 {
				t.Fatal("complete indexed scalar family", vectorProfile, op, err)
			}
		}
	}
}

func TestProductionMultiMemoryBoundsBeforeStagingAndPrivateClosure(t *testing.T) {
	for _, vector := range []bool{false, true} {
		result, body := wasm.I32, []byte{0x41, 0, 0x0b}
		if vector {
			result, body = wasm.V128, append(append([]byte{0xfd, 12}, make([]byte, 16)...), 0x0b)
		}
		good := multiMemoryBackendSource(MaxProductionMemories, nil, []wasm.ValType{result}, body)
		if err := AdmitProductionSource(good); err != nil {
			t.Fatal(err)
		}
		// Both private byte provenance profiles retain their one-memory boundary.
		if err := admitProductionWire(good); !errors.Is(err, ErrUnsupported) {
			t.Fatal("private wire widened", vector, err)
		}
		m := multiMemoryBackendDecode(t, good)
		if vector {
			if image, err := compileSWARDecodedModule(m, emissionMode{production: true}, swarModulePlain); image != nil || !errors.Is(err, ErrUnsupported) {
				t.Fatal("private SWAR decoded widened", err)
			}
		} else if _, err := admitStateMode(m, true); !errors.Is(err, ErrUnsupported) {
			t.Fatal("private scalar decoded widened", err)
		}
		for _, fault := range []string{"count", "shared", "memory64", "minimum", "maximum", "reversed"} {
			bad := multiMemoryBackendDecode(t, good)
			switch fault {
			case "count":
				bad.Memories = append(bad.Memories, bad.Memories[0])
			case "shared":
				bad.Memories[99].Shared = true
			case "memory64":
				bad.Memories[99].Limits.Addr64 = true
			case "minimum":
				bad.Memories[99].Limits.Min = 65537
			case "maximum":
				bad.Memories[99].Limits.Max = 65537
			case "reversed":
				bad.Memories[99].Limits.Max = 0
			}
			pressure := 0
			image, err := CompileModuleWith(bad, CompileOptions{MemoryPressure: func() { pressure++ }})
			if image != nil || err == nil || pressure != 0 {
				t.Fatal("unused secondary memory bypassed admission", vector, fault, err, pressure)
			}
		}
	}
}

func TestSWARIndexedMemoryCompleteMemargAndDeadBounds(t *testing.T) {
	for _, kind := range []uint32{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 84, 85, 86, 87, 88, 89, 90, 91, 92, 93} {
		params, results := []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.V128}
		body := []byte{0x20, 0}
		lane := kind >= 84 && kind <= 91
		store := kind == 11 || kind >= 88 && kind <= 91
		if lane || store {
			params, body = append(params, wasm.V128), append(body, 0x20, 1)
		}
		if store {
			results = nil
		}
		body = append(body, 0xfd)
		body = append(body, wasmtest.ULEB(kind)...)
		body = append(body, 64, 99, 0)
		if lane {
			body = append(body, 0)
		}
		body = append(body, 0x0b)
		m := multiMemoryBackendDecode(t, multiMemoryBackendSource(100, params, results, body))
		image, err := CompileModuleWith(m, CompileOptions{DeferCodeMapping: true, Interruptible: true})
		if err != nil || image == nil || len(image.Code) == 0 {
			t.Fatal("indexed SIMD family", kind, err)
		}
		ft, _ := m.LocalFuncType(0)
		if _, err := walkSWARPairsWithImportMode(&m.Code[0], ft, true, nil, nil, nil, 0, nil, nil, 0, 0, false, false, nil); err == nil {
			t.Fatal("private walker admits explicit memarg", kind)
		}
		ghost := wasm.Func{BodyBytes: append([]byte{0x00}, body[2:]...)}
		if _, err := walkSWARPairsWithMemoryMode(&ghost, ft, 100, true, nil, nil, nil, 0, nil, nil, 0, 0, false, false, nil); err != nil {
			t.Fatal("dead valid memory control", kind, err)
		}
		// A known out-of-range memory remains denied even with polymorphic operands.
		if _, err := walkSWARPairsWithMemoryMode(&ghost, ft, 99, true, nil, nil, nil, 0, nil, nil, 0, 0, false, false, nil); err == nil {
			t.Fatal("dead indexed memory bypass", kind)
		}
	}
}

func TestProductionMultiMemoryBulkIndicesAndOriginalDataNamespace(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, sub := range []byte{8, 9, 10, 11} {
			params := []wasm.ValType{wasm.I32, wasm.I32, wasm.I32}
			body := []byte{0x20, 0, 0x20, 1, 0x20, 2, 0xfc, sub}
			switch sub {
			case 8:
				body = append(body, 1, 99)
			case 9:
				params, body = nil, []byte{0xfc, 9, 1}
			case 10:
				body = append(body, 99, 98)
			case 11:
				body = append(body, 99)
			}
			body = append(body, 0x0b)
			m := multiMemoryBackendDecode(t, multiMemoryBackendSource(100, params, nil, []byte{0x0b}))
			m.Code[0].BodyBytes = body
			// Original data0 is active on a secondary memory, while data1 is
			// passive. Both instructions retain their original numeric index.
			dataSource := wasmtest.Module(wasmtest.Section(5, []byte{1, 1, 1, 2}),
				wasmtest.Section(11, []byte{2, 0, 0x41, 0, 0x0b, 1, 0x44, 1, 1, 0x77}))
			data := multiMemoryBackendDecode(t, dataSource)
			m.Data = data.Data
			m.Data[0].Mode.Mem = 98
			count := uint32(2)
			m.DataCount = &count
			if vector {
				m.Types = append(m.Types, wasm.RecType{SubTypes: []wasm.SubType{{Final: true, Comp: wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.V128}}}}})
			}
			image, err := CompileModuleWith(m, CompileOptions{DeferCodeMapping: true, Interruptible: true})
			if err != nil || image == nil || len(image.Code) == 0 {
				t.Fatal("indexed bulk original namespace", vector, sub, err)
			}
			state, err := admitStatePolicy(m, true, true, true, false)
			if err != nil || len(state.passiveData) != 2 || state.passiveData[0] != nil || !bytes.Equal(state.passiveData[1], []byte{0x77}) {
				t.Fatal("secondary active descriptor not initially dropped", vector, sub, err)
			}
		}
	}
}

func TestProductionIndexedMemoryHelpersStayWithinChargedBounds(t *testing.T) {
	for _, index := range []uint32{0, 85, 86, 99} {
		for _, kind := range []wasm.InstrKind{wasm.InstrMemoryCopy, wasm.InstrMemoryFill, wasm.InstrMemoryInit} {
			f := integerEmitter{target: target{mode: emissionMode{production: true, interruptible: true}}}
			f.memoryBulkAt(kind, 255*16, index, 99-index)
			if f.err != nil || f.a.Err() != nil || f.Position() > 768 {
				t.Fatal("bulk indexed envelope exceeds 1024 including homes/traps", index, kind, f.Position(), f.err, f.a.Err())
			}
		}
		f := integerEmitter{target: target{mode: emissionMode{production: true}}}
		f.productionMemorySizeGrowAt(index, true)
		if f.err != nil || f.a.Err() != nil || f.Position() > 768 {
			t.Fatal("growth indexed envelope", index, f.Position(), f.err, f.a.Err())
		}
	}
	// Explicit index0 shares the ordinary path and still handles a missing dir.
	a, b := integerEmitter{target: target{mode: emissionMode{production: true}}}, integerEmitter{target: target{mode: emissionMode{production: true}}}
	a.memoryAccess(0, 8, true, false, false)
	b.memoryAccessAt(0, 0, 8, true, false, false)
	if !bytes.Equal(a.a.B, b.a.B) {
		t.Fatal("index0 wrapper differs")
	}
	var address integerEmitter
	address.productionMemoryBaseAt(99, enc.X28)
	if address.a.Err() != nil || address.Position() <= 12 {
		t.Fatal("index99 did not materialize the escaped directory address")
	}
}
