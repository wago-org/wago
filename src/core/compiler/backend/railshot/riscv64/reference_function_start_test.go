//go:build linux && (amd64 || arm64 || riscv64) && !tinygo && !wago_target_tinygo

package riscv64

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func referenceStartDecode(t *testing.T, vector, tails, wide bool) *wasm.Module {
	t.Helper()
	m, err := wasm.DecodeModule(wasmtest.ClosedReferenceDefinedStart(vector, tails, wide, 1, false, nil))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestReferenceFunctionDefinedStartSource(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, tails := range []bool{false, true} {
			for _, wide := range []bool{false, true} {
				t.Run(fmt.Sprintf("vector%t-tails%t-wide%t", vector, tails, wide), func(t *testing.T) {
					m := referenceStartDecode(t, vector, tails, wide)
					if *m.Start != 3 || m.ImportedFuncCount() != 3 || admitClosedReferenceFunctionModule(m, vector) != nil {
						t.Fatal("defined start closed binding")
					}
					opts := closedReferenceProductionOptions(true, false)
					opts.ImportBindings = hostImportBindings(3)
					image, err := CompileModuleWith(m, opts)
					if image == nil || err != nil || !image.NativeReferenceFunctions || len(image.Entry) != 7 || image.NativeTail == nil || len(image.NativeTail.Plans) != 3 {
						t.Fatal("defined start whole-module staging", err)
					}
					if _, err := NativeFrameBytes(image.Code[:image.Entry[1]], image.Entry[0]); err != nil {
						t.Fatal("real framed void start", err)
					}
				})
			}
		}
	}
}

func TestReferenceFunctionDefinedStartAdmission(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, tc := range []struct {
			name   string
			change func(*wasm.Module)
		}{
			{"imported", func(m *wasm.Module) { *m.Start = 0 }},
			{"missing", func(m *wasm.Module) { *m.Start = 10 }},
			{"max-index", func(m *wasm.Module) { *m.Start = wasm.FuncIdx(^uint32(0)) }},
			{"numeric-result", func(m *wasm.Module) { *m.Start = 4 }},
			{"private-source", func(m *wasm.Module) { *m.Start = 5 }},
			{"late-private-source", func(m *wasm.Module) { *m.Start = 9 }},
			{"recursive-binding", func(m *wasm.Module) { m.FuncTypes[0].Rec = true }},
			{"missing-binding", func(m *wasm.Module) { m.FuncTypes[0].Index = 99 }},
			{"params", func(m *wasm.Module) { m.Types[1].SubTypes[0].Comp.Params = []wasm.ValType{wasm.I32} }},
			{"reference-result", func(m *wasm.Module) { m.Types[1].SubTypes[0].Comp.Results = []wasm.ValType{wasm.FuncRef} }},
		} {
			t.Run(fmt.Sprintf("vector%t-%s", vector, tc.name), func(t *testing.T) {
				m := referenceStartDecode(t, vector, true, true)
				tc.change(m)
				opts := closedReferenceProductionOptions(true, false)
				opts.ImportBindings = hostImportBindings(3)
				pressure := 0
				opts.MemoryPressure = func() { pressure++ }
				if image, err := CompileModuleWith(m, opts); image != nil || !errors.Is(err, ErrUnsupported) || pressure != 0 {
					t.Fatal("bad start reached allocation", err, pressure)
				}
			})
		}
		// The start selector contributes five bounded metadata bytes, even
		// though choosing it does not change any emitted function body.
		m := referenceStartDecode(t, vector, true, true)
		name := strings.Repeat("n", MaxModuleBytes)
		lo, hi := 1, len(name)
		for lo < hi {
			mid := (lo + hi + 1) / 2
			m.Exports[0].Name = name[:mid]
			if admitClosedReferenceFunctionModule(m, vector) == nil {
				lo = mid
			} else {
				hi = mid - 1
			}
		}
		m.Exports[0].Name = name[:lo]
		if lo == 1 || admitClosedReferenceFunctionModule(m, vector) != nil {
			t.Fatal("exact start aggregate bound")
		}
		m.Exports[0].Name = name[:lo+1]
		if err := admitClosedReferenceFunctionModule(m, vector); !errors.Is(err, ErrUnsupported) {
			t.Fatal("start aggregate plus one", err)
		}
		m.Start = nil
		m.Exports[0].Name = name[:lo+5]
		if err := admitClosedReferenceFunctionModule(m, vector); err != nil {
			t.Fatal("start index charge not exactly five", err)
		}
		m.Exports[0].Name = name[:lo+6]
		if err := admitClosedReferenceFunctionModule(m, vector); !errors.Is(err, ErrUnsupported) {
			t.Fatal("absent start aggregate plus one", err)
		}
	}
}

func TestReferenceFunctionDefinedStartOtherOrdinals(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, index := range []uint32{7, 10} {
			t.Run(fmt.Sprintf("vector%t-index%d", vector, index), func(t *testing.T) {
				m, err := wasm.DecodeModule(wasmtest.ClosedReferenceDefinedStartSelected(vector, index))
				if err != nil {
					t.Fatal(err)
				}
				opts := closedReferenceProductionOptions(true, false)
				opts.ImportBindings = hostImportBindings(3)
				image, err := CompileModuleWith(m, opts)
				if err != nil || image == nil {
					t.Fatal("nonzero/final void start", err)
				}
				local := int(index) - 3
				end := len(image.Code)
				if local+1 < len(image.Entry) {
					end = image.Entry[local+1]
				}
				if _, err := NativeFrameBytes(image.Code[:end], image.Entry[local]); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}

func TestReferenceFunctionDefinedStartVoidImportedStart(t *testing.T) {
	for _, start := range []bool{false, true} {
		m, err := wasm.DecodeModule(wasmtest.ClosedReferenceImportedVoidStart(start))
		if err != nil {
			t.Fatal(err)
		}
		err = admitClosedReferenceFunctionModule(m, false)
		if err != nil {
			t.Fatal("selected void imported start gate", start, err)
		}
	}
}
