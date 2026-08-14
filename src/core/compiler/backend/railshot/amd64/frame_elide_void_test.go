//go:build (linux || darwin || windows) && amd64

package amd64

import "testing"

func TestFrameElidesRegisterOnlyVoidLeafAMD64(t *testing.T) {
	m := modFuncs(t, funcDef{body: []byte{0x00, 0x0b}})
	before := frameElideVoid
	t.Cleanup(func() { frameElideVoid = before })
	compile := func(enabled bool) (*ModuleStats, int) {
		frameElideVoid = enabled
		objective := OptimizeSize
		var stats ModuleStats
		cm, err := CompileModuleWith(m, CompileOptions{Objective: &objective, Stats: &stats, Workers: 1})
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
	frameElideVoid = true
	objective := OptimizeSize
	cm, err := CompileModuleWith(m, CompileOptions{Objective: &objective, Workers: 1})
	if err != nil {
		t.Fatal(err)
	}
	if cm.CodeImage != nil {
		defer cm.CodeImage.Close()
	}
	_ = runCompiledAmd64u(t, cm)
}
