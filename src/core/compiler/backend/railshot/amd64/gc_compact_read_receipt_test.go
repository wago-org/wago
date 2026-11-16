//go:build amd64 && (wago_gc_compact_reads || wago_gc_compact_alloc || (wago_gc_compact_refstore || wago_gc_compact_shared_checks) || wago_gc_compact_scalar_arrays || (wago_gc_compact_array_alloc || (wago_gc_compact_fill || (wago_gc_compact_copy || wago_gc_compact_loop))) || wago_gc_compact_refstruct)

package amd64

import (
	"github.com/wago-org/wago/src/core/compiler/codegen"
	"github.com/wago-org/wago/src/core/compiler/frontend"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"strings"
	"testing"
)

func TestCompactNativeReadArtifactReceipt(t *testing.T) {
	if gcDirectReads {
		cm, err := CompileModuleWith(nil, CompileOptions{})
		if cm != nil || err == nil {
			t.Fatal("mixed compact/direct lowering accepted")
		}
		return
	}
	m := gcNativeAttributionModule(t, false)
	cm, err := CompileModuleWith(m, CompileOptions{GCStructHelpers: true})
	if err != nil {
		t.Fatal(err)
	}
	if cm.CodeImage != nil {
		defer cm.CodeImage.Close()
	}
	if cm.GCHelperOnlyReceipt || cm.GCCompactHelperVersion != 0 || cm.GCDirectReadABIVersion != 0 || uint32(cm.GCCompactNativeVersion) != gc.CompactNativeABIVersion {
		t.Fatalf("helper=%v direct=%d", cm.GCHelperOnlyReceipt, cm.GCDirectReadABIVersion)
	}
}

func TestCompactNativeReadInstructions(t *testing.T) {
	if gcDirectReads {
		t.Skip("mixed mode separately rejects")
	}
	data := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec([]byte{0x5f, 1, 0x7f, 0}, []byte{0x60, 1, 0x63, 0, 1, 0x7f})), wasmtest.Section(3, wasmtest.Vec([]byte{1})), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0xfb, 2, 0, 0, 0x0b}))))
	m, e := wasm.DecodeModule(data)
	if e != nil {
		t.Fatal(e)
	}
	if e = wasm.ValidateModule(m); e != nil {
		t.Fatal(e)
	}
	var stats *ModuleStats
	if diagnosticsEnabled {
		stats = &ModuleStats{}
	}
	metadata, e := frontend.BuildGCTypeMetadata(m)
	if e != nil {
		t.Fatal(e)
	}
	cm, e := CompileModuleWith(m, CompileOptions{GCStructHelpers: true, Stats: stats, Codegen: codegen.Options{Module: codegen.ModuleInfo{GCTypeLayouts: metadata.Layouts}}})
	if e != nil {
		t.Fatal(e)
	}
	defer cm.CodeImage.Close()
	if stats != nil && stats.Funcs[0].Peephole["gc-compact-struct-read"] != 1 {
		t.Fatal("missing compact scalar lowering", stats.Funcs[0].Peephole)
	}
	listing := disasm(t, cm.Code)
	found := false
	for _, line := range strings.Split(listing, "\n") {
		if strings.Contains(line, "mov") && strings.Contains(line, "dword ptr [") && strings.Contains(line, "+0x4]") {
			found = true
			t.Log(line)
		}
	}
	if !found {
		t.Fatal("missing compact payload-offset4 load", listing)
	}
}
