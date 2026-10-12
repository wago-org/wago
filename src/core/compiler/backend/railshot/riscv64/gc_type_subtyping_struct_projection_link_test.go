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

func TestRV64GCTypeSubtypingStructProjectionLinkAdmissionIsExact(t *testing.T) {
	wantSources := [][sha256.Size]byte{
		{0x8d, 0xe4, 0x1f, 0xdb, 0x1e, 0x1b, 0x4e, 0xf5, 0x76, 0x39, 0xe5, 0xa6, 0x34, 0x4e, 0xed, 0x6c, 0x13, 0xbf, 0xb5, 0xad, 0xa5, 0xea, 0x56, 0x43, 0x3b, 0xb2, 0x21, 0xf4, 0x03, 0xc5, 0x6d, 0x8e},
		{0xa5, 0xd3, 0xe6, 0x06, 0x0f, 0x52, 0xfa, 0x0b, 0xec, 0xf6, 0x8e, 0x6e, 0x4d, 0xd0, 0x66, 0x23, 0xdf, 0x6e, 0xcf, 0x7b, 0xf2, 0x2b, 0xfe, 0x54, 0x30, 0xb4, 0x84, 0xf2, 0xad, 0xbd, 0xf0, 0xa2},
	}
	wantSizes := []int{104, 85}
	for i, wire := range rv64GCTypeSubtypingStructProjectionLinkWires {
		variant := uint8(i + 1)
		data := bytes.Clone(wire)
		if gotHash, gotVariant := sha256.Sum256(data), GCTypeSubtypingStructProjectionLinkSourceVariant(data); len(data) != wantSizes[i] || gotHash != wantSources[i] || gotVariant != variant {
			t.Fatalf("variant %d source identity changed: size=%d hash=%x want=%x classifier=%d", variant, len(data), gotHash, wantSources[i], gotVariant)
		}
		m, err := wasm.DecodeModule(data)
		if err != nil || admitRV64GCTypeSubtypingStructProjectionLink(m) != variant {
			t.Fatalf("variant %d decoded shape not admitted: %v", variant, err)
		}
		metadata, err := frontend.BuildGCTypeMetadata(m)
		if err != nil {
			t.Fatal(err)
		}
		opts := CompileOptions{
			DeferCodeMapping: true, Interruptible: true, InvocationContext: true, ForeignTableCalls: true,
			GCTypeSubtypingStructProjectionSource: variant,
			Codegen:                               codegen.Options{Module: codegen.ModuleInfo{GCTypeDescs: metadata.Descs, GCTypeLayouts: metadata.Layouts}},
		}
		if variant == rv64GCTypeSubtypingStructProjectionLinkConsumerVariant {
			opts.ImportBindings = []ImportBinding{{Dynamic: true}}
		}
		compiled, err := CompileModuleWith(m, opts)
		if err != nil {
			t.Fatalf("variant %d compile: %v", variant, err)
		}
		if variant == rv64GCTypeSubtypingStructProjectionLinkProviderVariant {
			wantCode := [sha256.Size]byte{0x1d, 0xf4, 0x1d, 0xb6, 0xda, 0x6c, 0xf7, 0x49, 0x1e, 0x95, 0x1a, 0x23, 0xe0, 0x52, 0x51, 0xba, 0xc1, 0x48, 0x0b, 0xe5, 0xaa, 0x36, 0xe1, 0x01, 0x5d, 0x3c, 0xd9, 0x75, 0x4c, 0x4d, 0x65, 0x03}
			if len(compiled.Code) != 576 || sha256.Sum256(compiled.Code) != wantCode || !slices.Equal(compiled.Entry, []int{0}) || !slices.Equal(compiled.InternalEntry, []int{0}) || compiled.CodeImage != nil {
				t.Fatalf("provider image is incomplete: code=%d/%x entries=%v/%v image=%v", len(compiled.Code), sha256.Sum256(compiled.Code), compiled.Entry, compiled.InternalEntry, compiled.CodeImage)
			}
		} else if len(compiled.Code) != 0 || len(compiled.Entry) != 0 || len(compiled.InternalEntry) != 0 || compiled.CodeImage != nil {
			t.Fatalf("consumer must remain zero-code: code=%d entries=%v/%v image=%v", len(compiled.Code), compiled.Entry, compiled.InternalEntry, compiled.CodeImage)
		}
		withoutWire := opts
		withoutWire.GCTypeSubtypingStructProjectionSource = 0
		if _, err = CompileModuleWith(m, withoutWire); !errors.Is(err, ErrUnsupported) {
			t.Fatal("struct-projection-link product compiled without exact wire provenance", variant, err)
		}
		wrongWire := opts
		wrongWire.GCTypeSubtypingStructProjectionSource = variant%uint8(len(rv64GCTypeSubtypingStructProjectionLinkWires)) + 1
		if _, err = CompileModuleWith(m, wrongWire); !errors.Is(err, ErrUnsupported) {
			t.Fatal("struct-projection-link product compiled with wrong wire provenance", variant, err)
		}
		for j := range data {
			mutated := bytes.Clone(data)
			mutated[j] ^= 1
			got := GCTypeSubtypingStructProjectionLinkSourceVariant(mutated)
			if got != 0 && (int(got) > len(rv64GCTypeSubtypingStructProjectionLinkWires) || !bytes.Equal(mutated, rv64GCTypeSubtypingStructProjectionLinkWires[got-1])) {
				t.Fatalf("variant %d wire mutation %d was admitted outside the exact source set as variant %d", variant, j, got)
			}
		}
	}
}
