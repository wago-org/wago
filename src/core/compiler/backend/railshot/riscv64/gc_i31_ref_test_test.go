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

const rv64I31OfficialRefTestHex = "0061736d01000000018a80808000026000017f60017f017f038880808000070000010101010107ba8080800007096e756c6c2d646174610000086e756c6c2d693331000103693331000203616e790003026571000406737472756374000505617272617900060a9381808000078d8080800000d06efb146ed06efb156e6a0b8d8080800000d06cfb146cd06cfb156c6a0b9180808000002000fb1cfb146c2000fb1cfb156c6a0b9180808000002000fb1cfb146e2000fb1cfb156e6a0b9180808000002000fb1cfb146d2000fb1cfb156d6a0b9180808000002000fb1cfb146b2000fb1cfb156b6a0b9180808000002000fb1cfb146a2000fb1cfb156a6a0b"

func rv64I31OfficialRefTestBytes(t testing.TB) []byte {
	t.Helper()
	b, err := hex.DecodeString(rv64I31OfficialRefTestHex)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestRV64I31OfficialRefTestAdmissionAndImage(t *testing.T) {
	source := rv64I31OfficialRefTestBytes(t)
	wantSource := [sha256.Size]byte{0x15, 0xae, 0x51, 0xeb, 0x55, 0x7d, 0xb9, 0x16, 0x94, 0xfc, 0x8c, 0xb2, 0xe2, 0xca, 0x14, 0x87, 0x92, 0xee, 0xc4, 0xae, 0x2b, 0x52, 0x4c, 0x61, 0x0e, 0xdf, 0x80, 0x05, 0xa0, 0x48, 0x37, 0xec}
	if len(source) != 255 || sha256.Sum256(source) != wantSource {
		t.Fatalf("i31 ref.test source identity changed: len=%d sha=%x", len(source), sha256.Sum256(source))
	}
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	if !admitRV64I31OfficialRefTest(m) || !admitRV64I31Core(m) || !admitRV64I31CoreWire(source) || !AdmitI31CoreSource(source) {
		t.Fatal("i31 ref.test classifiers rejected")
	}
	opts := CompileOptions{DeferCodeMapping: true, Interruptible: true, InvocationContext: true, ForeignTableCalls: true}
	if _, err = CompileModuleWith(m, opts); !errors.Is(err, ErrUnsupported) {
		t.Fatal("decoded ref.test shape escaped exact-wire provenance", err)
	}
	opts.I31CoreSource = true
	compiled, err := CompileModuleWith(m, opts)
	if err != nil {
		t.Fatal("production compile", err)
	}
	wantDigest := [sha256.Size]byte{0x26, 0xe0, 0x8e, 0xf3, 0x90, 0xe5, 0x32, 0x53, 0x68, 0x07, 0xef, 0x9e, 0x58, 0xa2, 0xd8, 0x07, 0xc5, 0xd2, 0x30, 0x88, 0x21, 0xc0, 0x47, 0x5a, 0x10, 0x09, 0xd6, 0x8d, 0x80, 0x56, 0xc5, 0x24}
	wantEntry := []int{0, 688, 1376, 2112, 2848, 3584, 4304}
	if len(compiled.Code) != 5024 || sha256.Sum256(compiled.Code) != wantDigest || !slices.Equal(compiled.Entry, wantEntry) || !slices.Equal(compiled.InternalEntry, wantEntry) {
		t.Fatalf("i31 ref.test image identity = %d/%x entries %v/%v", len(compiled.Code), sha256.Sum256(compiled.Code), compiled.Entry, compiled.InternalEntry)
	}

	// Insert an empty memory section between the function and export sections.
	// Decoding intentionally discards it, while the exact-wire witness does not.
	nearWire := slices.Concat(source[:38], []byte{5, 1, 0}, source[38:])
	nearModule, err := wasm.DecodeModule(nearWire)
	if err != nil {
		t.Fatal("decode inert-section near miss", err)
	}
	if !admitRV64I31OfficialRefTest(nearModule) || admitRV64I31CoreWire(nearWire) || AdmitI31CoreSource(nearWire) {
		t.Fatal("inert-section near miss lost decoded/wire distinction")
	}
	opts.I31CoreSource = false
	if _, err = CompileModuleWith(nearModule, opts); !errors.Is(err, ErrUnsupported) {
		t.Fatal("inert-section near miss escaped provenance", err)
	}

	for name, mutate := range map[string]func(*wasm.Module){
		"body":          func(v *wasm.Module) { v.Code[3].BodyBytes[6] = 0x6c },
		"export":        func(v *wasm.Module) { v.Exports[0].Name = "null-x" },
		"function-type": func(v *wasm.Module) { v.Types[1].SubTypes[0].Comp.Results[0] = wasm.I64 },
	} {
		candidate, err := wasm.DecodeModule(slices.Clone(source))
		if err != nil {
			t.Fatal(err)
		}
		mutate(candidate)
		if admitRV64I31OfficialRefTest(candidate) || admitRV64I31Core(candidate) {
			t.Fatal("decoded near miss admitted", name)
		}
		opts.I31CoreSource = true
		if _, err = CompileModuleWith(candidate, opts); !errors.Is(err, ErrUnsupported) {
			t.Fatal("decoded near miss escaped provenance", name, err)
		}
	}
}
