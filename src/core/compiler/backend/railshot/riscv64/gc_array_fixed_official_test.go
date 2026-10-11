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

const rv64GCArrayFixedOfficialHex = "0061736d0100000001b080808000095e7d005e7d01600001640060027f6400017d60017f017d60037f64017d017d60027f7d017d6001646a017f6000017f038880808000070203040506070806938080800001640000430000803f4300000040fb0800020b079d8080800004036e65770000036765740002077365745f6765740004036c656e00060afe8080800007908080800000430000803f4300000040fb0800020b89808080000020012000fb0b000b8880808000002000100010010b928080800000200120002002fb0e0120012000fb0b010b9b80808000002000430000803f43000000404300004040fb080103200110030b8680808000002000fb0f0b868080800000100010050b"

func TestRV64GCArrayFixedOfficialHelperImage(t *testing.T) {
	source, err := hex.DecodeString(rv64GCArrayFixedOfficialHex)
	if err != nil {
		t.Fatal(err)
	}
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	if !admitRV64GCArrayFixedOfficialWire(source) || !AdmitGCArrayFixedOfficialSource(source) || !admitRV64GCArrayFixedOfficial(m) {
		t.Fatal("gc array fixed official identity rejected")
	}
	metadata, err := frontend.BuildGCTypeMetadata(m)
	if err != nil {
		t.Fatal(err)
	}
	opts := CompileOptions{
		DeferCodeMapping: true, Interruptible: true, InvocationContext: true, ForeignTableCalls: true,
		GCArrayHelpers: true, SyncHostSlots: 5, GCArrayFixedOfficialSource: true,
		Codegen: codegen.Options{Module: codegen.ModuleInfo{GCTypeDescs: metadata.Descs, GCTypeLayouts: metadata.Layouts}},
	}
	withoutWire := opts
	withoutWire.GCArrayFixedOfficialSource = false
	if image, err := CompileModuleWith(m, withoutWire); image != nil || !errors.Is(err, ErrUnsupported) {
		t.Fatal("gc array fixed official escaped exact wire provenance", err)
	}
	nearWire := slices.Concat(source[:76], []byte{5, 1, 0}, source[76:])
	nearModule, err := wasm.DecodeModule(nearWire)
	if err != nil {
		t.Fatal("decode inert-section near miss", err)
	}
	if !admitRV64GCArrayFixedOfficial(nearModule) || admitRV64GCArrayFixedOfficialWire(nearWire) || AdmitGCArrayFixedOfficialSource(nearWire) {
		t.Fatal("inert-section near miss lost decoded/wire distinction")
	}
	if image, err := CompileModuleWith(nearModule, withoutWire); image != nil || !errors.Is(err, ErrUnsupported) {
		t.Fatal("inert-section near miss escaped provenance", err)
	}
	for name, mutate := range map[string]func(*wasm.Module){
		"body": func(v *wasm.Module) {
			v.Code[4].BodyBytes = slices.Clone(v.Code[4].BodyBytes)
			v.Code[4].BodyBytes[6] ^= 1
		},
		"type":   func(v *wasm.Module) { v.Types[1].SubTypes[0].Final = false },
		"global": func(v *wasm.Module) { v.Globals[0].Type.Mutable = true },
		"export": func(v *wasm.Module) { v.Exports[0].Name = "make" },
	} {
		t.Run("decoded-"+name, func(t *testing.T) {
			candidate, err := wasm.DecodeModule(slices.Clone(source))
			if err != nil {
				t.Fatal(err)
			}
			mutate(candidate)
			if admitRV64GCArrayFixedOfficial(candidate) {
				t.Fatal("decoded near miss admitted")
			}
			if image, err := CompileModuleWith(candidate, opts); image != nil || !errors.Is(err, ErrUnsupported) {
				t.Fatal("decoded near miss escaped provenance", err)
			}
		})
	}
	for name, mutate := range map[string]func(*CompileOptions){
		"struct-helper":       func(candidate *CompileOptions) { candidate.GCStructHelpers = true },
		"missing-descriptors": func(candidate *CompileOptions) { candidate.Codegen.Module.GCTypeDescs = nil },
		"wrong-descriptor":    func(candidate *CompileOptions) { candidate.Codegen.Module.GCTypeDescs[1].ElemSize++ },
		"missing-layouts":     func(candidate *CompileOptions) { candidate.Codegen.Module.GCTypeLayouts = nil },
		"wrong-layout": func(candidate *CompileOptions) {
			candidate.Codegen.Module.GCTypeLayouts[1].PointerFree = !candidate.Codegen.Module.GCTypeLayouts[1].PointerFree
		},
		"frame-roots":   func(candidate *CompileOptions) { candidate.GCFrameRoots = shared.NewGCModuleFrameRootPlan(len(m.Code)) },
		"slot-capacity": func(candidate *CompileOptions) { candidate.SyncHostSlots = 4 },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := opts
			candidate.Codegen.Module.GCTypeDescs = slices.Clone(opts.Codegen.Module.GCTypeDescs)
			candidate.Codegen.Module.GCTypeLayouts = slices.Clone(opts.Codegen.Module.GCTypeLayouts)
			mutate(&candidate)
			if image, err := CompileModuleWith(m, candidate); image != nil || !errors.Is(err, ErrUnsupported) {
				t.Fatal("gc array fixed official escaped exact collector metadata", err)
			}
		})
	}
	image, err := CompileModuleWith(m, opts)
	if err != nil {
		t.Fatal(err)
	}
	wantDigest := [sha256.Size]byte{0xef, 0x84, 0xbb, 0x7b, 0x69, 0x10, 0x6b, 0x24, 0x8f, 0xd8, 0x23, 0xcf, 0xb8, 0xc3, 0x70, 0xda, 0xe7, 0x6a, 0xdf, 0x9a, 0x44, 0x63, 0x27, 0xdc, 0x16, 0x01, 0xee, 0x72, 0xd7, 0x3f, 0x3d, 0x88}
	wantEntry := []int{0, 704, 1424, 2160, 2976, 3936, 4624}
	if image == nil || len(image.Code) != 6684 || sha256.Sum256(image.Code) != wantDigest || !slices.Equal(image.Entry, wantEntry) || !slices.Equal(image.InternalEntry, wantEntry) {
		t.Fatal("gc array fixed official helper image metadata", image)
	}
}
