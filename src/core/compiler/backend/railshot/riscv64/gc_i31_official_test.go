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

const rv64I31OfficialCoreHex = "0061736d010000000199808080000560017f01646c60017f017f6000017f6000027f7f60017f00038880808000070001010202030406918080800002646c004102fb1c0b646c014103fb1c0b07cc8080800007036e65770000056765745f750001056765745f7300020a6765745f752d6e756c6c00030a6765745f732d6e756c6c00040b6765745f676c6f62616c7300050a7365745f676c6f62616c00060ad880808000078680808000002000fb1c0b8880808000002000fb1cfb1e0b8880808000002000fb1cfb1d0b868080800000d06cfb1e0b868080800000d06cfb1d0b8a80808000002300fb1e2301fb1e0b8880808000002000fb1c24010b"

func rv64I31OfficialCoreBytes(t testing.TB) []byte {
	t.Helper()
	data, err := hex.DecodeString(rv64I31OfficialCoreHex)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestRV64I31OfficialCoreAdmissionAndImage(t *testing.T) {
	source := rv64I31OfficialCoreBytes(t)
	wantSource := [sha256.Size]byte{0x4b, 0xdd, 0x4d, 0x0f, 0x18, 0x6a, 0x2f, 0xd6, 0x17, 0xb4, 0x1a, 0xd4, 0x94, 0x0e, 0x17, 0xf2, 0xc0, 0x41, 0x55, 0x14, 0xeb, 0xc6, 0x36, 0xa5, 0x6e, 0x41, 0x49, 0x6e, 0x8c, 0x39, 0x2a, 0xea}
	if len(source) != 252 || sha256.Sum256(source) != wantSource {
		t.Fatal("official i31 source identity changed")
	}
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	if !admitRV64I31OfficialCore(m) || !admitRV64I31Core(m) || !admitRV64I31CoreWire(source) || !AdmitI31CoreSource(source) {
		t.Fatal("official i31 classifiers rejected")
	}
	_, err = compileDecodedModule(m, scalarRegisters[:], CompileOptions{production: true, publicStarts: true, Interruptible: true, ForeignTableCalls: true, InvocationContext: true, i31Core: true})
	if err != nil {
		t.Fatal(err)
	}
	productionOptions := CompileOptions{DeferCodeMapping: true, Interruptible: true, InvocationContext: true, ForeignTableCalls: true}
	if _, err = CompileModuleWith(m, productionOptions); !errors.Is(err, ErrUnsupported) {
		t.Fatal("decoded shape escaped exact-wire provenance", err)
	}
	productionOptions = authenticateI31TestModule(t, m, productionOptions)
	compiled, err := CompileModuleWith(m, productionOptions)
	if err != nil {
		t.Fatal("production compile", err)
	}
	wantDigest := [sha256.Size]byte{0x1a, 0x1f, 0xe4, 0x7e, 0x21, 0xfc, 0x84, 0x40, 0x83, 0x14, 0xbb, 0x42, 0x0b, 0xf4, 0x8e, 0xd0, 0x35, 0xff, 0x0b, 0x0b, 0x6a, 0x5c, 0xa1, 0x70, 0x1c, 0xf1, 0x2c, 0x21, 0xac, 0x78, 0xc6, 0x27}
	wantEntries := []int{0, 640, 1360, 2080, 2768, 3456, 4240}
	if len(compiled.Code) != 4872 || sha256.Sum256(compiled.Code) != wantDigest || !slices.Equal(compiled.Entry, wantEntries) || !slices.Equal(compiled.InternalEntry, wantEntries) {
		t.Fatalf("official i31 image identity = %d/%x entries %v/%v", len(compiled.Code), sha256.Sum256(compiled.Code), compiled.Entry, compiled.InternalEntry)
	}

	// An inert empty table section is erased by decoding. The exact recognizer
	// stays closed, while the independent general immediate proof accepts it.
	nearWire := slices.Concat(source[:53], []byte{4, 1, 0}, source[53:])
	nearModule, err := wasm.DecodeModule(nearWire)
	if err != nil {
		t.Fatal("decode inert-section near miss", err)
	}
	if !admitRV64I31OfficialCore(nearModule) || admitExactProductWireShape(nearWire, nil) || !AdmitI31CoreSource(nearWire) {
		t.Fatal("inert-section near miss lost decoded/wire distinction")
	}
	productionOptions.I31CoreSource = false
	if _, err = CompileModuleWith(nearModule, productionOptions); !errors.Is(err, ErrUnsupported) {
		t.Fatal("inert-section near miss escaped provenance", err)
	}

	checkGeneralI31Wire(t, nearWire)

	for name, mutate := range map[string]func(*wasm.Module){
		"body":   func(v *wasm.Module) { v.Code[1].BodyBytes[5] = 0x1d },
		"global": func(v *wasm.Module) { v.Globals[1].Type.Mutable = false },
		"export": func(v *wasm.Module) { v.Exports[6].Name = "set_global_x" },
	} {
		candidate, err := wasm.DecodeModule(slices.Clone(source))
		if err != nil {
			t.Fatal(err)
		}
		mutate(candidate)
		if admitRV64I31OfficialCore(candidate) {
			t.Fatal("decoded near miss admitted", name)
		}
		checkGeneralI31Module(t, candidate, productionOptions)
	}
}
