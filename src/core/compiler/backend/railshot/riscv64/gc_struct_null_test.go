//go:build linux && !tinygo

package riscv64

import (
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/codegen"
	"github.com/wago-org/wago/src/core/compiler/frontend"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestRV64GCStructNullExactAdmission(t *testing.T) {
	source, err := hex.DecodeString(rv64GCStructNullHex)
	if err != nil {
		t.Fatal(err)
	}
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	if len(source) != 118 || sha256.Sum256(source) != rv64GCStructNullSourceDigest || !admitRV64GCStructNullWire(source) || !AdmitGCStructNullSource(source) || !admitRV64GCStructNull(m) {
		t.Fatal("exact official struct null leader was not admitted")
	}
	canonical := canonicalSectionSizes(source)
	canonicalModule, err := wasm.DecodeModule(canonical)
	if err != nil || !admitRV64GCStructNull(canonicalModule) || len(canonical) != 102 {
		t.Fatalf("canonical alternate did not preserve decoded semantics: len=%d err=%v", len(canonical), err)
	}
	if admitRV64GCStructNullWire(canonical) || AdmitGCStructNullSource(canonical) {
		t.Fatal("semantically identical alternate wire encoding was admitted")
	}
	metadata, err := frontend.BuildGCTypeMetadata(m)
	if err != nil {
		t.Fatal(err)
	}
	opts := CompileOptions{DeferCodeMapping: true, Interruptible: true, InvocationContext: true, ForeignTableCalls: true, GCStructHelpers: true, SyncHostSlots: 4, GCStructNullSource: true, Codegen: codegen.Options{Module: codegen.ModuleInfo{GCTypeDescs: metadata.Descs, GCTypeLayouts: metadata.Layouts}}}
	compiled, err := CompileModuleWith(m, opts)
	if err != nil {
		t.Fatal(err)
	}
	wantDigest := [sha256.Size]byte{0xda, 0x20, 0xa4, 0xc8, 0x70, 0xac, 0xca, 0x39, 0x87, 0x51, 0xd5, 0x02, 0xd2, 0xf9, 0x33, 0x6b, 0x5b, 0x91, 0xd9, 0x8b, 0xdd, 0xf1, 0xd1, 0xd0, 0x57, 0xfc, 0x4d, 0xb3, 0x1d, 0x7a, 0xe8, 0x5f}
	wantEntries := []int{0, 672}
	if len(compiled.Code) != 1896 || sha256.Sum256(compiled.Code) != wantDigest || !slices.Equal(compiled.Entry, wantEntries) || !slices.Equal(compiled.InternalEntry, wantEntries) {
		t.Fatalf("unexpected struct null image: code=%d/%x entries=%v/%v", len(compiled.Code), sha256.Sum256(compiled.Code), compiled.Entry, compiled.InternalEntry)
	}
	withoutWire := opts
	withoutWire.GCStructNullSource = false
	if _, err := CompileModuleWith(m, withoutWire); err == nil {
		t.Fatal("decoded struct null module compiled without exact-wire witness")
	}
	for name, mutate := range map[string]func(*CompileOptions){
		"missing-descriptors": func(candidate *CompileOptions) { candidate.Codegen.Module.GCTypeDescs = nil },
		"wrong-descriptor": func(candidate *CompileOptions) {
			candidate.Codegen.Module.GCTypeDescs[0].Fields[1].Offset++
		},
		"missing-layouts": func(candidate *CompileOptions) { candidate.Codegen.Module.GCTypeLayouts = nil },
		"wrong-layout": func(candidate *CompileOptions) {
			candidate.Codegen.Module.GCTypeLayouts[0].PointerFree = !candidate.Codegen.Module.GCTypeLayouts[0].PointerFree
		},
		"array-helper":  func(candidate *CompileOptions) { candidate.GCArrayHelpers = true },
		"slot-capacity": func(candidate *CompileOptions) { candidate.SyncHostSlots = 3 },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := opts
			candidate.Codegen.Module.GCTypeDescs = slices.Clone(opts.Codegen.Module.GCTypeDescs)
			candidate.Codegen.Module.GCTypeLayouts = slices.Clone(opts.Codegen.Module.GCTypeLayouts)
			if len(candidate.Codegen.Module.GCTypeDescs) != 0 {
				candidate.Codegen.Module.GCTypeDescs[0].Fields = slices.Clone(opts.Codegen.Module.GCTypeDescs[0].Fields)
			}
			mutate(&candidate)
			if image, err := CompileModuleWith(m, candidate); image != nil || err == nil {
				t.Fatal("gc struct null escaped exact helper/collector metadata", err)
			}
		})
	}
	for i := range source {
		candidate := append([]byte(nil), source...)
		candidate[i] ^= 1
		if admitRV64GCStructNullWire(candidate) {
			t.Fatalf("wire mutation at byte %d was admitted", i)
		}
	}
}
