//go:build amd64 && wago_gc_compact_range_stride

package amd64

import (
	"bytes"
	"github.com/wago-org/wago/src/core/compiler/codegen"
	"github.com/wago-org/wago/src/core/compiler/frontend"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"os"
	"testing"
)

func TestCompactRangeStrideLargeEndFallback(t *testing.T) {
	requireCompilerDiagnostics(t)
	data, e := os.ReadFile("../../../../../../bench/experiments/gc-v8/gc-array-loop-copy-checksum.wasm")
	if e != nil {
		t.Fatal(e)
	}
	m, e := wasm.DecodeModule(data)
	if e != nil {
		t.Fatal(e)
	}
	// 0x20000000 i32 elements imply byte-end 0x80000000, not a positive
	// sign-extended imm32 in a 64-bit comparison. Compile only, never allocate.
	m.Code[2].BodyBytes = bytes.ReplaceAll(m.Code[2].BodyBytes, []byte{0x41, 32, 0x49}, []byte{0x41, 0x80, 0x80, 0x80, 0x80, 2, 0x49})
	md, e := frontend.BuildGCTypeMetadata(m)
	if e != nil {
		t.Fatal(e)
	}
	stats := &ModuleStats{}
	cm, e := CompileModuleWith(m, CompileOptions{GCStructHelpers: true, GCArrayHelpers: true, Stats: stats, Codegen: codegen.Options{Module: codegen.ModuleInfo{GCTypeLayouts: md.Layouts}}})
	if e != nil {
		t.Fatal(e)
	}
	defer cm.CodeImage.Close()
	if stats.Funcs[2].Peephole["gc-compact-range-loop"] != 1 || stats.Funcs[2].Peephole["gc-compact-range-stride"] != 0 {
		t.Fatalf("bad fallback %+v", stats.Funcs[2].Peephole)
	}
}
