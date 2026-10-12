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

func TestRV64GCTypeSubtypingExtendedRecursiveLinkAdmissionIsExact(t *testing.T) {
	wantSources := [][sha256.Size]byte{
		{0xba, 0x25, 0x1b, 0x03, 0x25, 0xc3, 0xf0, 0xc4, 0xaa, 0xb8, 0x0a, 0xea, 0xcf, 0x9f, 0xaf, 0x0d, 0xd9, 0x60, 0xe4, 0x8b, 0x48, 0xb6, 0xb7, 0xa4, 0x1d, 0x2f, 0x07, 0xe0, 0x3f, 0x37, 0xd3, 0x79},
		{0xb0, 0xe1, 0x4a, 0x30, 0x1b, 0xd4, 0x40, 0xe1, 0x55, 0x0b, 0xe3, 0x5f, 0x09, 0xd9, 0xb0, 0xec, 0xb0, 0x47, 0x6b, 0x09, 0xe4, 0x67, 0xb9, 0x65, 0xb0, 0x2b, 0x69, 0x35, 0xdc, 0xac, 0x0f, 0xd2},
	}
	wantSizes := []int{136, 164}
	for i, wire := range rv64GCTypeSubtypingExtendedRecursiveLinkWires {
		variant := uint8(i + 1)
		data := bytes.Clone(wire)
		if gotHash, gotVariant := sha256.Sum256(data), GCTypeSubtypingExtendedRecursiveLinkSourceVariant(data); len(data) != wantSizes[i] || gotHash != wantSources[i] || gotVariant != variant {
			t.Fatalf("variant %d source identity changed: size=%d hash=%x want=%x classifier=%d", variant, len(data), gotHash, wantSources[i], gotVariant)
		}
		m, err := wasm.DecodeModule(data)
		if err != nil || admitRV64GCTypeSubtypingExtendedRecursiveLink(m) != variant {
			t.Fatalf("variant %d decoded shape not admitted: %v", variant, err)
		}
		metadata, err := frontend.BuildGCTypeMetadata(m)
		if err != nil {
			t.Fatal(err)
		}
		opts := CompileOptions{DeferCodeMapping: true, Interruptible: true, InvocationContext: true, ForeignTableCalls: true, GCTypeSubtypingExtendedRecursiveSource: variant, Codegen: codegen.Options{Module: codegen.ModuleInfo{GCTypeDescs: metadata.Descs, GCTypeLayouts: metadata.Layouts}}}
		if variant == rv64GCTypeSubtypingExtendedRecursiveLinkConsumerVariant {
			opts.ImportBindings = make([]ImportBinding, 8)
			for j := range opts.ImportBindings {
				opts.ImportBindings[j] = ImportBinding{Dynamic: true, ImportIndex: uint32(j)}
			}
		}
		compiled, err := CompileModuleWith(m, opts)
		if err != nil {
			t.Fatalf("variant %d compile: %v", variant, err)
		}
		if variant == rv64GCTypeSubtypingExtendedRecursiveLinkProviderVariant {
			wantCode := [sha256.Size]byte{0xae, 0xc4, 0xd3, 0x0d, 0x9e, 0x07, 0xb3, 0x2c, 0x1a, 0xe9, 0x92, 0xfe, 0xf2, 0x8b, 0x5a, 0x4c, 0xc3, 0x22, 0x4f, 0xb1, 0xc6, 0x26, 0xf7, 0xb5, 0xe1, 0x63, 0xed, 0xf1, 0xc8, 0xa9, 0xed, 0x77}
			if len(compiled.Code) != 1248 || sha256.Sum256(compiled.Code) != wantCode || !slices.Equal(compiled.Entry, []int{0, 624}) || !slices.Equal(compiled.InternalEntry, []int{0, 624}) || compiled.CodeImage != nil {
				t.Fatalf("provider image incomplete: code=%d hash=%x entries=%v/%v image=%v", len(compiled.Code), sha256.Sum256(compiled.Code), compiled.Entry, compiled.InternalEntry, compiled.CodeImage)
			}
		} else if len(compiled.Code) != 0 || len(compiled.Entry) != 0 || len(compiled.InternalEntry) != 0 || compiled.CodeImage != nil {
			t.Fatalf("consumer must remain zero-code: code=%d entries=%v/%v image=%v", len(compiled.Code), compiled.Entry, compiled.InternalEntry, compiled.CodeImage)
		}
		withoutWire := opts
		withoutWire.GCTypeSubtypingExtendedRecursiveSource = 0
		if _, err = CompileModuleWith(m, withoutWire); !errors.Is(err, ErrUnsupported) {
			t.Fatal("extended-recursive product compiled without exact provenance", variant, err)
		}
		wrongWire := opts
		wrongWire.GCTypeSubtypingExtendedRecursiveSource = variant%uint8(len(rv64GCTypeSubtypingExtendedRecursiveLinkWires)) + 1
		if _, err = CompileModuleWith(m, wrongWire); !errors.Is(err, ErrUnsupported) {
			t.Fatal("extended-recursive product compiled with wrong provenance", variant, err)
		}
		for j := range data {
			mutated := bytes.Clone(data)
			mutated[j] ^= 1
			got := GCTypeSubtypingExtendedRecursiveLinkSourceVariant(mutated)
			if got != 0 && (int(got) > len(rv64GCTypeSubtypingExtendedRecursiveLinkWires) || !bytes.Equal(mutated, rv64GCTypeSubtypingExtendedRecursiveLinkWires[got-1])) {
				t.Fatalf("variant %d mutation %d admitted outside exact source set as %d", variant, j, got)
			}
		}
	}
}
