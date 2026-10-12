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

func TestRV64GCTypeSubtypingDuplicateRecursiveLinkAdmissionIsExact(t *testing.T) {
	wantSources := [][sha256.Size]byte{
		{0xce, 0xe5, 0x3b, 0x6e, 0x42, 0x09, 0x32, 0xfa, 0xec, 0x8b, 0xf1, 0x66, 0xa6, 0xae, 0x79, 0xcf, 0xab, 0x88, 0xf7, 0xca, 0x89, 0x0c, 0xd3, 0x98, 0x51, 0xd3, 0xf7, 0x0b, 0x93, 0x24, 0x71, 0xaa},
		{0x30, 0xff, 0x7e, 0x3b, 0xef, 0xab, 0x74, 0x05, 0xc6, 0x35, 0x54, 0x52, 0x6e, 0xcf, 0x2e, 0x61, 0xfc, 0x9e, 0xcf, 0x2b, 0x65, 0xe1, 0x75, 0x41, 0x4d, 0xb0, 0x7a, 0xa7, 0x74, 0xe6, 0xa5, 0x40},
	}
	wantSizes := []int{100, 92}
	for i, wire := range rv64GCTypeSubtypingDuplicateRecursiveLinkWires {
		variant := uint8(i + 1)
		data := bytes.Clone(wire)
		if gotHash, gotVariant := sha256.Sum256(data), GCTypeSubtypingDuplicateRecursiveLinkSourceVariant(data); len(data) != wantSizes[i] || gotHash != wantSources[i] || gotVariant != variant {
			t.Fatalf("variant %d source identity changed: size=%d hash=%x want=%x classifier=%d", variant, len(data), gotHash, wantSources[i], gotVariant)
		}
		m, err := wasm.DecodeModule(data)
		if err != nil || admitRV64GCTypeSubtypingDuplicateRecursiveLink(m) != variant {
			t.Fatalf("variant %d decoded shape not admitted: %v", variant, err)
		}
		metadata, err := frontend.BuildGCTypeMetadata(m)
		if err != nil {
			t.Fatal(err)
		}
		opts := CompileOptions{DeferCodeMapping: true, Interruptible: true, InvocationContext: true, ForeignTableCalls: true, GCTypeSubtypingDuplicateRecursiveSource: variant, Codegen: codegen.Options{Module: codegen.ModuleInfo{GCTypeDescs: metadata.Descs, GCTypeLayouts: metadata.Layouts}}}
		if variant == rv64GCTypeSubtypingDuplicateRecursiveLinkConsumerVariant {
			opts.ImportBindings = []ImportBinding{{Dynamic: true}, {Dynamic: true, ImportIndex: 1}, {Dynamic: true, ImportIndex: 2}, {Dynamic: true, ImportIndex: 3}}
		}
		compiled, err := CompileModuleWith(m, opts)
		if err != nil {
			t.Fatalf("variant %d compile: %v", variant, err)
		}
		if variant == rv64GCTypeSubtypingDuplicateRecursiveLinkProviderVariant {
			wantCode := [sha256.Size]byte{0xae, 0xc4, 0xd3, 0x0d, 0x9e, 0x07, 0xb3, 0x2c, 0x1a, 0xe9, 0x92, 0xfe, 0xf2, 0x8b, 0x5a, 0x4c, 0xc3, 0x22, 0x4f, 0xb1, 0xc6, 0x26, 0xf7, 0xb5, 0xe1, 0x63, 0xed, 0xf1, 0xc8, 0xa9, 0xed, 0x77}
			if len(compiled.Code) != 1248 || sha256.Sum256(compiled.Code) != wantCode || !slices.Equal(compiled.Entry, []int{0, 624}) || !slices.Equal(compiled.InternalEntry, []int{0, 624}) || compiled.CodeImage != nil {
				t.Fatalf("provider image incomplete: code=%d entries=%v/%v image=%v", len(compiled.Code), compiled.Entry, compiled.InternalEntry, compiled.CodeImage)
			}
		} else if len(compiled.Code) != 0 || len(compiled.Entry) != 0 || len(compiled.InternalEntry) != 0 || compiled.CodeImage != nil {
			t.Fatalf("consumer must remain zero-code: code=%d entries=%v/%v image=%v", len(compiled.Code), compiled.Entry, compiled.InternalEntry, compiled.CodeImage)
		}
		withoutWire := opts
		withoutWire.GCTypeSubtypingDuplicateRecursiveSource = 0
		if _, err = CompileModuleWith(m, withoutWire); !errors.Is(err, ErrUnsupported) {
			t.Fatal("duplicate-recursive product compiled without exact provenance", variant, err)
		}
		wrongWire := opts
		wrongWire.GCTypeSubtypingDuplicateRecursiveSource = variant%uint8(len(rv64GCTypeSubtypingDuplicateRecursiveLinkWires)) + 1
		if _, err = CompileModuleWith(m, wrongWire); !errors.Is(err, ErrUnsupported) {
			t.Fatal("duplicate-recursive product compiled with wrong provenance", variant, err)
		}
		for j := range data {
			mutated := bytes.Clone(data)
			mutated[j] ^= 1
			got := GCTypeSubtypingDuplicateRecursiveLinkSourceVariant(mutated)
			if got != 0 && (int(got) > len(rv64GCTypeSubtypingDuplicateRecursiveLinkWires) || !bytes.Equal(mutated, rv64GCTypeSubtypingDuplicateRecursiveLinkWires[got-1])) {
				t.Fatalf("variant %d mutation %d admitted outside exact source set as %d", variant, j, got)
			}
		}
	}
}
