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

func TestRV64GCTypeSubtypingLinkAdmissionIsExact(t *testing.T) {
	wantSources := [][sha256.Size]byte{
		{0x8e, 0x9b, 0xdb, 0xeb, 0x27, 0xa4, 0x96, 0x32, 0x8e, 0xb9, 0x52, 0x9e, 0x0e, 0xa6, 0x29, 0xd1, 0x4a, 0x01, 0x12, 0x4b, 0x65, 0x7c, 0x4e, 0xb0, 0xaa, 0xd7, 0x4a, 0x8b, 0xd0, 0xf4, 0x26, 0xdb},
		{0xea, 0x4d, 0x5a, 0xaf, 0x13, 0xa9, 0x74, 0x4b, 0xd3, 0x19, 0xa1, 0xb3, 0x3d, 0x1e, 0xe2, 0x30, 0x3c, 0xfa, 0xec, 0xc8, 0x4d, 0xae, 0x73, 0xa4, 0x17, 0x93, 0x51, 0xa6, 0xfb, 0x91, 0xa7, 0x60},
		{0x63, 0x4f, 0x7c, 0xaa, 0x3c, 0x4e, 0x26, 0xb7, 0x57, 0xfc, 0xa7, 0xa9, 0xa9, 0xf8, 0x36, 0x7f, 0x99, 0xe3, 0x33, 0x04, 0xc8, 0x7f, 0x7b, 0x2c, 0xf6, 0xec, 0x7d, 0x1e, 0x31, 0x56, 0x65, 0x35},
		{0xfe, 0x07, 0x22, 0x81, 0x54, 0xa2, 0x7a, 0x9d, 0xe4, 0x70, 0x2a, 0xfb, 0x12, 0x18, 0x77, 0x09, 0x53, 0x6e, 0x89, 0x44, 0x95, 0xe8, 0xfa, 0x2e, 0x34, 0xa7, 0x10, 0xe2, 0xdd, 0x7c, 0x0b, 0x88},
		{0x24, 0xce, 0x2b, 0x2e, 0xec, 0x63, 0x1e, 0xe2, 0x94, 0x6c, 0x64, 0x1e, 0x05, 0x45, 0xd0, 0x6d, 0xc1, 0x17, 0x9e, 0x2e, 0x9b, 0xa6, 0x46, 0xae, 0x59, 0xc5, 0xd3, 0x79, 0x74, 0x11, 0x16, 0x49},
	}
	wantSizes := []int{103, 86, 51, 51, 51}
	for i, wire := range rv64GCTypeSubtypingLinkWires {
		variant := uint8(i + 1)
		data := bytes.Clone(wire)
		if len(data) != wantSizes[i] || sha256.Sum256(data) != wantSources[i] || GCTypeSubtypingLinkSourceVariant(data) != variant {
			t.Fatalf("variant %d source identity changed", variant)
		}
		m, err := wasm.DecodeModule(data)
		if err != nil || admitRV64GCTypeSubtypingLink(m) != variant {
			t.Fatalf("variant %d decoded shape not admitted: %v", variant, err)
		}
		metadata, err := frontend.BuildGCTypeMetadata(m)
		if err != nil {
			t.Fatal(err)
		}
		opts := CompileOptions{
			DeferCodeMapping: true, Interruptible: true, InvocationContext: true, ForeignTableCalls: true,
			GCTypeSubtypingLinkSource: variant,
			Codegen:                   codegen.Options{Module: codegen.ModuleInfo{GCTypeDescs: metadata.Descs, GCTypeLayouts: metadata.Layouts}},
		}
		if variant >= rv64GCTypeSubtypingLinkConsumerVariant {
			imports := 6
			if variant >= rv64GCTypeSubtypingLinkUnlinkableFirst {
				imports = 1
			}
			opts.ImportBindings = make([]ImportBinding, imports)
			for j := range opts.ImportBindings {
				opts.ImportBindings[j] = ImportBinding{Dynamic: true, ImportIndex: uint32(j)}
			}
		}
		compiled, err := CompileModuleWith(m, opts)
		if err != nil {
			t.Fatalf("variant %d compile: %v", variant, err)
		}
		if variant == rv64GCTypeSubtypingLinkProviderVariant {
			wantCode := [sha256.Size]byte{0x83, 0x09, 0x02, 0xc6, 0xd1, 0x4e, 0xb8, 0x41, 0xe6, 0xaf, 0xd9, 0x88, 0x4b, 0x43, 0xe8, 0xbe, 0x55, 0x2d, 0x18, 0xc9, 0xca, 0xda, 0xdc, 0x89, 0xab, 0x99, 0xe4, 0x03, 0xd5, 0x5a, 0xad, 0x48}
			if len(compiled.Code) != 1812 || sha256.Sum256(compiled.Code) != wantCode || !slices.Equal(compiled.Entry, []int{0, 608, 1216}) || !slices.Equal(compiled.InternalEntry, []int{0, 608, 1216}) || compiled.CodeImage != nil {
				t.Fatalf("provider image is incomplete: code=%d/%x entries=%v/%v image=%v", len(compiled.Code), sha256.Sum256(compiled.Code), compiled.Entry, compiled.InternalEntry, compiled.CodeImage)
			}
		} else if len(compiled.Code) != 0 || len(compiled.Entry) != 0 || len(compiled.InternalEntry) != 0 || compiled.CodeImage != nil {
			t.Fatalf("consumer must remain zero-code: code=%d entries=%v/%v image=%v", len(compiled.Code), compiled.Entry, compiled.InternalEntry, compiled.CodeImage)
		}
		withoutWire := opts
		withoutWire.GCTypeSubtypingLinkSource = 0
		if _, err = CompileModuleWith(m, withoutWire); !errors.Is(err, ErrUnsupported) {
			t.Fatal("link product compiled without exact wire provenance", variant, err)
		}
		wrongWire := opts
		wrongWire.GCTypeSubtypingLinkSource = 3 - variant
		if _, err = CompileModuleWith(m, wrongWire); !errors.Is(err, ErrUnsupported) {
			t.Fatal("link product compiled with wrong wire provenance", variant, err)
		}
		for j := range data {
			mutated := bytes.Clone(data)
			mutated[j] ^= 1
			got := GCTypeSubtypingLinkSourceVariant(mutated)
			if got != 0 && (int(got) > len(rv64GCTypeSubtypingLinkWires) || !bytes.Equal(mutated, rv64GCTypeSubtypingLinkWires[got-1])) {
				t.Fatalf("variant %d wire mutation %d was admitted outside the exact source set as variant %d", variant, j, got)
			}
		}
	}
}
