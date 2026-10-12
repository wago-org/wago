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

func TestRV64GCTypeSubtypingFinalityLinkAdmissionIsExact(t *testing.T) {
	wantSources := [][sha256.Size]byte{
		{0xdc, 0xf5, 0x44, 0x59, 0xe9, 0xf3, 0x90, 0x87, 0xc6, 0x97, 0xc9, 0xd9, 0xed, 0xc0, 0x95, 0x5a, 0xab, 0xc0, 0x2e, 0xb2, 0x8e, 0x40, 0xb6, 0x5c, 0x84, 0x29, 0x1c, 0xbe, 0x19, 0x4a, 0x95, 0x62},
		{0xea, 0x96, 0x0d, 0xde, 0xc4, 0xf2, 0x4c, 0x95, 0x2d, 0x26, 0xee, 0x7a, 0x56, 0x73, 0x09, 0xa4, 0x1c, 0x58, 0x95, 0xcf, 0x84, 0x69, 0x0c, 0xa1, 0x20, 0xd4, 0x57, 0x7b, 0xb4, 0xc2, 0x6e, 0x08},
		{0x7f, 0xc4, 0x3b, 0xbb, 0xff, 0x42, 0xca, 0x92, 0x3d, 0xb1, 0x60, 0x4d, 0x03, 0x39, 0xca, 0xdd, 0x21, 0x45, 0x8f, 0x56, 0x71, 0xea, 0x79, 0x62, 0xd0, 0x31, 0x78, 0x6e, 0x93, 0x51, 0x79, 0x96},
	}
	wantSizes := []int{70, 38, 38}
	for i, wire := range rv64GCTypeSubtypingFinalityLinkWires {
		variant := uint8(i + 1)
		data := bytes.Clone(wire)
		if gotHash, gotVariant := sha256.Sum256(data), GCTypeSubtypingFinalityLinkSourceVariant(data); len(data) != wantSizes[i] || gotHash != wantSources[i] || gotVariant != variant {
			t.Fatalf("variant %d source identity changed: size=%d hash=%x want=%x classifier=%d", variant, len(data), gotHash, wantSources[i], gotVariant)
		}
		m, err := wasm.DecodeModule(data)
		if err != nil || admitRV64GCTypeSubtypingFinalityLink(m) != variant {
			t.Fatalf("variant %d decoded shape not admitted: %v", variant, err)
		}
		metadata, err := frontend.BuildGCTypeMetadata(m)
		if err != nil {
			t.Fatal(err)
		}
		opts := CompileOptions{
			DeferCodeMapping: true, Interruptible: true, InvocationContext: true, ForeignTableCalls: true,
			GCTypeSubtypingFinalityLinkSource: variant,
			Codegen:                           codegen.Options{Module: codegen.ModuleInfo{GCTypeDescs: metadata.Descs, GCTypeLayouts: metadata.Layouts}},
		}
		if variant != rv64GCTypeSubtypingFinalityLinkProviderVariant {
			opts.ImportBindings = []ImportBinding{{Dynamic: true}}
		}
		compiled, err := CompileModuleWith(m, opts)
		if err != nil {
			t.Fatalf("variant %d compile: %v", variant, err)
		}
		if variant == rv64GCTypeSubtypingFinalityLinkProviderVariant {
			wantCode := [sha256.Size]byte{0x26, 0x6b, 0x8e, 0x30, 0xd1, 0x62, 0xca, 0x13, 0x66, 0x7f, 0x1d, 0x65, 0x6c, 0x04, 0xee, 0xe4, 0x7d, 0x45, 0x41, 0xbc, 0x17, 0x96, 0xb3, 0x41, 0x26, 0xa3, 0x32, 0x1f, 0x2b, 0xe5, 0x4c, 0x0b}
			if len(compiled.Code) != 1152 || sha256.Sum256(compiled.Code) != wantCode || !slices.Equal(compiled.Entry, []int{0, 576}) || !slices.Equal(compiled.InternalEntry, []int{0, 576}) || compiled.CodeImage != nil {
				t.Fatalf("provider image is incomplete: code=%d/%x entries=%v/%v image=%v", len(compiled.Code), sha256.Sum256(compiled.Code), compiled.Entry, compiled.InternalEntry, compiled.CodeImage)
			}
		} else if len(compiled.Code) != 0 || len(compiled.Entry) != 0 || len(compiled.InternalEntry) != 0 || compiled.CodeImage != nil {
			t.Fatalf("consumer must remain zero-code: code=%d entries=%v/%v image=%v", len(compiled.Code), compiled.Entry, compiled.InternalEntry, compiled.CodeImage)
		}
		withoutWire := opts
		withoutWire.GCTypeSubtypingFinalityLinkSource = 0
		if _, err = CompileModuleWith(m, withoutWire); !errors.Is(err, ErrUnsupported) {
			t.Fatal("finality-link product compiled without exact wire provenance", variant, err)
		}
		wrongWire := opts
		wrongWire.GCTypeSubtypingFinalityLinkSource = variant%uint8(len(rv64GCTypeSubtypingFinalityLinkWires)) + 1
		if _, err = CompileModuleWith(m, wrongWire); !errors.Is(err, ErrUnsupported) {
			t.Fatal("finality-link product compiled with wrong wire provenance", variant, err)
		}
		for j := range data {
			mutated := bytes.Clone(data)
			mutated[j] ^= 1
			got := GCTypeSubtypingFinalityLinkSourceVariant(mutated)
			if got != 0 && (int(got) > len(rv64GCTypeSubtypingFinalityLinkWires) || !bytes.Equal(mutated, rv64GCTypeSubtypingFinalityLinkWires[got-1])) {
				t.Fatalf("variant %d wire mutation %d was admitted outside the exact source set as variant %d", variant, j, got)
			}
		}
	}
}
