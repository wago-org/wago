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

const rv64GCArrayDefaultOfficialHex = "0061736d0100000001b080808000095e7d005e7d01600001640060027f6400017d60017f017d60037f64017d017d60027f7d017d6001646a017f6000017f038880808000070203040506070806988080800002640000430000803f4103fb06000b6400004103fb07000b079d8080800004036e65770000036765740002077365745f6765740004036c656e00060ae780808000078780808000004103fb07000b89808080000020012000fb0b000b8880808000002000100010010b928080800000200120002002fb0e0120012000fb0b010b8d808080000020004103fb0701200110030b8680808000002000fb0f0b868080800000100010050b"

func TestRV64GCArrayDefaultOfficialHelperImage(t *testing.T) {
	source, err := hex.DecodeString(rv64GCArrayDefaultOfficialHex)
	if err != nil {
		t.Fatal(err)
	}
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	if !admitRV64GCArrayDefaultOfficialWire(source) || !AdmitGCArrayDefaultOfficialSource(source) || !admitRV64GCArrayDefaultOfficial(m) {
		t.Fatal("gc array default official identity rejected")
	}
	metadata, err := frontend.BuildGCTypeMetadata(m)
	if err != nil {
		t.Fatal(err)
	}
	opts := CompileOptions{
		DeferCodeMapping: true, Interruptible: true, InvocationContext: true, ForeignTableCalls: true,
		GCArrayHelpers: true, SyncHostSlots: 5, GCArrayDefaultOfficialSource: true,
		Codegen: codegen.Options{Module: codegen.ModuleInfo{GCTypeDescs: metadata.Descs, GCTypeLayouts: metadata.Layouts}},
	}
	withoutWire := opts
	withoutWire.GCArrayDefaultOfficialSource = false
	if image, err := CompileModuleWith(m, withoutWire); image != nil || !errors.Is(err, ErrUnsupported) {
		t.Fatal("gc array default official escaped exact wire provenance", err)
	}
	nearWire := slices.Concat(source[:76], []byte{5, 1, 0}, source[76:])
	nearModule, err := wasm.DecodeModule(nearWire)
	if err != nil {
		t.Fatal("decode inert-section near miss", err)
	}
	if !admitRV64GCArrayDefaultOfficial(nearModule) || admitRV64GCArrayDefaultOfficialWire(nearWire) || AdmitGCArrayDefaultOfficialSource(nearWire) {
		t.Fatal("inert-section near miss lost decoded/wire distinction")
	}
	if image, err := CompileModuleWith(nearModule, withoutWire); image != nil || !errors.Is(err, ErrUnsupported) {
		t.Fatal("inert-section near miss escaped provenance", err)
	}
	for name, mutate := range map[string]func(*wasm.Module){
		"body": func(v *wasm.Module) {
			v.Code[4].BodyBytes = slices.Clone(v.Code[4].BodyBytes)
			v.Code[4].BodyBytes[4] ^= 1
		},
		"type":   func(v *wasm.Module) { v.Types[1].SubTypes[0].Final = false },
		"global": func(v *wasm.Module) { v.Globals[1].Type.Mutable = true },
		"global-init": func(v *wasm.Module) {
			v.Globals[1].Init.BodyBytes = slices.Clone(v.Globals[1].Init.BodyBytes)
			v.Globals[1].Init.BodyBytes[1]++
		},
		"export": func(v *wasm.Module) { v.Exports[0].Name = "make" },
	} {
		t.Run("decoded-"+name, func(t *testing.T) {
			candidate, err := wasm.DecodeModule(slices.Clone(source))
			if err != nil {
				t.Fatal(err)
			}
			mutate(candidate)
			if admitRV64GCArrayDefaultOfficial(candidate) {
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
				t.Fatal("gc array default official escaped exact collector metadata", err)
			}
		})
	}
	image, err := CompileModuleWith(m, opts)
	if err != nil {
		t.Fatal(err)
	}
	wantDigest := [sha256.Size]byte{0x7e, 0x5e, 0xed, 0x16, 0x14, 0xb9, 0x8d, 0xd5, 0x4f, 0xf0, 0x90, 0xc7, 0x8b, 0xf3, 0xc4, 0x2f, 0x45, 0x30, 0x1b, 0x4c, 0x31, 0xe2, 0x30, 0x11, 0x5a, 0x69, 0xf5, 0xc5, 0xd4, 0x89, 0xff, 0xaf}
	wantEntry := []int{0, 672, 1392, 2128, 2944, 3728, 4416}
	if image == nil || len(image.Code) != 6172 || sha256.Sum256(image.Code) != wantDigest || !slices.Equal(image.Entry, wantEntry) || !slices.Equal(image.InternalEntry, wantEntry) {
		t.Fatal("gc array default official helper image metadata", image)
	}
}
