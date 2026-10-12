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

func TestRV64GCTypeSubtypingCrossGroupMismatchLinkAdmissionIsExact(t *testing.T) {
	wantSources := [][sha256.Size]byte{
		{0xdb, 0x07, 0xa8, 0xde, 0x50, 0x25, 0xcd, 0x21, 0x5a, 0x20, 0xa1, 0xe1, 0x18, 0x18, 0x3a, 0x22, 0x03, 0x54, 0xa1, 0xaa, 0x93, 0xea, 0x15, 0x4d, 0xaa, 0xd3, 0xf4, 0xcd, 0x75, 0xc0, 0xdf, 0xf9},
		{0x33, 0x62, 0xe5, 0xe8, 0x79, 0x44, 0xfe, 0x7f, 0x1a, 0x90, 0xcc, 0x31, 0xd6, 0xfd, 0x4b, 0xac, 0xf6, 0x43, 0x06, 0x73, 0xbc, 0x94, 0x19, 0x04, 0x2d, 0x1f, 0xc0, 0xe4, 0x87, 0xe8, 0x4a, 0x7b},
	}
	wantSizes := []int{74, 43}
	for i, wire := range rv64GCTypeSubtypingCrossGroupMismatchLinkWires {
		variant := uint8(i + 1)
		data := bytes.Clone(wire)
		if gotHash, gotVariant := sha256.Sum256(data), GCTypeSubtypingCrossGroupMismatchLinkSourceVariant(data); len(data) != wantSizes[i] || gotHash != wantSources[i] || gotVariant != variant {
			t.Fatalf("variant %d source identity changed: size=%d hash=%x want=%x classifier=%d", variant, len(data), gotHash, wantSources[i], gotVariant)
		}
		m, err := wasm.DecodeModule(data)
		if err != nil || admitRV64GCTypeSubtypingCrossGroupMismatchLink(m) != variant {
			t.Fatalf("variant %d decoded shape not admitted: %v", variant, err)
		}
		metadata, err := frontend.BuildGCTypeMetadata(m)
		if err != nil {
			t.Fatal(err)
		}
		opts := CompileOptions{
			DeferCodeMapping: true, Interruptible: true, InvocationContext: true, ForeignTableCalls: true,
			GCTypeSubtypingCrossGroupMismatchSource: variant,
			Codegen:                                 codegen.Options{Module: codegen.ModuleInfo{GCTypeDescs: metadata.Descs, GCTypeLayouts: metadata.Layouts}},
		}
		if variant == rv64GCTypeSubtypingCrossGroupMismatchLinkConsumerVariant {
			opts.ImportBindings = []ImportBinding{{Dynamic: true}}
		}
		compiled, err := CompileModuleWith(m, opts)
		if err != nil {
			t.Fatalf("variant %d compile: %v", variant, err)
		}
		if variant == rv64GCTypeSubtypingCrossGroupMismatchLinkProviderVariant {
			wantCode := [sha256.Size]byte{0x1d, 0xf4, 0x1d, 0xb6, 0xda, 0x6c, 0xf7, 0x49, 0x1e, 0x95, 0x1a, 0x23, 0xe0, 0x52, 0x51, 0xba, 0xc1, 0x48, 0x0b, 0xe5, 0xaa, 0x36, 0xe1, 0x01, 0x5d, 0x3c, 0xd9, 0x75, 0x4c, 0x4d, 0x65, 0x03}
			if len(compiled.Code) != 576 || sha256.Sum256(compiled.Code) != wantCode || !slices.Equal(compiled.Entry, []int{0}) || !slices.Equal(compiled.InternalEntry, []int{0}) || compiled.CodeImage != nil {
				t.Fatalf("provider image is incomplete: code=%d/%x entries=%v/%v image=%v", len(compiled.Code), sha256.Sum256(compiled.Code), compiled.Entry, compiled.InternalEntry, compiled.CodeImage)
			}
		} else if len(compiled.Code) != 0 || len(compiled.Entry) != 0 || len(compiled.InternalEntry) != 0 || compiled.CodeImage != nil {
			t.Fatalf("consumer must remain zero-code: code=%d entries=%v/%v image=%v", len(compiled.Code), compiled.Entry, compiled.InternalEntry, compiled.CodeImage)
		}
		withoutWire := opts
		withoutWire.GCTypeSubtypingCrossGroupMismatchSource = 0
		if _, err = CompileModuleWith(m, withoutWire); !errors.Is(err, ErrUnsupported) {
			t.Fatal("cross-group-mismatch-link product compiled without exact wire provenance", variant, err)
		}
		wrongWire := opts
		wrongWire.GCTypeSubtypingCrossGroupMismatchSource = variant%uint8(len(rv64GCTypeSubtypingCrossGroupMismatchLinkWires)) + 1
		if _, err = CompileModuleWith(m, wrongWire); !errors.Is(err, ErrUnsupported) {
			t.Fatal("cross-group-mismatch-link product compiled with wrong wire provenance", variant, err)
		}
		for j := range data {
			mutated := bytes.Clone(data)
			mutated[j] ^= 1
			got := GCTypeSubtypingCrossGroupMismatchLinkSourceVariant(mutated)
			if got != 0 && (int(got) > len(rv64GCTypeSubtypingCrossGroupMismatchLinkWires) || !bytes.Equal(mutated, rv64GCTypeSubtypingCrossGroupMismatchLinkWires[got-1])) {
				t.Fatalf("variant %d wire mutation %d was admitted outside the exact source set as variant %d", variant, j, got)
			}
		}
	}
}
