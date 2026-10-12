//go:build linux && !tinygo

package riscv64

import (
	"crypto/sha256"
	"errors"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/codegen"
	"github.com/wago-org/wago/src/core/compiler/frontend"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestRV64GCTypeSubtypingNonFlatExportAdmission(t *testing.T) {
	if len(rv64GCTypeSubtypingNonFlatExportWire) != 183 || sha256.Sum256(rv64GCTypeSubtypingNonFlatExportWire) != [sha256.Size]byte{0x0c, 0x14, 0x10, 0x8c, 0xfe, 0x32, 0x3e, 0x2d, 0xd3, 0x9f, 0xd8, 0xbf, 0xe3, 0x33, 0xbd, 0x92, 0x5e, 0x2f, 0x19, 0x32, 0x4e, 0x90, 0xcd, 0x1c, 0x3e, 0xe1, 0x61, 0x5f, 0xda, 0xd2, 0xbb, 0x60} {
		t.Fatal("official non-flat export wire identity changed")
	}
	if !admitRV64GCTypeSubtypingNonFlatExportWire(rv64GCTypeSubtypingNonFlatExportWire) || !admitRV64GCTypeSubtypingNonFlatExport(rv64GCTypeSubtypingNonFlatExportModule) {
		t.Fatal("official non-flat export product rejected")
	}
	metadata, err := frontend.BuildGCTypeMetadata(rv64GCTypeSubtypingNonFlatExportModule)
	if err != nil {
		t.Fatal(err)
	}
	opts := CompileOptions{
		DeferCodeMapping:                   true,
		Interruptible:                      true,
		InvocationContext:                  true,
		ForeignTableCalls:                  true,
		GCTypeSubtypingNonFlatExportSource: true,
		Codegen: codegen.Options{Module: codegen.ModuleInfo{
			GCTypeDescs:   metadata.Descs,
			GCTypeLayouts: metadata.Layouts,
		}},
	}
	compiled, err := CompileModuleWith(rv64GCTypeSubtypingNonFlatExportModule, opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(compiled.Code) == 0 || len(compiled.Entry) != 6 || len(compiled.InternalEntry) != 6 || compiled.CodeImage != nil {
		t.Fatalf("non-flat export missing deferred code: code=%d entries=%v/%v image=%v", len(compiled.Code), compiled.Entry, compiled.InternalEntry, compiled.CodeImage)
	}
	t.Logf("code=%d sha256=%x entry=%v internal=%v", len(compiled.Code), sha256.Sum256(compiled.Code), compiled.Entry, compiled.InternalEntry)
	withoutWire := opts
	withoutWire.GCTypeSubtypingNonFlatExportSource = false
	if _, err = CompileModuleWith(rv64GCTypeSubtypingNonFlatExportModule, withoutWire); !errors.Is(err, ErrUnsupported) {
		t.Fatal("decoded non-flat export compiled without exact wire provenance", err)
	}
	for i := range rv64GCTypeSubtypingNonFlatExportWire {
		candidate := append([]byte(nil), rv64GCTypeSubtypingNonFlatExportWire...)
		candidate[i] ^= 1
		if admitRV64GCTypeSubtypingNonFlatExportWire(candidate) {
			t.Fatalf("mutated official wire admitted at byte %d", i)
		}
	}
	m, err := wasm.DecodeModule(append([]byte(nil), rv64GCTypeSubtypingNonFlatExportWire...))
	if err != nil {
		t.Fatal(err)
	}
	m.Code[0].BodyBytes[1] = 1
	if admitRV64GCTypeSubtypingNonFlatExport(m) {
		t.Fatal("mutated decoded body admitted")
	}
}
