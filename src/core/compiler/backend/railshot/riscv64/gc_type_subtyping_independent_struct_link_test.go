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

func TestRV64GCTypeSubtypingIndependentStructLinkAdmissionIsExact(t *testing.T) {
	wantSources := [][sha256.Size]byte{
		{0x7c, 0x8a, 0xf0, 0x76, 0x5c, 0x2e, 0x2d, 0x43, 0xa0, 0x7e, 0x7a, 0x6a, 0x75, 0xa8, 0x5d, 0x39, 0x65, 0x31, 0x82, 0x7c, 0x1b, 0x2c, 0xb4, 0x40, 0x2a, 0x24, 0x27, 0x73, 0x08, 0x78, 0x1d, 0xff},
		{0xa5, 0x93, 0xd0, 0xdb, 0x0e, 0x5f, 0x17, 0x3a, 0xaa, 0xc2, 0xd6, 0x00, 0x7b, 0x84, 0xa4, 0xb2, 0x68, 0xd7, 0xad, 0x20, 0x47, 0xa4, 0xe8, 0xcd, 0x8f, 0xe3, 0xa2, 0x75, 0xef, 0x9b, 0x08, 0x20},
	}
	wantSizes := []int{82, 63}
	for i, wire := range rv64GCTypeSubtypingIndependentStructLinkWires {
		variant := uint8(i + 1)
		data := bytes.Clone(wire)
		if gotHash, gotVariant := sha256.Sum256(data), GCTypeSubtypingIndependentStructLinkSourceVariant(data); len(data) != wantSizes[i] || gotHash != wantSources[i] || gotVariant != variant {
			t.Fatalf("variant %d source identity changed: size=%d hash=%x want=%x classifier=%d", variant, len(data), gotHash, wantSources[i], gotVariant)
		}
		m, err := wasm.DecodeModule(data)
		if err != nil || admitRV64GCTypeSubtypingIndependentStructLink(m) != variant {
			t.Fatalf("variant %d decoded shape not admitted: %v", variant, err)
		}
		metadata, err := frontend.BuildGCTypeMetadata(m)
		if err != nil {
			t.Fatal(err)
		}
		opts := CompileOptions{DeferCodeMapping: true, Interruptible: true, InvocationContext: true, ForeignTableCalls: true, GCTypeSubtypingIndependentStructSource: variant, Codegen: codegen.Options{Module: codegen.ModuleInfo{GCTypeDescs: metadata.Descs, GCTypeLayouts: metadata.Layouts}}}
		if variant == rv64GCTypeSubtypingIndependentStructLinkConsumerVariant {
			opts.ImportBindings = []ImportBinding{{Dynamic: true}}
		}
		compiled, err := CompileModuleWith(m, opts)
		if err != nil {
			t.Fatalf("variant %d compile: %v", variant, err)
		}
		if variant == rv64GCTypeSubtypingIndependentStructLinkProviderVariant {
			wantCode := [sha256.Size]byte{0x1d, 0xf4, 0x1d, 0xb6, 0xda, 0x6c, 0xf7, 0x49, 0x1e, 0x95, 0x1a, 0x23, 0xe0, 0x52, 0x51, 0xba, 0xc1, 0x48, 0x0b, 0xe5, 0xaa, 0x36, 0xe1, 0x01, 0x5d, 0x3c, 0xd9, 0x75, 0x4c, 0x4d, 0x65, 0x03}
			if len(compiled.Code) != 576 || sha256.Sum256(compiled.Code) != wantCode || !slices.Equal(compiled.Entry, []int{0}) || !slices.Equal(compiled.InternalEntry, []int{0}) || compiled.CodeImage != nil {
				t.Fatalf("provider image is incomplete: code=%d/%x entries=%v/%v image=%v", len(compiled.Code), sha256.Sum256(compiled.Code), compiled.Entry, compiled.InternalEntry, compiled.CodeImage)
			}
		} else if len(compiled.Code) != 0 || len(compiled.Entry) != 0 || len(compiled.InternalEntry) != 0 || compiled.CodeImage != nil {
			t.Fatalf("consumer must remain zero-code: code=%d entries=%v/%v image=%v", len(compiled.Code), compiled.Entry, compiled.InternalEntry, compiled.CodeImage)
		}
		withoutWire := opts
		withoutWire.GCTypeSubtypingIndependentStructSource = 0
		if _, err = CompileModuleWith(m, withoutWire); !errors.Is(err, ErrUnsupported) {
			t.Fatal("independent-struct-link product compiled without exact wire provenance", variant, err)
		}
		wrongWire := opts
		wrongWire.GCTypeSubtypingIndependentStructSource = variant%uint8(len(rv64GCTypeSubtypingIndependentStructLinkWires)) + 1
		if _, err = CompileModuleWith(m, wrongWire); !errors.Is(err, ErrUnsupported) {
			t.Fatal("independent-struct-link product compiled with wrong wire provenance", variant, err)
		}
		for j := range data {
			mutated := bytes.Clone(data)
			mutated[j] ^= 1
			got := GCTypeSubtypingIndependentStructLinkSourceVariant(mutated)
			if got != 0 && (int(got) > len(rv64GCTypeSubtypingIndependentStructLinkWires) || !bytes.Equal(mutated, rv64GCTypeSubtypingIndependentStructLinkWires[got-1])) {
				t.Fatalf("variant %d wire mutation %d admitted outside exact source set as variant %d", variant, j, got)
			}
		}
	}
}
