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

const rv64I31OfficialAnyTableHex = "0061736d01000000019680808000046000017f60017f017f60027f7f017f60037f7f7f0003878080800006000102030303048580808000016e01030a07aa80808000060473697a6500000367657400010467726f7700020466696c6c000304636f7079000404696e6974000509af8080800002060041000b6c0341e707fb1c0b41f806fb1c0b418906fb1c0b056c0341fb00fb1c0b41c803fb1c0b419506fb1c0b0adf8080800006858080800000fc10000b8b808080000020002500fb176cfb1e0b8b80808000002001fb1c2000fc0f000b8d808080000020002001fb1c2002fc11000b8c8080800000200020012002fc0e00000b8c8080800000200020012002fc0c01000b"

func rv64I31OfficialAnyTableBytes(t testing.TB) []byte {
	t.Helper()
	b, err := hex.DecodeString(rv64I31OfficialAnyTableHex)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestRV64I31OfficialAnyTableAdmissionAndImage(t *testing.T) {
	source := rv64I31OfficialAnyTableBytes(t)
	wantSource := [sha256.Size]byte{0x57, 0x23, 0x87, 0xa2, 0xc9, 0xd7, 0xea, 0x91, 0x12, 0xf3, 0x94, 0x00, 0x25, 0xb7, 0xc5, 0x70, 0x41, 0xcd, 0x94, 0x78, 0x18, 0x5e, 0xd7, 0xe3, 0x2b, 0xb9, 0x3a, 0x01, 0xfb, 0xfa, 0x5a, 0x69}
	if len(source) != 262 || sha256.Sum256(source) != wantSource {
		t.Fatal("official i31 anyref-table source identity changed")
	}
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	if !admitRV64I31OfficialAnyTable(m) || !admitRV64I31Core(m) || !admitRV64I31TableProduct(m) || !admitRV64I31CoreWire(source) || !AdmitI31CoreSource(source) {
		t.Fatal("official i31 anyref-table classifiers rejected")
	}
	productionOptions := CompileOptions{DeferCodeMapping: true, Interruptible: true, InvocationContext: true, ForeignTableCalls: true}
	if _, err = CompileModuleWith(m, productionOptions); !errors.Is(err, ErrUnsupported) {
		t.Fatal("decoded anyref-table shape escaped exact-wire provenance", err)
	}
	productionOptions = authenticateI31TestModule(t, m, productionOptions)
	compiled, err := CompileModuleWith(m, productionOptions)
	if err != nil {
		t.Fatal("production compile", err)
	}
	wantDigest := [sha256.Size]byte{0x55, 0xb9, 0xc0, 0x28, 0x2a, 0x13, 0xbf, 0xc3, 0x59, 0xf9, 0xdf, 0x20, 0xf9, 0x6a, 0x2c, 0xb3, 0x77, 0x1d, 0x7a, 0xba, 0xb0, 0x65, 0xdd, 0x47, 0x47, 0x32, 0x36, 0x1a, 0x17, 0xad, 0xa6, 0x81}
	wantEntry := []int{0, 608, 1536, 2464, 3440, 4688}
	if len(compiled.Code) != 5856 || sha256.Sum256(compiled.Code) != wantDigest || !slices.Equal(compiled.Entry, wantEntry) || !slices.Equal(compiled.InternalEntry, wantEntry) {
		t.Fatalf("official i31 anyref-table image identity = %d/%x entries %v/%v", len(compiled.Code), sha256.Sum256(compiled.Code), compiled.Entry, compiled.InternalEntry)
	}

	nearWire := slices.Concat(source[:60], []byte{5, 1, 0}, source[60:])
	nearModule, err := wasm.DecodeModule(nearWire)
	if err != nil {
		t.Fatal("decode inert-section near miss", err)
	}
	if !admitRV64I31OfficialAnyTable(nearModule) || !admitRV64I31CoreWire(nearWire) || !AdmitI31CoreSource(nearWire) {
		t.Fatal("valid inert section was rejected by general immediate-reference admission")
	}
	productionOptions.I31CoreSource = false
	if _, err = CompileModuleWith(nearModule, productionOptions); !errors.Is(err, ErrUnsupported) {
		t.Fatal("inert-section near miss escaped provenance", err)
	}

	for name, mutate := range map[string]func(*wasm.Module){
		"body":         func(v *wasm.Module) { v.Code[1].BodyBytes[7] = 0x6b },
		"table-type":   func(v *wasm.Module) { v.Tables[0].Type.Ref = wasm.I31Ref.Ref() },
		"element-type": func(v *wasm.Module) { v.Elements[0].Kind.Ref = wasm.AnyRef.Ref() },
		"element-value": func(v *wasm.Module) {
			v.Elements[1].Kind.Exprs[2].BodyBytes[1]++
		},
		"export":        func(v *wasm.Module) { v.Exports[1].Name = "get_x" },
		"function-type": func(v *wasm.Module) { v.Types[1].SubTypes[0].Comp.Results[0] = wasm.I64 },
	} {
		candidate, err := wasm.DecodeModule(slices.Clone(source))
		if err != nil {
			t.Fatal(err)
		}
		mutate(candidate)
		if admitRV64I31OfficialAnyTable(candidate) {
			t.Fatal("decoded near miss admitted", name)
		}
		checkGeneralI31Module(t, candidate, productionOptions)
	}
}
