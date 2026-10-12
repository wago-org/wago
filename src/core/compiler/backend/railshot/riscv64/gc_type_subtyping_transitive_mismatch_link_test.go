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

func TestRV64GCTypeSubtypingTransitiveMismatchLinkAdmissionIsExact(t *testing.T) {
	wantSources := [][sha256.Size]byte{
		{0xaf, 0x07, 0xe7, 0xf6, 0x58, 0x6d, 0xb5, 0x7d, 0x5a, 0x92, 0x7c, 0x9c, 0x7f, 0x02, 0x5b, 0x12, 0x16, 0x57, 0xf0, 0x42, 0x37, 0xdc, 0xee, 0x03, 0x44, 0xe7, 0xdb, 0x26, 0x37, 0x98, 0xcf, 0xea},
		{0x9e, 0x70, 0x3d, 0xc0, 0xad, 0xdc, 0x7e, 0xc3, 0x5b, 0xc2, 0x5d, 0x91, 0x90, 0x31, 0x31, 0xc8, 0x15, 0xc5, 0x12, 0x73, 0x6e, 0xda, 0x1a, 0xe0, 0xf1, 0x67, 0x8e, 0xf8, 0xa7, 0xa4, 0x01, 0x44},
	}
	wantSizes := []int{87, 56}
	for i, wire := range rv64GCTypeSubtypingTransitiveMismatchLinkWires {
		variant := uint8(i + 1)
		data := bytes.Clone(wire)
		if gotHash, gotVariant := sha256.Sum256(data), GCTypeSubtypingTransitiveMismatchLinkSourceVariant(data); len(data) != wantSizes[i] || gotHash != wantSources[i] || gotVariant != variant {
			t.Fatalf("variant %d source identity changed: size=%d hash=%x want=%x classifier=%d", variant, len(data), gotHash, wantSources[i], gotVariant)
		}
		m, err := wasm.DecodeModule(data)
		if err != nil || admitRV64GCTypeSubtypingTransitiveMismatchLink(m) != variant {
			t.Fatalf("variant %d decoded shape not admitted: %v", variant, err)
		}
		metadata, err := frontend.BuildGCTypeMetadata(m)
		if err != nil {
			t.Fatal(err)
		}
		opts := CompileOptions{
			DeferCodeMapping: true, Interruptible: true, InvocationContext: true, ForeignTableCalls: true,
			GCTypeSubtypingTransitiveMismatchSource: variant,
			Codegen:                                 codegen.Options{Module: codegen.ModuleInfo{GCTypeDescs: metadata.Descs, GCTypeLayouts: metadata.Layouts}},
		}
		if variant == rv64GCTypeSubtypingTransitiveMismatchLinkConsumerVariant {
			opts.ImportBindings = []ImportBinding{{Dynamic: true}}
		}
		compiled, err := CompileModuleWith(m, opts)
		if err != nil {
			t.Fatalf("variant %d compile: %v", variant, err)
		}
		if variant == rv64GCTypeSubtypingTransitiveMismatchLinkProviderVariant {
			wantCode := [sha256.Size]byte{0x1d, 0xf4, 0x1d, 0xb6, 0xda, 0x6c, 0xf7, 0x49, 0x1e, 0x95, 0x1a, 0x23, 0xe0, 0x52, 0x51, 0xba, 0xc1, 0x48, 0x0b, 0xe5, 0xaa, 0x36, 0xe1, 0x01, 0x5d, 0x3c, 0xd9, 0x75, 0x4c, 0x4d, 0x65, 0x03}
			if len(compiled.Code) != 576 || sha256.Sum256(compiled.Code) != wantCode || !slices.Equal(compiled.Entry, []int{0}) || !slices.Equal(compiled.InternalEntry, []int{0}) || compiled.CodeImage != nil {
				t.Fatalf("provider image is incomplete: code=%d/%x entries=%v/%v image=%v", len(compiled.Code), sha256.Sum256(compiled.Code), compiled.Entry, compiled.InternalEntry, compiled.CodeImage)
			}
		} else if len(compiled.Code) != 0 || len(compiled.Entry) != 0 || len(compiled.InternalEntry) != 0 || compiled.CodeImage != nil {
			t.Fatalf("consumer must remain zero-code: code=%d entries=%v/%v image=%v", len(compiled.Code), compiled.Entry, compiled.InternalEntry, compiled.CodeImage)
		}
		withoutWire := opts
		withoutWire.GCTypeSubtypingTransitiveMismatchSource = 0
		if _, err = CompileModuleWith(m, withoutWire); !errors.Is(err, ErrUnsupported) {
			t.Fatal("transitive-mismatch-link product compiled without exact wire provenance", variant, err)
		}
		wrongWire := opts
		wrongWire.GCTypeSubtypingTransitiveMismatchSource = variant%uint8(len(rv64GCTypeSubtypingTransitiveMismatchLinkWires)) + 1
		if _, err = CompileModuleWith(m, wrongWire); !errors.Is(err, ErrUnsupported) {
			t.Fatal("transitive-mismatch-link product compiled with wrong wire provenance", variant, err)
		}
		for j := range data {
			mutated := bytes.Clone(data)
			mutated[j] ^= 1
			got := GCTypeSubtypingTransitiveMismatchLinkSourceVariant(mutated)
			if got != 0 && (int(got) > len(rv64GCTypeSubtypingTransitiveMismatchLinkWires) || !bytes.Equal(mutated, rv64GCTypeSubtypingTransitiveMismatchLinkWires[got-1])) {
				t.Fatalf("variant %d wire mutation %d was admitted outside the exact source set as variant %d", variant, j, got)
			}
		}
	}
}
