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

func TestRV64GCArrayInitElemHelperImage(t *testing.T) {
	source, err := hex.DecodeString(rv64GCArrayInitElemHex)
	if err != nil {
		t.Fatal(err)
	}
	if len(source) != 268 || sha256.Sum256(source) != ([sha256.Size]byte{0x77, 0x15, 0x3c, 0xc9, 0x16, 0x6a, 0x1b, 0x88, 0xe5, 0x64, 0xa9, 0x3e, 0x47, 0x3e, 0x2d, 0x4d, 0x31, 0x97, 0x92, 0x88, 0xac, 0x4b, 0x82, 0xb9, 0xb0, 0x03, 0x89, 0x11, 0xcd, 0x15, 0x98, 0x3b}) {
		t.Fatal("array.init_elem source identity changed")
	}
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	if !admitRV64GCArrayInitElemWire(source) || !AdmitGCArrayInitElemSource(source) || !admitRV64GCArrayInitElem(m) {
		t.Fatal("array.init_elem identity rejected")
	}
	metadata, err := frontend.BuildGCTypeMetadata(m)
	if err != nil {
		t.Fatal(err)
	}
	opts := CompileOptions{DeferCodeMapping: true, Interruptible: true, InvocationContext: true, ForeignTableCalls: true, GCArrayHelpers: true, SyncHostSlots: 6, GCArrayInitElemSource: true, Codegen: codegen.Options{Module: codegen.ModuleInfo{GCTypeDescs: metadata.Descs, GCTypeLayouts: metadata.Layouts}}}
	withoutWire := opts
	withoutWire.GCArrayInitElemSource = false
	if image, err := CompileModuleWith(m, withoutWire); image != nil || !errors.Is(err, ErrUnsupported) {
		t.Fatal("array.init_elem escaped exact wire provenance", err)
	}
	for name, mutate := range map[string]func(*wasm.Module){
		"body": func(v *wasm.Module) {
			v.Code[3].BodyBytes = slices.Clone(v.Code[3].BodyBytes)
			v.Code[3].BodyBytes[8] ^= 1
		},
		"type":          func(v *wasm.Module) { v.Types[2].SubTypes[0].Final = false },
		"table":         func(v *wasm.Module) { v.Tables[0].Type.Limits.Min++ },
		"global-type":   func(v *wasm.Module) { v.Globals[1].Type.Mutable = true },
		"global-body":   func(v *wasm.Module) { v.Globals[0].Init.BodyBytes = []byte{0xd2, 1, 0x41, 12, 0xfb, 6, 1, 0x0b} },
		"element-mode":  func(v *wasm.Module) { v.Elements[0].Mode.Kind = wasm.ElemDeclarative },
		"element-count": func(v *wasm.Module) { v.Elements[0].Kind.Funcs = v.Elements[0].Kind.Funcs[:11] },
		"element-value": func(v *wasm.Module) { v.Elements[0].Kind.Funcs[7] = 1 },
		"export":        func(v *wasm.Module) { v.Exports[0].Name = "call" },
	} {
		t.Run("decoded-"+name, func(t *testing.T) {
			candidate, err := wasm.DecodeModule(slices.Clone(source))
			if err != nil {
				t.Fatal(err)
			}
			mutate(candidate)
			if admitRV64GCArrayInitElem(candidate) {
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
		"wrong-descriptor":    func(v *CompileOptions) { v.Codegen.Module.GCTypeDescs[2].ElemSize++ },
		"missing-layouts":     func(v *CompileOptions) { v.Codegen.Module.GCTypeLayouts = nil },
		"frame-roots":         func(v *CompileOptions) { v.GCFrameRoots = shared.NewGCModuleFrameRootPlan(len(m.Code)) },
		"slot-capacity":       func(v *CompileOptions) { v.SyncHostSlots = 5 },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := opts
			candidate.Codegen.Module.GCTypeDescs = slices.Clone(opts.Codegen.Module.GCTypeDescs)
			candidate.Codegen.Module.GCTypeLayouts = slices.Clone(opts.Codegen.Module.GCTypeLayouts)
			mutate(&candidate)
			if image, err := CompileModuleWith(m, candidate); image != nil || !errors.Is(err, ErrUnsupported) {
				t.Fatal("array.init_elem escaped exact collector metadata", err)
			}
		})
	}
	image, err := CompileModuleWith(m, opts)
	if err != nil {
		t.Fatal(err)
	}
	wantDigest := [sha256.Size]byte{0x4c, 0xf6, 0xda, 0x07, 0x79, 0x85, 0x04, 0x63, 0xe4, 0x66, 0x81, 0x00, 0xa5, 0xf4, 0x44, 0xdc, 0xeb, 0x59, 0x3a, 0xec, 0x2e, 0x93, 0x2d, 0x28, 0x88, 0x27, 0xce, 0xd3, 0xb4, 0xc7, 0x36, 0x07}
	wantEntry := []int{0, 576, 4016, 4736, 5488}
	if image == nil || len(image.Code) != 6920 || sha256.Sum256(image.Code) != wantDigest || !slices.Equal(image.Entry, wantEntry) || !slices.Equal(image.InternalEntry, wantEntry) {
		t.Fatal("array.init_elem helper image metadata", image)
	}
}
