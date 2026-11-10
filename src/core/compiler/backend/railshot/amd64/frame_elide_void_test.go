//go:build (linux || darwin || windows) && amd64

package amd64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestFrameElidesRegisterOnlyVoidLeafAMD64(t *testing.T) {
	requireCompilerDiagnostics(t)
	m := modFuncs(t, funcDef{body: []byte{0x00, 0x0b}})
	compile := func(enabled bool) (*ModuleStats, int) {
		var stats ModuleStats
		cm, err := CompileModuleWith(m, CompileOptions{
			CompactNative: true,
			Stats:         optionalTestStats(&stats),
			Workers:       1,
			Optimizations: map[string]bool{"frame-elide": enabled},
		})
		if err != nil {
			t.Fatal(err)
		}
		if cm.CodeImage != nil {
			defer cm.CodeImage.Close()
		}
		return &stats, len(cm.Code)
	}
	rollback, rollbackBytes := compile(false)
	enabled, enabledBytes := compile(true)
	if rollback.Funcs[0].FrameBytes == 0 {
		t.Fatal("rollback void leaf unexpectedly has no frame")
	}
	if enabled.Funcs[0].FrameBytes != 0 || enabled.Funcs[0].Peephole["frame-adjust-elide"] != 1 || enabled.Funcs[0].Peephole["frame-adjust-elide-void"] != 1 {
		t.Fatalf("enabled void leaf stats = %+v", enabled.Funcs[0])
	}
	if enabledBytes >= rollbackBytes {
		t.Fatalf("enabled code = %d bytes, rollback = %d", enabledBytes, rollbackBytes)
	}
	cm, err := CompileModuleWith(m, CompileOptions{CompactNative: true, Workers: 1, Optimizations: map[string]bool{"frame-elide": true}})
	if err != nil {
		t.Fatal(err)
	}
	if cm.CodeImage != nil {
		defer cm.CodeImage.Close()
	}
	_ = runCompiledAmd64u(t, cm)
}

func TestFrameDoesNotElideExceptionHandlingVoidLeafAMD64(t *testing.T) {
	requireCompilerDiagnostics(t)
	m := modFuncs(t, funcDef{body: []byte{
		0x00,                                        // no locals
		0x1f, 0x40, 0x01, byte(wasm.CatchAll), 0x00, // try_table void, catch_all label 0
		0x0b, // end try_table
		0x0b, // end function
	}})
	var stats ModuleStats
	cm, err := CompileModuleWith(m, CompileOptions{Stats: optionalTestStats(&stats), Workers: 1})
	if err != nil {
		t.Fatal(err)
	}
	if cm.CodeImage != nil {
		defer cm.CodeImage.Close()
	}
	if diagnosticsEnabled {
		if stats.Funcs[0].FrameBytes == 0 || stats.Funcs[0].Peephole["frame-adjust-elide"] != 0 {
			t.Fatalf("exception-handling void leaf elided its frame: %+v", stats.Funcs[0])
		}
	}
}

func TestFrameDoesNotElideCallsWithEagerLocalSpillsAMD64(t *testing.T) {
	m := modFuncs(t,
		funcDef{params: []wasm.ValType{wasm.I64}, results: []wasm.ValType{wasm.I64}, body: []byte{0, 0x20, 0, 0x10, 1, 0x1a, 0x20, 0, 0x0b}},
		funcDef{params: []wasm.ValType{wasm.I64}, results: []wasm.ValType{wasm.I64}, body: []byte{0, 0x20, 0, 0x42, 1, 0x7c, 0x0b}},
	)
	var stats ModuleStats
	cm, err := CompileModuleWith(m, CompileOptions{Stats: optionalTestStats(&stats), Workers: 1,
		Optimizations: map[string]bool{"inline": false, "stack-reg": false, "frame-elide": true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if cm.CodeImage != nil {
		defer cm.CodeImage.Close()
	}
	if diagnosticsEnabled {
		if stats.Funcs[0].FrameBytes == 0 || stats.Funcs[0].Peephole["frame-adjust-elide"] != 0 {
			t.Fatal("caller's eagerly spilled local lost its frame home")
		}
	}
	if got := runCompiledAmd64u(t, cm, 11); got != 11 {
		t.Fatalf("caller local after call = %d, want 11", got)
	}
}
