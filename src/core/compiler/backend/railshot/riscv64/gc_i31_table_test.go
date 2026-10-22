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

const rv64I31OfficialTableHex = "0061736d01000000019680808000046000017f60017f017f60027f7f017f60037f7f7f0003878080800006000102030303048580808000016c01030a07aa80808000060473697a6500000367657400010467726f7700020466696c6c000304636f7079000404696e6974000509af8080800002060041000b6c0341e707fb1c0b41f806fb1c0b418906fb1c0b056c0341fb00fb1c0b41c803fb1c0b419506fb1c0b0adc8080800006858080800000fc10000b88808080000020002500fb1e0b8b80808000002001fb1c2000fc0f000b8d808080000020002001fb1c2002fc11000b8c8080800000200020012002fc0e00000b8c8080800000200020012002fc0c01000b"

func rv64I31OfficialTableBytes(t testing.TB) []byte {
	t.Helper()
	b, err := hex.DecodeString(rv64I31OfficialTableHex)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestRV64I31OfficialTableAdmissionAndImage(t *testing.T) {
	source := rv64I31OfficialTableBytes(t)
	wantSource := [sha256.Size]byte{0xc2, 0xa2, 0x06, 0x20, 0x22, 0xd3, 0xb9, 0x9a, 0x27, 0xaa, 0xda, 0x76, 0xd1, 0xfe, 0x14, 0xcd, 0xac, 0xf3, 0x38, 0x7e, 0x74, 0x97, 0x94, 0x3d, 0x17, 0xd3, 0x68, 0x1f, 0x65, 0xed, 0x73, 0x29}
	if len(source) != 259 || sha256.Sum256(source) != wantSource {
		t.Fatal("official i31 table source identity changed")
	}
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	if !admitRV64I31OfficialTable(m) || !admitRV64I31Core(m) || !admitRV64I31CoreWire(source) || !AdmitI31CoreSource(source) {
		t.Fatal("official i31 table classifiers rejected")
	}
	productionOptions := CompileOptions{DeferCodeMapping: true, Interruptible: true, InvocationContext: true, ForeignTableCalls: true}
	if _, err = CompileModuleWith(m, productionOptions); !errors.Is(err, ErrUnsupported) {
		t.Fatal("decoded table shape escaped exact-wire provenance", err)
	}
	productionOptions = authenticateI31TestModule(t, m, productionOptions)
	compiled, err := CompileModuleWith(m, productionOptions)
	if err != nil {
		t.Fatal("production compile", err)
	}
	wantDigest := [sha256.Size]byte{0xf6, 0xf3, 0x62, 0x07, 0xa9, 0x73, 0xb1, 0x78, 0xfd, 0xad, 0xda, 0x46, 0xed, 0xe6, 0xce, 0x23, 0x94, 0xc7, 0x8a, 0x66, 0x58, 0xc7, 0x82, 0xf3, 0xd0, 0x24, 0x04, 0x7e, 0x37, 0xa9, 0x17, 0x1d}
	wantEntries := []int{0, 608, 1456, 2384, 3360, 4608}
	if len(compiled.Code) != 5776 || sha256.Sum256(compiled.Code) != wantDigest || !slices.Equal(compiled.Entry, wantEntries) || !slices.Equal(compiled.InternalEntry, wantEntries) {
		t.Fatalf("official i31 table image identity = %d/%x entries %v/%v", len(compiled.Code), sha256.Sum256(compiled.Code), compiled.Entry, compiled.InternalEntry)
	}

	nearWire := slices.Concat(source[:60], []byte{5, 1, 0}, source[60:])
	nearModule, err := wasm.DecodeModule(nearWire)
	if err != nil {
		t.Fatal("decode inert-section near miss", err)
	}
	if !admitRV64I31OfficialTable(nearModule) || !admitRV64I31CoreWire(nearWire) || !AdmitI31CoreSource(nearWire) {
		t.Fatal("valid inert section was rejected by general immediate-reference admission")
	}
	productionOptions.I31CoreSource = false
	if _, err = CompileModuleWith(nearModule, productionOptions); !errors.Is(err, ErrUnsupported) {
		t.Fatal("inert-section near miss escaped provenance", err)
	}

	for name, mutate := range map[string]func(*wasm.Module){
		"body":    func(v *wasm.Module) { v.Code[1].BodyBytes[5] = 0x1d },
		"table":   func(v *wasm.Module) { v.Tables[0].Type.Limits.Max = 11 },
		"element": func(v *wasm.Module) { v.Elements[1].Kind.Exprs[0].BodyBytes[1] = 0xfa },
		"export":  func(v *wasm.Module) { v.Exports[5].Name = "init_x" },
	} {
		candidate, err := wasm.DecodeModule(slices.Clone(source))
		if err != nil {
			t.Fatal(err)
		}
		mutate(candidate)
		if admitRV64I31OfficialTable(candidate) {
			t.Fatal("decoded near miss admitted", name)
		}
		checkGeneralI31Module(t, candidate, productionOptions)
	}
}
