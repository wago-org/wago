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

const rv64I31OfficialGlobalInitializerHex = "0061736d01000000018580808000016000017f028a808080000103656e760167037f000382808080000100068880808000016c002300fb1c0b078780808000010367657400000a8c80808000018680808000002301fb1e0b"

func rv64I31OfficialGlobalInitializerBytes(t testing.TB) []byte {
	t.Helper()
	b, err := hex.DecodeString(rv64I31OfficialGlobalInitializerHex)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestRV64I31OfficialGlobalInitializerAdmissionAndImage(t *testing.T) {
	source := rv64I31OfficialGlobalInitializerBytes(t)
	wantSource := [sha256.Size]byte{0x02, 0x4b, 0x9a, 0x33, 0x4c, 0x9a, 0x7b, 0xb6, 0x93, 0x32, 0x43, 0xfa, 0x1e, 0xaf, 0x60, 0xed, 0x65, 0x33, 0x38, 0x03, 0x4b, 0xfa, 0xf9, 0xe5, 0x4f, 0xb2, 0x3c, 0xcc, 0x13, 0xc9, 0xad, 0x87}
	if len(source) != 88 || sha256.Sum256(source) != wantSource {
		t.Fatal("official i31 global-global initializer source identity changed")
	}
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	if !admitRV64I31OfficialGlobalInitializer(m) || !admitRV64I31Core(m) || !admitRV64I31CoreWire(source) || !AdmitI31CoreSource(source) {
		t.Fatal("official i31 global-global initializer classifiers rejected")
	}
	productionOptions := CompileOptions{DeferCodeMapping: true, Interruptible: true, InvocationContext: true, ForeignTableCalls: true}
	if _, err = CompileModuleWith(m, productionOptions); !errors.Is(err, ErrUnsupported) {
		t.Fatal("decoded initializer shape escaped exact-wire provenance", err)
	}
	productionOptions.I31CoreSource = true
	compiled, err := CompileModuleWith(m, productionOptions)
	if err != nil {
		t.Fatal("production compile", err)
	}
	wantDigest := [sha256.Size]byte{0xaa, 0x0b, 0xe9, 0x11, 0x63, 0x9f, 0xa4, 0x57, 0x90, 0x15, 0x35, 0xfc, 0xb1, 0x6f, 0xd6, 0xde, 0x01, 0x08, 0x5d, 0x8e, 0x56, 0x1f, 0x5b, 0xee, 0x2d, 0x5b, 0x57, 0x0a, 0x20, 0x2e, 0xf6, 0xd5}
	if len(compiled.Code) != 688 || sha256.Sum256(compiled.Code) != wantDigest || !slices.Equal(compiled.Entry, []int{0}) || !slices.Equal(compiled.InternalEntry, []int{0}) {
		t.Fatalf("official i31 global-global initializer image identity = %d/%x entries %v/%v", len(compiled.Code), sha256.Sum256(compiled.Code), compiled.Entry, compiled.InternalEntry)
	}

	nearWire := slices.Concat(source[:43], []byte{5, 1, 0}, source[43:])
	nearModule, err := wasm.DecodeModule(nearWire)
	if err != nil {
		t.Fatal("decode inert-section near miss", err)
	}
	if !admitRV64I31OfficialGlobalInitializer(nearModule) || admitRV64I31CoreWire(nearWire) || AdmitI31CoreSource(nearWire) {
		t.Fatal("inert-section near miss lost decoded/wire distinction")
	}
	productionOptions.I31CoreSource = false
	if _, err = CompileModuleWith(nearModule, productionOptions); !errors.Is(err, ErrUnsupported) {
		t.Fatal("inert-section near miss escaped provenance", err)
	}

	for name, mutate := range map[string]func(*wasm.Module){
		"body":          func(v *wasm.Module) { v.Code[0].BodyBytes[4] = 0x1d },
		"import-name":   func(v *wasm.Module) { v.Imports[0].Name = "g_x" },
		"import-type":   func(v *wasm.Module) { v.Imports[0].Type = wasm.NewGlobalExternType(wasm.GlobalType{Type: wasm.I64}) },
		"global-type":   func(v *wasm.Module) { v.Globals[0].Type.Type = wasm.AnyRef },
		"initializer":   func(v *wasm.Module) { v.Globals[0].Init.BodyBytes[1] = 1 },
		"export":        func(v *wasm.Module) { v.Exports[0].Name = "get_x" },
		"function-type": func(v *wasm.Module) { v.Types[0].SubTypes[0].Comp.Results[0] = wasm.I64 },
	} {
		candidate, err := wasm.DecodeModule(slices.Clone(source))
		if err != nil {
			t.Fatal(err)
		}
		mutate(candidate)
		if admitRV64I31OfficialGlobalInitializer(candidate) || admitRV64I31Core(candidate) {
			t.Fatal("decoded near miss admitted", name)
		}
		productionOptions.I31CoreSource = true
		if _, err = CompileModuleWith(candidate, productionOptions); !errors.Is(err, ErrUnsupported) {
			t.Fatal("decoded near miss escaped provenance", name, err)
		}
	}
}
