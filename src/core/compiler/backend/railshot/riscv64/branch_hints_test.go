//go:build linux && !tinygo

package riscv64

import (
	"bytes"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func branchHintFixture(vector bool, likely byte) ([]byte, []byte) {
	functions := []fixtureFunc{{params: []byte{0x7f}, result: 0x7f, body: []byte{0x41, 8, 0x20, 0, 0x0d, 0, 0x1a, 0x41, 9, 0x0b}}}
	if vector {
		functions = append(functions, fixtureFunc{params: []byte{0x7b}, result: 0x7b, body: []byte{0x20, 0, 0x0b}, unexported: true})
	}
	plain := binaryModule(functions...)
	payload := []byte{1, 0, 1, 5, 1, likely}
	return plain, rv64CustomSectionSource(plain, "metadata.code.branch_hint", payload)
}

func TestPublicBranchHintsPreserveCodeLayout(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, likely := range []byte{0, 1} {
			t.Run(fmt.Sprintf("vector%t-likely%d", vector, likely), func(t *testing.T) {
				plainSource, hintedSource := branchHintFixture(vector, likely)
				plain, err := wasm.DecodeModule(plainSource)
				if err != nil {
					t.Fatal(err)
				}
				hinted, err := wasm.DecodeModule(hintedSource)
				if err != nil {
					t.Fatal(err)
				}
				opts := CompileOptions{DeferCodeMapping: true, Interruptible: true}
				want, err := CompileModuleWith(plain, opts)
				if err != nil {
					t.Fatal(err)
				}
				got, err := CompileModuleWith(hinted, opts)
				if err != nil || got == nil || !bytes.Equal(got.Code, want.Code) || !reflect.DeepEqual(got.Entry, want.Entry) || !reflect.DeepEqual(got.InternalEntry, want.InternalEntry) {
					t.Fatal("advisory metadata changed RV64 layout", err)
				}
				if len(hinted.BranchHints) != 1 || len(hinted.BranchHints[0].Hints) != 1 || hinted.BranchHints[0].Hints[0].Likely != (likely == 1) {
					t.Fatal("decoded branch hint identity")
				}
			})
		}
	}
}

func TestPublicBranchHintsRejectHandBuiltMetadataBeforeStaging(t *testing.T) {
	plainSource, hintedSource := branchHintFixture(false, 0)
	base, err := wasm.DecodeModule(hintedSource)
	if err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*wasm.Module){
		"target":         func(m *wasm.Module) { m.BranchHints[0].Hints[0].Offset = 0 },
		"function":       func(m *wasm.Module) { m.BranchHints[0].FuncIndex = ^uint32(0) },
		"function-order": func(m *wasm.Module) { m.BranchHints = append(m.BranchHints, m.BranchHints[0]) },
		"offset-order": func(m *wasm.Module) {
			m.BranchHints[0].Hints = append(m.BranchHints[0].Hints, m.BranchHints[0].Hints[0])
		},
		"raw-mismatch":     func(m *wasm.Module) { m.Customs[0].Data[len(m.Customs[0].Data)-1] ^= 1 },
		"raw-malformed":    func(m *wasm.Module) { m.Customs[0].Data = []byte{1} },
		"duplicate-custom": func(m *wasm.Module) { m.Customs = append(m.Customs, m.Customs[0]) },
	} {
		t.Run(name, func(t *testing.T) {
			m := *base
			m.Customs = append([]wasm.CustomSec(nil), base.Customs...)
			m.Customs[0].Data = append([]byte(nil), base.Customs[0].Data...)
			m.BranchHints = append([]wasm.FuncBranchHints(nil), base.BranchHints...)
			m.BranchHints[0].Hints = append([]wasm.BranchHint(nil), base.BranchHints[0].Hints...)
			mutate(&m)
			staged := false
			image, err := CompileModuleWith(&m, CompileOptions{DeferCodeMapping: true, Interruptible: true, MemoryPressure: func() { staged = true }})
			if image != nil || !errors.Is(err, ErrUnsupported) || staged {
				t.Fatal("invalid branch metadata reached staging", err, staged)
			}
		})
	}
	plain, err := wasm.DecodeModule(plainSource)
	if err != nil {
		t.Fatal(err)
	}
	plain.BranchHints = make([]wasm.FuncBranchHints, MaxFunctions+1)
	remaining := MaxModuleBytes
	if err := admitPublicBranchHints(plain, true, &remaining); !errors.Is(err, ErrUnsupported) {
		t.Fatal("branch group bound", err)
	}
	plain.BranchHints = []wasm.FuncBranchHints{{FuncIndex: 0, Hints: []wasm.BranchHint{{Offset: 5}}}}
	remaining = branchHintGroupBytes + branchHintEntryBytes - 1
	if err := admitPublicBranchHints(plain, true, &remaining); !errors.Is(err, ErrUnsupported) {
		t.Fatal("branch byte budget", err)
	}
	remaining = MaxModuleBytes
	if err := admitPublicBranchHints(base, false, &remaining); !errors.Is(err, ErrUnsupported) {
		t.Fatal("private branch metadata admitted", err)
	}
	direct := *base
	direct.Customs = nil
	direct.BranchHints = append([]wasm.FuncBranchHints(nil), base.BranchHints...)
	direct.BranchHints[0].Hints = append([]wasm.BranchHint(nil), base.BranchHints[0].Hints...)
	direct.BranchHints[0].Hints[0].Offset = 0
	remaining = MaxModuleBytes
	if err := admitPublicBranchHints(&direct, true, &remaining); !errors.Is(err, ErrUnsupported) {
		t.Fatal("decoded-only nonbranch target admitted", err)
	}
	imported := *base
	imported.Customs = nil
	imported.Imports = append([]wasm.Import(nil), base.Imports...)
	imported.Imports = append(imported.Imports, wasm.Import{Module: "env", Name: "f", Type: wasm.NewFuncExternType(wasm.TypeIdx{})})
	remaining = MaxModuleBytes
	if err := admitPublicBranchHints(&imported, true, &remaining); !errors.Is(err, ErrUnsupported) {
		t.Fatal("hint targeting an imported function admitted", err)
	}
}
