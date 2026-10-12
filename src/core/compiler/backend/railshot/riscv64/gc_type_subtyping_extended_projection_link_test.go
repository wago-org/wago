//go:build linux && !tinygo

package riscv64

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"slices"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/codegen"
	"github.com/wago-org/wago/src/core/compiler/frontend"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestRV64GCTypeSubtypingExtendedProjectionLinkAdmissionIsExact(t *testing.T) {
	wantSources := [][sha256.Size]byte{
		{0x4b, 0x62, 0x73, 0x8d, 0x11, 0x27, 0x0b, 0x2a, 0xc5, 0xb4, 0x3e, 0x5d, 0xe7, 0xc8, 0x10, 0x5d, 0xda, 0xc4, 0x49, 0xd9, 0xf2, 0x3d, 0xe0, 0x9c, 0xe4, 0x41, 0x60, 0x99, 0x6f, 0x30, 0x2d, 0x62},
		{0x59, 0x92, 0xb9, 0x26, 0xa2, 0xf2, 0xed, 0x28, 0xc4, 0xe6, 0xd5, 0x14, 0x9d, 0x97, 0xff, 0x07, 0x5c, 0x0e, 0x6f, 0xdb, 0xcc, 0x9f, 0xb8, 0xec, 0x81, 0x94, 0xfa, 0x96, 0x27, 0x14, 0x05, 0xdb},
	}
	wantSizes := []int{114, 102}
	for i, wire := range rv64GCTypeSubtypingExtendedProjectionLinkWires {
		variant := uint8(i + 1)
		data := bytes.Clone(wire)
		if gotHash, gotVariant := sha256.Sum256(data), GCTypeSubtypingExtendedProjectionLinkSourceVariant(data); len(data) != wantSizes[i] || gotHash != wantSources[i] || gotVariant != variant {
			t.Fatalf("variant %d source identity changed: size=%d hash=%x want=%x classifier=%d", variant, len(data), gotHash, wantSources[i], gotVariant)
		}
		m, err := wasm.DecodeModule(data)
		if err != nil || admitRV64GCTypeSubtypingExtendedProjectionLink(m) != variant {
			t.Fatalf("variant %d decoded shape not admitted: %v", variant, err)
		}
		metadata, err := frontend.BuildGCTypeMetadata(m)
		if err != nil {
			t.Fatal(err)
		}
		opts := CompileOptions{DeferCodeMapping: true, Interruptible: true, InvocationContext: true, ForeignTableCalls: true, GCTypeSubtypingExtendedProjectionSource: variant, Codegen: codegen.Options{Module: codegen.ModuleInfo{GCTypeDescs: metadata.Descs, GCTypeLayouts: metadata.Layouts}}}
		if variant == rv64GCTypeSubtypingExtendedProjectionLinkConsumerVariant {
			opts.ImportBindings = []ImportBinding{{Dynamic: true}, {Dynamic: true, ImportIndex: 1}}
		}
		compiled, err := CompileModuleWith(m, opts)
		if err != nil {
			t.Fatalf("variant %d compile: %v", variant, err)
		}
		if variant == rv64GCTypeSubtypingExtendedProjectionLinkProviderVariant {
			wantCode := [sha256.Size]byte{0x1d, 0xf4, 0x1d, 0xb6, 0xda, 0x6c, 0xf7, 0x49, 0x1e, 0x95, 0x1a, 0x23, 0xe0, 0x52, 0x51, 0xba, 0xc1, 0x48, 0x0b, 0xe5, 0xaa, 0x36, 0xe1, 0x01, 0x5d, 0x3c, 0xd9, 0x75, 0x4c, 0x4d, 0x65, 0x03}
			if len(compiled.Code) != 576 || sha256.Sum256(compiled.Code) != wantCode || !slices.Equal(compiled.Entry, []int{0}) || !slices.Equal(compiled.InternalEntry, []int{0}) || compiled.CodeImage != nil {
				t.Fatalf("provider image incomplete: code=%d/%x entries=%v/%v image=%v", len(compiled.Code), sha256.Sum256(compiled.Code), compiled.Entry, compiled.InternalEntry, compiled.CodeImage)
			}
		} else if len(compiled.Code) != 0 || len(compiled.Entry) != 0 || len(compiled.InternalEntry) != 0 || compiled.CodeImage != nil {
			t.Fatalf("consumer must remain zero-code: code=%d entries=%v/%v image=%v", len(compiled.Code), compiled.Entry, compiled.InternalEntry, compiled.CodeImage)
		}
		withoutWire := opts
		withoutWire.GCTypeSubtypingExtendedProjectionSource = 0
		if _, err = CompileModuleWith(m, withoutWire); !errors.Is(err, ErrUnsupported) {
			t.Fatal("extended-projection product compiled without exact provenance", variant, err)
		}
		wrongWire := opts
		wrongWire.GCTypeSubtypingExtendedProjectionSource = variant%uint8(len(rv64GCTypeSubtypingExtendedProjectionLinkWires)) + 1
		if _, err = CompileModuleWith(m, wrongWire); !errors.Is(err, ErrUnsupported) {
			t.Fatal("extended-projection product compiled with wrong provenance", variant, err)
		}
		for j := range data {
			mutated := bytes.Clone(data)
			mutated[j] ^= 1
			got := GCTypeSubtypingExtendedProjectionLinkSourceVariant(mutated)
			if got != 0 && (int(got) > len(rv64GCTypeSubtypingExtendedProjectionLinkWires) || !bytes.Equal(mutated, rv64GCTypeSubtypingExtendedProjectionLinkWires[got-1])) {
				t.Fatalf("variant %d mutation %d admitted outside exact source set as %d", variant, j, got)
			}
		}
	}
}
