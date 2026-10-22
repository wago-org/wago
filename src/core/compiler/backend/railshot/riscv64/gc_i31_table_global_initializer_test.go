//go:build linux && !tinygo

package riscv64

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"slices"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

const rv64I31OfficialTableGlobalInitializerHex = "0061736d010000000186808080000160017f017f028a808080000103656e760167037f000382808080000100048d80808000014000646c0103032300fb1c0b078780808000010367657400000a8e808080000188808080000020002500fb1e0b"

func rv64I31OfficialTableGlobalInitializerBytes(t testing.TB) []byte {
	t.Helper()
	b, err := hex.DecodeString(rv64I31OfficialTableGlobalInitializerHex)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestRV64I31OfficialTableGlobalInitializerAdmissionAndImage(t *testing.T) {
	source := rv64I31OfficialTableGlobalInitializerBytes(t)
	wantSource := [sha256.Size]byte{0x0a, 0x26, 0xe5, 0x0d, 0x6e, 0xc8, 0xcc, 0xba, 0xf1, 0xcc, 0x3a, 0x59, 0xfc, 0x2e, 0x1b, 0xe6, 0xdc, 0xa2, 0xc2, 0x22, 0x19, 0xba, 0x70, 0xca, 0x80, 0xa0, 0x14, 0x51, 0x88, 0x2b, 0xf0, 0xe4}
	if len(source) != 96 || sha256.Sum256(source) != wantSource {
		t.Fatal("official i31 table-global initializer source identity changed")
	}
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	if !admitRV64I31OfficialTableGlobalInitializer(m) || !admitRV64I31Core(m) || !admitRV64I31CoreWire(source) || !AdmitI31CoreSource(source) {
		t.Fatal("official i31 table-global initializer classifiers rejected")
	}
	productionOptions := CompileOptions{DeferCodeMapping: true, Interruptible: true, InvocationContext: true, ForeignTableCalls: true}
	if _, err = CompileModuleWith(m, productionOptions); !errors.Is(err, ErrUnsupported) {
		t.Fatal("decoded initializer shape escaped exact-wire provenance", err)
	}
	productionOptions = authenticateI31TestModule(t, m, productionOptions)
	compiled, err := CompileModuleWith(m, productionOptions)
	if err != nil {
		t.Fatal("production compile", err)
	}
	wantDigest := [sha256.Size]byte{0x85, 0x9f, 0xb4, 0x09, 0x53, 0xeb, 0xac, 0x16, 0x55, 0xf4, 0x66, 0xe7, 0x2f, 0xa9, 0xa8, 0x7a, 0x8c, 0x12, 0xf0, 0x28, 0xc7, 0x10, 0x6e, 0x69, 0x46, 0x7d, 0x8c, 0x73, 0x49, 0xc9, 0xcf, 0xb2}
	if len(compiled.Code) != 840 || sha256.Sum256(compiled.Code) != wantDigest || !slices.Equal(compiled.Entry, []int{0}) || !slices.Equal(compiled.InternalEntry, []int{0}) {
		t.Fatalf("official i31 table-global initializer image identity = %d/%x entries %v/%v", len(compiled.Code), sha256.Sum256(compiled.Code), compiled.Entry, compiled.InternalEntry)
	}

	nearWire := slices.Concat(source[:63], []byte{5, 1, 0}, source[63:])
	nearModule, err := wasm.DecodeModule(nearWire)
	if err != nil {
		t.Fatal("decode inert-section near miss", err)
	}
	if !admitRV64I31OfficialTableGlobalInitializer(nearModule) || !admitRV64I31CoreWire(nearWire) || !AdmitI31CoreSource(nearWire) {
		t.Fatal("valid inert section was rejected by general immediate-reference admission")
	}
	productionOptions.I31CoreSource = false
	if _, err = CompileModuleWith(nearModule, productionOptions); !errors.Is(err, ErrUnsupported) {
		t.Fatal("inert-section near miss escaped provenance", err)
	}

	for name, mutate := range map[string]func(*wasm.Module){
		"body":          func(v *wasm.Module) { v.Code[0].BodyBytes[5] = 0x1d },
		"import-name":   func(v *wasm.Module) { v.Imports[0].Name = "g_x" },
		"import-type":   func(v *wasm.Module) { v.Imports[0].Type = wasm.NewGlobalExternType(wasm.GlobalType{Type: wasm.I64}) },
		"table":         func(v *wasm.Module) { v.Tables[0].Type.Limits.Max = 4 },
		"table-type":    func(v *wasm.Module) { v.Tables[0].Type.Ref = wasm.ExternRef.Ref() },
		"initializer":   func(v *wasm.Module) { v.Tables[0].Init.BodyBytes[1] = 1 },
		"export":        func(v *wasm.Module) { v.Exports[0].Name = "get_x" },
		"function-type": func(v *wasm.Module) { v.Types[0].SubTypes[0].Comp.Results[0] = wasm.I64 },
	} {
		candidate, err := wasm.DecodeModule(slices.Clone(source))
		if err != nil {
			t.Fatal(err)
		}
		mutate(candidate)
		if admitRV64I31OfficialTableGlobalInitializer(candidate) {
			t.Fatal("decoded near miss admitted", name)
		}
		checkGeneralI31Module(t, candidate, productionOptions)
	}
}
