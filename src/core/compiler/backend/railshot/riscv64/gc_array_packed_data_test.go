//go:build linux && !tinygo

package riscv64

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"slices"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/codegen"
	"github.com/wago-org/wago/src/core/compiler/frontend"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

const rv64GCArrayPackedDataHex = "0061736d0100000001b3808080000a5e78005e7801600001640060027f6400017f60017f017f60037f64017f017f60027f7f017f6001646a017f6000017f600000038c808080000b020203040304050607080907c28080800007036e657700000c6e65772d6f766572666c6f770001056765745f750003056765745f730005077365745f6765740007036c656e00090964726f705f73656773000a0c8180808000010aa9818080000b8a808080000041014103fb0900000b928080800000418080808078418080808078fb0900000b89808080000020012000fb0d000b8880808000002000100010020b89808080000020012000fb0c000b8880808000002000100010040b928080800000200120002002fb0e0120012000fb0d010b908080800000200041014103fb090100200110060b8680808000002000fb0f0b868080800000100010080b858080800000fc09000b0b8880808000010105000102ff04"

func TestRV64GCArrayPackedDataHelperImage(t *testing.T) {
	source, err := hex.DecodeString(rv64GCArrayPackedDataHex)
	if err != nil {
		t.Fatal(err)
	}
	if len(source) != 351 || sha256.Sum256(source) != ([sha256.Size]byte{0x7f, 0xc4, 0xaf, 0xb6, 0xa2, 0xe3, 0xb2, 0xf6, 0xb1, 0x56, 0x2b, 0x4d, 0x01, 0x85, 0xb6, 0xd5, 0xd4, 0x42, 0x6c, 0x57, 0x9b, 0xcd, 0xa4, 0x4c, 0xce, 0x3b, 0x3a, 0x14, 0x01, 0x24, 0x7b, 0xce}) {
		t.Fatal("packed data source identity changed")
	}
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	if !admitRV64GCArrayPackedDataWire(source) || !AdmitGCArrayPackedDataSource(source) || !admitRV64GCArrayPackedData(m) {
		t.Fatal("gc array packed data identity rejected")
	}
	metadata, err := frontend.BuildGCTypeMetadata(m)
	if err != nil {
		t.Fatal(err)
	}
	opts := CompileOptions{DeferCodeMapping: true, Interruptible: true, InvocationContext: true, ForeignTableCalls: true, GCArrayHelpers: true, SyncHostSlots: 4, GCArrayPackedDataSource: true, Codegen: codegen.Options{Module: codegen.ModuleInfo{GCTypeDescs: metadata.Descs, GCTypeLayouts: metadata.Layouts}}}
	withoutWire := opts
	withoutWire.GCArrayPackedDataSource = false
	if image, err := CompileModuleWith(m, withoutWire); image != nil || !errors.Is(err, ErrUnsupported) {
		t.Fatal("packed data escaped exact wire provenance", err)
	}
	for name, mutate := range map[string]func(*wasm.Module){
		"body": func(v *wasm.Module) {
			v.Code[6].BodyBytes = slices.Clone(v.Code[6].BodyBytes)
			v.Code[6].BodyBytes[8] ^= 1
		},
		"type": func(v *wasm.Module) { v.Types[1].SubTypes[0].Final = false },
		"data": func(v *wasm.Module) {
			v.Data[0].Init = slices.Clone(v.Data[0].Init)
			v.Data[0].Init[2] ^= 1
		},
		"data-memory":      func(v *wasm.Module) { v.Data[0].Mode.Mem = 1 },
		"data-offset-body": func(v *wasm.Module) { v.Data[0].Mode.Offset.BodyBytes = []byte{0x41, 0, 0x0b} },
		"data-offset-instr": func(v *wasm.Module) {
			v.Data[0].Mode.Offset.Instrs = []wasm.Instruction{{Kind: wasm.InstrNop}}
		},
		"export": func(v *wasm.Module) { v.Exports[0].Name = "make" },
	} {
		t.Run("decoded-"+name, func(t *testing.T) {
			candidate, err := wasm.DecodeModule(slices.Clone(source))
			if err != nil {
				t.Fatal(err)
			}
			mutate(candidate)
			if admitRV64GCArrayPackedData(candidate) {
				t.Fatal("decoded near miss admitted")
			}
			if image, err := CompileModuleWith(candidate, opts); image != nil || !errors.Is(err, ErrUnsupported) {
				t.Fatal("decoded near miss escaped provenance", err)
			}
		})
	}
	for name, mutate := range map[string]func(*CompileOptions){
		"struct-helper":       func(v *CompileOptions) { v.GCStructHelpers = true },
		"missing-descriptors": func(v *CompileOptions) { v.Codegen.Module.GCTypeDescs = nil },
		"wrong-descriptor":    func(v *CompileOptions) { v.Codegen.Module.GCTypeDescs[0].ElemSize++ },
		"missing-layouts":     func(v *CompileOptions) { v.Codegen.Module.GCTypeLayouts = nil },
		"frame-roots":         func(v *CompileOptions) { v.GCFrameRoots = shared.NewGCModuleFrameRootPlan(len(m.Code)) },
		"slot-capacity":       func(v *CompileOptions) { v.SyncHostSlots = 3 },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := opts
			candidate.Codegen.Module.GCTypeDescs = slices.Clone(opts.Codegen.Module.GCTypeDescs)
			candidate.Codegen.Module.GCTypeLayouts = slices.Clone(opts.Codegen.Module.GCTypeLayouts)
			mutate(&candidate)
			if image, err := CompileModuleWith(m, candidate); image != nil || !errors.Is(err, ErrUnsupported) {
				t.Fatal("packed data escaped exact collector metadata", err)
			}
		})
	}
	image, err := CompileModuleWith(m, opts)
	if err != nil {
		t.Fatal(err)
	}
	wantDigest := [sha256.Size]byte{0x14, 0xcf, 0xa1, 0x40, 0x4f, 0xc3, 0x21, 0xc8, 0xac, 0x7e, 0x46, 0x35, 0x49, 0x1a, 0x89, 0x82, 0x07, 0xb8, 0x8a, 0x25, 0x89, 0xf9, 0xca, 0x93, 0x36, 0xf3, 0x65, 0x76, 0xb9, 0xe7, 0x3f, 0x2f}
	wantEntry := []int{0, 704, 1424, 2144, 2880, 3600, 4336, 5152, 5968, 6656, 7376}
	if image == nil || len(image.Code) != 9308 || sha256.Sum256(image.Code) != wantDigest || !slices.Equal(image.Entry, wantEntry) || !slices.Equal(image.InternalEntry, wantEntry) {
		t.Fatal("gc array packed data helper image metadata", image)
	}
}
