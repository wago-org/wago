//go:build linux && (amd64 || arm64 || riscv64) && !tinygo && !wago_target_tinygo

package riscv64

import (
	"bytes"
	"errors"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func memory64ULEB(v uint64) []byte {
	var out []byte
	for {
		b := byte(v & 127)
		v >>= 7
		if v != 0 {
			b |= 128
		}
		out = append(out, b)
		if v == 0 {
			return out
		}
	}
}

func memory64BackendModule(t *testing.T, wideIndices []int, params, results []wasm.ValType, body []byte, vector bool) *wasm.Module {
	t.Helper()
	// Decode a neutral body before installing the actual per-memory widths.
	m := multiMemoryBackendDecode(t, multiMemoryBackendSource(MaxProductionMemories, params, results, []byte{0x0b}))
	for _, index := range wideIndices {
		m.Memories[index].Limits.Addr64 = true
	}
	m.Code[0].BodyBytes = body
	if vector {
		m.Types = append(m.Types, wasm.RecType{SubTypes: []wasm.SubType{{Final: true, Comp: wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.V128}}}}})
	}
	return m
}

func TestProductionMemory64CompleteScalarAndMixedOffsetGrammar(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, index := range []byte{0, 99} {
			for op := byte(0x28); op <= 0x40; op++ {
				params, results := []wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I64}
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
					}
					if op == 0x2b || op == 0x39 {
						value = wasm.F64
					}
					results = []wasm.ValType{value}
					if store {
						params, results, body = append(params, value), nil, append(body, 0x20, 1)
					}
				}
				body = append(body, op)
				if op < 0x3f {
					body = append(body, 64, index)
					body = append(body, memory64ULEB(^uint64(0))...)
				} else {
					body = append(body, index)
				}
				body = append(body, 0x0b)
				m := memory64BackendModule(t, []int{int(index)}, params, results, body, vector)
				image, err := CompileModuleWith(m, CompileOptions{DeferCodeMapping: true, Interruptible: true})
				if err != nil || image == nil || image.CodeImage != nil || len(image.Entry) != 1 || len(image.Code) == 0 {
					t.Fatal("memory64 scalar family", vector, index, op, err)
				}
			}
		}
	}
	// FD bytes inside ULEB offsets must not select vector lowering.
	body := append([]byte{0x20, 0, 0x2d, 64, 99}, memory64ULEB(0xfd)...)
	body = append(body, 0x0b)
	m := memory64BackendModule(t, []int{99}, []wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I32}, body, false)
	if vector, err := UsesSWAR(m); vector || err != nil {
		t.Fatal("offset selected SIMD", vector, err)
	}
	m.Code[0].BodyBytes = append(append([]byte{0x41, 0, 0x2d, 64, 0}, memory64ULEB(1<<32)...), 0x0b)
	if image, err := CompileModuleWith(m, CompileOptions{DeferCodeMapping: true}); image != nil || err == nil {
		t.Fatal("memory32 accepted u64 offset", err)
	}
}

func TestProductionMemory64SIMDMemoryFamilyAndDeadClassBounds(t *testing.T) {
	for _, kind := range []uint32{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 84, 85, 86, 87, 88, 89, 90, 91, 92, 93} {
		params, results := []wasm.ValType{wasm.I64}, []wasm.ValType{wasm.V128}
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
		body = append(body, 64, 99)
		body = append(body, memory64ULEB(^uint64(0))...)
		if lane {
			body = append(body, 0)
		}
		body = append(body, 0x0b)
		m := memory64BackendModule(t, []int{99}, params, results, body, false)
		if image, err := CompileModuleWith(m, CompileOptions{DeferCodeMapping: true, Interruptible: true}); err != nil || image == nil {
			t.Fatal("memory64 SIMD family", kind, err)
		}
		ft, _ := m.LocalFuncType(0)
		profile := makeMemoryProfile(m.Memories, true)
		ghost := wasm.Func{BodyBytes: append([]byte{0x00}, body[2:]...)}
		if _, err := walkSWARPairsWithMemoryProfile(&ghost, ft, profile, nil, nil, nil, 0, nil, nil, 0, 0, false, false, nil); err != nil {
			t.Fatal("dead memory64 operation", kind, err)
		}
		ghost.BodyBytes = append([]byte{0x00, 0x41, 0}, body[2:]...)
		if lane || store {
			ghost.BodyBytes = append([]byte{0x00, 0x41, 0, 0x20, 1}, body[4:]...)
		}
		if _, err := walkSWARPairsWithMemoryProfile(&ghost, ft, profile, nil, nil, nil, 0, nil, nil, 0, 0, false, false, nil); err == nil {
			t.Fatal("dead known wrong address class", kind)
		}
	}
}

func TestProductionMemory64BulkOperandWidthsAndOriginalData(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, sub := range []byte{8, 10, 11} {
			for _, wide := range [][2]bool{{false, false}, {false, true}, {true, false}, {true, true}} {
				params := []wasm.ValType{wasm.I32, wasm.I32, wasm.I32}
				if wide[0] {
					params[0] = wasm.I64
				}
				if sub == 10 && wide[1] {
					params[1] = wasm.I64
				}
				count64 := sub == 11 && wide[0] || sub == 10 && wide[0] && wide[1]
				if count64 {
					params[2] = wasm.I64
				}
				body := []byte{0x20, 0, 0x20, 1, 0x20, 2, 0xfc, sub}
				switch sub {
				case 8:
					body = append(body, 0, 99)
				case 10:
					body = append(body, 99, 98)
				case 11:
					body = append(body, 99)
				}
				body = append(body, 0x0b)
				indices := []int{}
				if wide[0] {
					indices = append(indices, 99)
				}
				if wide[1] {
					indices = append(indices, 98)
				}
				m := memory64BackendModule(t, indices, params, nil, body, vector)
				m.Data = []wasm.Data{{Mode: wasm.DataMode{Kind: wasm.DataPassive}, Init: []byte{1, 2, 3}}}
				count := uint32(1)
				m.DataCount = &count
				if image, err := CompileModuleWith(m, CompileOptions{DeferCodeMapping: true, Interruptible: true}); image == nil || err != nil {
					t.Fatal("bulk width matrix", vector, sub, wide, err)
				}
				m.Types[0].SubTypes[0].Comp.Params[2] = wasm.I64
				if count64 {
					m.Types[0].SubTypes[0].Comp.Params[2] = wasm.I32
				}
				pressure := 0
				if image, err := CompileModuleWith(m, CompileOptions{DeferCodeMapping: true, Interruptible: true, MemoryPressure: func() { pressure++ }}); image != nil || err == nil || pressure != 0 {
					t.Fatal("wrong count staged", vector, sub, wide, err, pressure)
				}
			}
		}
	}
}

func TestProductionMemory64ShapeInitializerAndPrivateClosure(t *testing.T) {
	body := []byte{0x41, 7, 0x0b}
	for _, vector := range []bool{false, true} {
		m := memory64BackendModule(t, []int{99}, nil, []wasm.ValType{wasm.I32}, body, vector)
		m.Memories[99].Limits.Max = 1 << 48
		if image, err := CompileModuleWith(m, CompileOptions{DeferCodeMapping: true}); err != nil || image == nil {
			t.Fatal("exact maximum incorrectly limited to reservation", vector, err)
		}
		if _, err := admitStateMode(m, true); !errors.Is(err, ErrUnsupported) {
			t.Fatal("private scalar memory64 opened", err)
		}
		if image, err := compileSWARDecodedModule(m, emissionMode{production: true}, swarModulePlain); image != nil || !errors.Is(err, ErrUnsupported) {
			t.Fatal("private SWAR profile opened", err)
		}
		// Test memory64 itself at index0 through the old one-memory pilots too.
		private := memory64BackendModule(t, []int{0}, nil, []wasm.ValType{wasm.I32}, body, false)
		private.Memories = private.Memories[:1]
		if _, err := admitStateMode(private, true); !errors.Is(err, ErrUnsupported) {
			t.Fatal("single-memory private scalar widened", err)
		}
		if image, err := compileSWARDecodedModule(private, emissionMode{production: true}, swarModulePlain); image != nil || !errors.Is(err, ErrUnsupported) {
			t.Fatal("single-memory private SWAR widened", err)
		}
		for _, fault := range []string{"min", "max", "shared", "reversed"} {
			bad := memory64BackendModule(t, []int{99}, nil, []wasm.ValType{wasm.I32}, body, vector)
			switch fault {
			case "min":
				bad.Memories[99].Limits.Min = 65536
			case "max":
				bad.Memories[99].Limits.Max = (1 << 48) + 1
			case "shared":
				bad.Memories[99].Shared = true
			case "reversed":
				bad.Memories[99].Limits.Max = 0
			}
			pressure := 0
			if image, err := CompileModuleWith(bad, CompileOptions{MemoryPressure: func() { pressure++ }}); image != nil || err == nil || pressure != 0 {
				t.Fatal("unused shape staged", vector, fault, err, pressure)
			}
		}
		m.Data = []wasm.Data{{Mode: wasm.DataMode{Kind: wasm.DataActive, Mem: 99, Offset: wasm.Expr{BodyBytes: []byte{0x42, 1, 0x0b}}}, Init: []byte{0x77}}}
		if image, err := CompileModuleWith(m, CompileOptions{DeferCodeMapping: true}); err != nil || image == nil {
			t.Fatal("I64 active data", vector, err)
		}
		state, err := admitStatePolicy(m, true, true, true, false)
		if err != nil || len(state.data) != 0 || len(state.passiveData) != 1 || state.passiveData[0] != nil {
			t.Fatal("memory64 staged uint32 private offset", err)
		}
		m.Memories[0].Limits.Addr64 = true
		m.Data[0].Mode.Mem = 0
		state, err = admitStatePolicy(m, true, true, true, false)
		if err != nil || len(state.data) != 0 {
			t.Fatal("primary memory64 staged uint32 private offset", err)
		}
		m.Data[0].Mode.Offset.BodyBytes = []byte{0x41, 1, 0x0b}
		if err := AdmitPublicInitializers(m); err == nil {
			t.Fatal("memory64 I32 offset admitted")
		}
	}
}

func TestProductionMemory64CarryHelpersStayWithinBudget(t *testing.T) {
	for _, index := range []uint32{0, 99} {
		f := integerEmitter{target: target{mode: emissionMode{production: true}}}
		f.memoryAccessTypedAt(index, ^uint64(0), 8, true, false, true, true)
		if f.err != nil || f.a.Err() != nil || f.Position() > 768 || len(f.traps) != 3 {
			t.Fatal("complete carry preflight envelope", index, f.Position(), len(f.traps), f.err, f.a.Err())
		}
		g := integerEmitter{target: target{mode: emissionMode{production: true}}}
		g.productionMemorySizeGrowTypedAt(index, true, true)
		if g.err != nil || g.a.Err() != nil || g.Position() > 768 {
			t.Fatal("full64 growth budget", index, g.Position(), g.err, g.a.Err())
		}
	}
	a, b := integerEmitter{target: target{mode: emissionMode{production: true}}}, integerEmitter{target: target{mode: emissionMode{production: true}}}
	a.memoryAccessTypedAt(0, 1, 8, true, false, false, false)
	b.memoryAccessTypedAt(0, 1, 8, true, false, false, true)
	if bytes.Equal(a.a.B, b.a.B) {
		t.Fatal("memory64 carry/normalization distinction absent")
	}
}
