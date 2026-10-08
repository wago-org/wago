//go:build linux && (amd64 || arm64 || riscv64) && !tinygo && !wago_target_tinygo

package riscv64

import (
	"bytes"
	"errors"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/codegen"
	"github.com/wago-org/wago/src/core/compiler/frontend"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/src/core/runtime/gc/native"
)

func TestProductionDecodedCompilerOwnershipContract(t *testing.T) {
	data := binaryModule(sumCallFixture(0), fixtureFunc{params: []byte{0x7f, 0x7e}, results: []byte{0x7f, 0x7e}, body: []byte{0x20, 0, 0x20, 1, 0x0b}})
	m, err := wasm.DecodeModule(data)
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := frontend.BuildGCTypeMetadata(m)
	if err != nil {
		t.Fatal(err)
	}
	for _, fallback := range []bool{false, true} {
		reference, err := CompileProduction(data, ProductionOptions{ForceFallback: fallback, DeferCodeMapping: true, Interruptible: true})
		if err != nil {
			t.Fatal(err)
		}
		defer reference.CodeImage.Close()
		for _, heap := range []bool{false, true} {
			pressureCalls := 0
			image, err := CompileModuleWith(m, CompileOptions{DeferCodeMapping: heap, ForceFallback: fallback, Interruptible: true, Workers: 8, SyncHostSlots: 64, Codegen: codegen.Options{Module: codegen.ModuleInfo{GCTypeDescs: metadata.Descs}}, MemoryPressure: func() { pressureCalls++ }})
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(image.Code, reference.Code) || pressureCalls != 1 {
				t.Fatal("decoded contract changed code or pressure checkpoint", pressureCalls)
			}
			if heap {
				if image.CodeImage != nil {
					t.Fatal("heap bytes advertised an RW mapping")
				}
			} else {
				if image.CodeImage == nil {
					t.Fatal("RW image ownership lost")
				}
				mapping, base, err := image.CodeImage.Take()
				if err != nil || base == 0 || !bytes.Equal(mapping[:len(image.Code)], image.Code) {
					t.Fatal("shared RW transfer", err)
				}
				if err := coreruntime.Unmap(mapping); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
}

func TestProductionDecodedCompilerRejectsSemanticDependencies(t *testing.T) {
	data := binaryModule(fixtureFunc{result: 0x7f, body: []byte{0x41, 7, 0x0b}})
	m, err := wasm.DecodeModule(data)
	if err != nil {
		t.Fatal(err)
	}
	for _, opts := range []CompileOptions{
		{ElideBoundsChecks: true}, {SyncHostCalls: true}, {ImportBindings: []ImportBinding{{Dynamic: true}}},
		{GCTypeSubtypingRefTest: true}, {GCStructHelpers: true}, {GCArrayHelpers: true},
		{Profile: true}, {SourceMaps: true}, {UnwindMaps: true}, {Stats: &ModuleStats{}}, {CollectInlineReport: true},
		{Codegen: codegen.Options{Module: codegen.ModuleInfo{GCTypeDescs: []gc.TypeDesc{{Kind: gc.KindStruct}}}}},
		{Codegen: codegen.Options{Module: codegen.ModuleInfo{Features: codegen.Features{GC: true}}}},
		{Optimizations: map[string]bool{"reg-abi": true}}, {SyncHostSlots: -1}, {SyncHostSlots: 65536},
	} {
		if opts.Stats != nil && diagnosticsEnabled || opts.Profile && profileEnabled {
			continue // Finalized sizes are qualified independently of profiling.
		}
		pressureCalls := 0
		opts.MemoryPressure = func() { pressureCalls++ }
		image, err := CompileModuleWith(m, opts)
		if err == nil || image != nil || pressureCalls != 0 {
			t.Fatalf("pending semantic option reached image staging: %+v / %v", opts, err)
		}
	}
	if image, err := CompileModuleWith(nil, CompileOptions{}); err == nil || image != nil {
		t.Fatal("nil decoded module admitted")
	}
	if SetOptKnob("reg-abi", true) || len(OptKnobs()) != 0 {
		t.Fatal("unimplemented optimization advertised")
	}
}

func TestProductionSourceRetainsPrivateWireAdmission(t *testing.T) {
	base := binaryModule(fixtureFunc{result: 0x7f, body: []byte{0x41, 7, 0x0b}})
	for _, section := range []byte{2, 4, 9} {
		before := byte(3)
		if section == 4 {
			before = 7
		}
		if section == 9 {
			before = 10
		}
		data := modifySection(base, before, section, []byte{0}, false)
		m, err := wasm.DecodeModule(data)
		if err != nil {
			t.Fatal(err)
		}
		if len(m.Imports) != 0 || len(m.Tables) != 0 {
			t.Fatal("fixture is not a present empty section")
		}
		if err := wasm.ValidateModule(m); err != nil {
			t.Fatal(err)
		}
		if err := AdmitProductionSource(data); err != nil {
			t.Fatal("empty public section rejected", section, err)
		}
		if err := admitProductionWire(data); !errors.Is(err, ErrUnsupported) {
			t.Fatal("private empty-section provenance disappeared", section, err)
		}
	}
	if err := AdmitProductionSource(base); err != nil {
		t.Fatal(err)
	}
}
