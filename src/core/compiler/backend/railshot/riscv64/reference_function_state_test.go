//go:build linux && (amd64 || arm64 || riscv64) && !tinygo && !wago_target_tinygo

package riscv64

import (
	"errors"
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func referenceStateDecode(t *testing.T, vector, tails, wideFirst bool) *wasm.Module {
	t.Helper()
	decoded, err := wasm.DecodeModuleByteBackedWithLimits(wasmtest.ClosedReferenceState(vector, tails, wideFirst), wasm.ValidationFeatures{ExtendedConstGlobals: true, MultiMemory: true}, wasm.DecodeLimits{MaxTypes: MaxFunctions, MaxMetadataBytes: 16 << 20})
	if err != nil {
		t.Fatal(err)
	}
	return decoded.Module
}
func TestReferenceFunctionOwnedStateProduction(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, tails := range []bool{false, true} {
			for _, wideFirst := range []bool{false, true} {
				t.Run(fmt.Sprintf("vector%t-tails%t-wideFirst%t", vector, tails, wideFirst), func(t *testing.T) {
					m := referenceStateDecode(t, vector, tails, wideFirst)
					if err := admitClosedReferenceFunctionModule(m, vector); err != nil {
						t.Fatal(err)
					}
					if a := testing.AllocsPerRun(10, func() {
						if admitClosedReferenceFunctionModule(m, vector) != nil {
							panic("closed owned admission")
						}
					}); a > 1 {
						t.Fatal("bounded mixed-width classifier allocation", a)
					}
					if a := testing.AllocsPerRun(10, func() {
						remaining := MaxModuleBytes
						if admitClosedReferenceNumericState(m, vector, &remaining) != nil {
							panic("owned numeric preflight")
						}
					}); a != 0 {
						t.Fatal("state preflight allocation", a)
					}
					opts := CompileOptions{NativeReferenceFunctions: true, NativeTailCalls: tails, Interruptible: true, ForeignTableCalls: true, InvocationContext: true, DeferCodeMapping: true}
					image, err := CompileModuleWith(m, opts)
					if err != nil {
						t.Fatal(err)
					}
					if !image.NativeReferenceFunctions || image.NativeReferenceCalls || image.NativeReferenceControl || len(image.Entry) != 12 {
						t.Fatal("owned-state image contract")
					}
				})
			}
		}
	}
}
func TestReferenceFunctionOwnedStateAdmission(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for name, mutate := range map[string]func(*wasm.Module){
			"memory-count":      func(m *wasm.Module) { m.Memories = make([]wasm.MemType, MaxProductionMemories+1) },
			"shared":            func(m *wasm.Module) { m.Memories[0].Shared = true },
			"inactive-max":      func(m *wasm.Module) { m.Memories[0].Limits.HasMax = false },
			"reference-global":  func(m *wasm.Module) { m.Globals[0].Type.Type = wasm.FuncRef },
			"global-count":      func(m *wasm.Module) { m.Globals = make([]wasm.Global, 257) },
			"structured-global": func(m *wasm.Module) { m.Globals[0].Init.Instrs = []wasm.Instruction{{}} },
			"empty-global":      func(m *wasm.Module) { m.Globals[0].Init.BodyBytes = nil },
			"large-global":      func(m *wasm.Module) { m.Globals[0].Init.BodyBytes = make([]byte, MaxIntegerInitializerBytes+1) },
			"data-count":        func(m *wasm.Module) { m.Data = make([]wasm.Data, 257) },
			"wrong-data-count":  func(m *wasm.Module) { *m.DataCount = 99 },
			"data-memory":       func(m *wasm.Module) { m.Data[0].Mode.Mem = 2 },
			"data-mode":         func(m *wasm.Module) { m.Data[0].Mode.Kind = 9 },
			"structured-offset": func(m *wasm.Module) { m.Data[0].Mode.Offset.Instrs = []wasm.Instruction{{}} },
			"large-offset":      func(m *wasm.Module) { m.Data[0].Mode.Offset.BodyBytes = make([]byte, MaxIntegerInitializerBytes+1) },
			"passive-offset":    func(m *wasm.Module) { m.Data[2].Mode.Offset.BodyBytes = []byte{0x41, 0, 0x0b} },
			"passive-memory":    func(m *wasm.Module) { m.Data[2].Mode.Mem = 1 },
			"global-export":     func(m *wasm.Module) { m.Exports[0].Index.Kind = wasm.ExternGlobal; m.Exports[0].Index.Index = 3 },
			"memory-export":     func(m *wasm.Module) { m.Exports[0].Index.Kind = wasm.ExternMem; m.Exports[0].Index.Index = 2 },
		} {
			t.Run(fmt.Sprintf("vector%t-%s", vector, name), func(t *testing.T) {
				m := referenceStateDecode(t, vector, false, false)
				mutate(m)
				if name == "shared" {
					checkPublicPrimitiveModule(t, m, closedReferenceProductionOptions(false, true))
					return
				}
				if err := admitClosedReferenceFunctionModule(m, vector); !errors.Is(err, ErrUnsupported) {
					t.Fatal("owned state admission", err)
				}
			})
		}
		m := referenceStateDecode(t, vector, false, false)
		m.Memories[1].Limits.HasMax = false
		m.Memories[1].Limits.Max = 0
		if _, err := CompileModuleWith(m, CompileOptions{NativeReferenceFunctions: true, Interruptible: true, ForeignTableCalls: true, InvocationContext: true, DeferCodeMapping: true}); err != nil {
			t.Fatal("owned memory without a declared maximum", err)
		}
		// Find the exact combined remaining budget without repeating the validator's
		// formula. Then independently grow data and initializer storage by one byte.
		buffer := make([]byte, MaxModuleBytes)
		m.Customs = []wasm.CustomSec{{Name: "opaque"}}
		lo, hi := 0, len(buffer)
		for lo < hi {
			mid := (lo + hi + 1) / 2
			m.Customs[0].Data = buffer[:mid]
			if admitClosedReferenceFunctionModule(m, vector) == nil {
				lo = mid
			} else {
				hi = mid - 1
			}
		}
		m.Customs[0].Data = buffer[:lo]
		if lo == 0 || admitClosedReferenceFunctionModule(m, vector) != nil {
			t.Fatal("exact combined state budget")
		}
		m.Data[2].Init = append(m.Data[2].Init, 0)
		if err := admitClosedReferenceFunctionModule(m, vector); !errors.Is(err, ErrUnsupported) {
			t.Fatal("aggregate data overflow", err)
		}
		m.Data[2].Init = m.Data[2].Init[:len(m.Data[2].Init)-1]
		m.Globals[0].Init.BodyBytes = append(m.Globals[0].Init.BodyBytes, 0)
		if err := admitClosedReferenceFunctionModule(m, vector); !errors.Is(err, ErrUnsupported) {
			t.Fatal("aggregate initializer overflow", err)
		}
	}
}

func TestReferenceFunctionOwnedStateMemoryBitmapBoundaries(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, count := range []int{64, 65, 100, 101} {
			t.Run(fmt.Sprintf("vector%t-count%d", vector, count), func(t *testing.T) {
				m := referenceStateDecode(t, vector, false, false)
				for len(m.Memories) < count {
					m.Memories = append(m.Memories, wasm.MemType{Limits: wasm.Limits{Min: 1, Max: 3, HasMax: true}})
				}
				m.Memories[count-1].Limits.Addr64 = true
				// The late memory requires an offset larger than u32. A wrong bitmap word
				// would make immediate classification reject this otherwise valid body.
				m.Code[11].BodyBytes = []byte{0x42, 0, 0x28, 0x40, byte(count - 1), 0x80, 0x80, 0x80, 0x80, 0x10, 0xad, 0x0b}
				err := admitClosedReferenceFunctionModule(m, vector)
				if count > MaxProductionMemories {
					if !errors.Is(err, ErrUnsupported) {
						t.Fatal("over-limit memory count", err)
					}
					return
				}
				if err != nil {
					t.Fatal("late memory bitmap", err)
				}
				if a := testing.AllocsPerRun(10, func() {
					if admitClosedReferenceFunctionModule(m, vector) != nil {
						panic("late memory bitmap")
					}
				}); a > 1 {
					t.Fatal("temporary bitmap count", a)
				}
				_, err = CompileModuleWith(m, CompileOptions{NativeReferenceFunctions: true, Interruptible: true, ForeignTableCalls: true, InvocationContext: true, DeferCodeMapping: true})
				if err != nil {
					t.Fatal("late memory staging", err)
				}
			})
		}
	}
}
