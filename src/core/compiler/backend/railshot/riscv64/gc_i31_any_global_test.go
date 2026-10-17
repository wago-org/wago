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

const rv64I31OfficialAnyGlobalHex = "0061736d01000000018a80808000026000027f7f60017f00038380808000020001069180808000026e0041d209fb1c0b6e0141ae2cfb1c0b079c80808000020b6765745f676c6f62616c7300000a7365745f676c6f62616c00010aa380808000029080808000002300fb176cfb1e2301fb176cfb1e0b8880808000002000fb1c24010b"

func rv64I31OfficialAnyGlobalBytes(t testing.TB) []byte {
	t.Helper()
	b, err := hex.DecodeString(rv64I31OfficialAnyGlobalHex)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestRV64I31OfficialAnyGlobalAdmissionAndImage(t *testing.T) {
	source := rv64I31OfficialAnyGlobalBytes(t)
	wantSource := [sha256.Size]byte{0x75, 0x7d, 0x32, 0x66, 0x61, 0x7e, 0xa9, 0x01, 0x22, 0x1f, 0xac, 0xbd, 0xa9, 0xb6, 0x60, 0xd6, 0xd1, 0xfd, 0x52, 0xad, 0xf4, 0x92, 0xff, 0xa8, 0x2b, 0xa0, 0x65, 0x8d, 0xc8, 0x46, 0xb2, 0x6d}
	if len(source) != 131 || sha256.Sum256(source) != wantSource {
		t.Fatal("official i31 anyref-global source identity changed")
	}
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	if !admitRV64I31OfficialAnyGlobal(m) || !admitRV64I31Core(m) || !admitRV64I31CoreWire(source) || !AdmitI31CoreSource(source) {
		t.Fatal("official i31 anyref-global classifiers rejected")
	}
	productionOptions := CompileOptions{DeferCodeMapping: true, Interruptible: true, InvocationContext: true, ForeignTableCalls: true}
	if _, err = CompileModuleWith(m, productionOptions); !errors.Is(err, ErrUnsupported) {
		t.Fatal("decoded anyref-global shape escaped exact-wire provenance", err)
	}
	productionOptions = authenticateI31TestModule(t, m, productionOptions)
	compiled, err := CompileModuleWith(m, productionOptions)
	if err != nil {
		t.Fatal("production compile", err)
	}
	wantDigest := [sha256.Size]byte{0xe1, 0xcc, 0x81, 0x73, 0x72, 0x8a, 0xfe, 0xa4, 0xb3, 0x52, 0x20, 0x6c, 0xa0, 0x67, 0x3c, 0xf2, 0xa8, 0xca, 0xc3, 0x70, 0x7f, 0x57, 0x52, 0x6b, 0x87, 0xad, 0x22, 0xb3, 0x06, 0xc6, 0xfc, 0x2d}
	if len(compiled.Code) != 1592 || sha256.Sum256(compiled.Code) != wantDigest || !slices.Equal(compiled.Entry, []int{0, 960}) || !slices.Equal(compiled.InternalEntry, []int{0, 960}) {
		t.Fatalf("official i31 anyref-global image identity = %d/%x entries %v/%v", len(compiled.Code), sha256.Sum256(compiled.Code), compiled.Entry, compiled.InternalEntry)
	}

	nearWire := slices.Concat(source[:33], []byte{5, 1, 0}, source[33:])
	nearModule, err := wasm.DecodeModule(nearWire)
	if err != nil {
		t.Fatal("decode inert-section near miss", err)
	}
	if !admitRV64I31OfficialAnyGlobal(nearModule) || !admitRV64I31CoreWire(nearWire) || !AdmitI31CoreSource(nearWire) {
		t.Fatal("valid inert section was rejected by general immediate-reference admission")
	}
	productionOptions.I31CoreSource = false
	if _, err = CompileModuleWith(nearModule, productionOptions); !errors.Is(err, ErrUnsupported) {
		t.Fatal("inert-section near miss escaped provenance", err)
	}

	for name, mutate := range map[string]func(*wasm.Module){
		"body":           func(v *wasm.Module) { v.Code[0].BodyBytes[4] = 0x6b },
		"global-type":    func(v *wasm.Module) { v.Globals[0].Type.Type = wasm.EqRef },
		"global-mutable": func(v *wasm.Module) { v.Globals[0].Type.Mutable = true },
		"initializer":    func(v *wasm.Module) { v.Globals[0].Init.BodyBytes[1]++ },
		"export":         func(v *wasm.Module) { v.Exports[0].Name = "get_x" },
		"function-type":  func(v *wasm.Module) { v.Types[0].SubTypes[0].Comp.Results[0] = wasm.I64 },
	} {
		candidate, err := wasm.DecodeModule(slices.Clone(source))
		if err != nil {
			t.Fatal(err)
		}
		mutate(candidate)
		if admitRV64I31OfficialAnyGlobal(candidate) {
			t.Fatal("decoded near miss admitted", name)
		}
		checkGeneralI31Module(t, candidate, productionOptions)
	}
}
