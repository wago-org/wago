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

func TestRV64GCTypeSubtypingStructMismatchLinkAdmissionIsExact(t *testing.T) {
	wantSources := [][sha256.Size]byte{
		{0x04, 0x94, 0xd7, 0xc9, 0x5b, 0x50, 0xe1, 0x51, 0xac, 0x8e, 0x0f, 0x9e, 0xb8, 0xa1, 0xc9, 0x35, 0xa0, 0x16, 0xdb, 0x45, 0xb1, 0x96, 0x93, 0x78, 0xed, 0x95, 0xd4, 0x03, 0x69, 0xfd, 0xa0, 0x62},
		{0xbb, 0x59, 0x8c, 0xc8, 0x9f, 0x2d, 0x73, 0x72, 0x01, 0x90, 0xe6, 0xc7, 0xe1, 0x15, 0xbe, 0xc1, 0x04, 0x01, 0x3b, 0xf8, 0xeb, 0xea, 0xd4, 0xc4, 0x17, 0xd1, 0x7e, 0x70, 0x15, 0x98, 0xc7, 0xa1},
	}
	wantSizes := []int{82, 51}
	for i, wire := range rv64GCTypeSubtypingStructMismatchLinkWires {
		variant := uint8(i + 1)
		data := bytes.Clone(wire)
		if gotHash, gotVariant := sha256.Sum256(data), GCTypeSubtypingStructMismatchLinkSourceVariant(data); len(data) != wantSizes[i] || gotHash != wantSources[i] || gotVariant != variant {
			t.Fatalf("variant %d source identity changed: size=%d hash=%x want=%x classifier=%d", variant, len(data), gotHash, wantSources[i], gotVariant)
		}
		m, err := wasm.DecodeModule(data)
		if err != nil || admitRV64GCTypeSubtypingStructMismatchLink(m) != variant {
			t.Fatalf("variant %d decoded shape not admitted: %v", variant, err)
		}
		metadata, err := frontend.BuildGCTypeMetadata(m)
		if err != nil {
			t.Fatal(err)
		}
		opts := CompileOptions{
			DeferCodeMapping: true, Interruptible: true, InvocationContext: true, ForeignTableCalls: true,
			GCTypeSubtypingStructMismatchSource: variant,
			Codegen:                             codegen.Options{Module: codegen.ModuleInfo{GCTypeDescs: metadata.Descs, GCTypeLayouts: metadata.Layouts}},
		}
		if variant == rv64GCTypeSubtypingStructMismatchLinkConsumerVariant {
			opts.ImportBindings = []ImportBinding{{Dynamic: true}}
		}
		compiled, err := CompileModuleWith(m, opts)
		if err != nil {
			t.Fatalf("variant %d compile: %v", variant, err)
		}
		if variant == rv64GCTypeSubtypingStructMismatchLinkProviderVariant {
			wantCode := [sha256.Size]byte{0x1d, 0xf4, 0x1d, 0xb6, 0xda, 0x6c, 0xf7, 0x49, 0x1e, 0x95, 0x1a, 0x23, 0xe0, 0x52, 0x51, 0xba, 0xc1, 0x48, 0x0b, 0xe5, 0xaa, 0x36, 0xe1, 0x01, 0x5d, 0x3c, 0xd9, 0x75, 0x4c, 0x4d, 0x65, 0x03}
			if len(compiled.Code) != 576 || sha256.Sum256(compiled.Code) != wantCode || !slices.Equal(compiled.Entry, []int{0}) || !slices.Equal(compiled.InternalEntry, []int{0}) || compiled.CodeImage != nil {
				t.Fatalf("provider image is incomplete: code=%d/%x entries=%v/%v image=%v", len(compiled.Code), sha256.Sum256(compiled.Code), compiled.Entry, compiled.InternalEntry, compiled.CodeImage)
			}
		} else if len(compiled.Code) != 0 || len(compiled.Entry) != 0 || len(compiled.InternalEntry) != 0 || compiled.CodeImage != nil {
			t.Fatalf("consumer must remain zero-code: code=%d entries=%v/%v image=%v", len(compiled.Code), compiled.Entry, compiled.InternalEntry, compiled.CodeImage)
		}
		withoutWire := opts
		withoutWire.GCTypeSubtypingStructMismatchSource = 0
		if _, err = CompileModuleWith(m, withoutWire); !errors.Is(err, ErrUnsupported) {
			t.Fatal("struct-mismatch-link product compiled without exact wire provenance", variant, err)
		}
		wrongWire := opts
		wrongWire.GCTypeSubtypingStructMismatchSource = variant%uint8(len(rv64GCTypeSubtypingStructMismatchLinkWires)) + 1
		if _, err = CompileModuleWith(m, wrongWire); !errors.Is(err, ErrUnsupported) {
			t.Fatal("struct-mismatch-link product compiled with wrong wire provenance", variant, err)
		}
		for j := range data {
			mutated := bytes.Clone(data)
			mutated[j] ^= 1
			got := GCTypeSubtypingStructMismatchLinkSourceVariant(mutated)
			if got != 0 && (int(got) > len(rv64GCTypeSubtypingStructMismatchLinkWires) || !bytes.Equal(mutated, rv64GCTypeSubtypingStructMismatchLinkWires[got-1])) {
				t.Fatalf("variant %d wire mutation %d was admitted outside the exact source set as variant %d", variant, j, got)
			}
		}
	}
}
