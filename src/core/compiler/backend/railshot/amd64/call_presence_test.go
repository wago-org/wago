//go:build (linux || darwin || windows) && amd64

package amd64

import "testing"

func TestCallPresencePreventsFrameElisionAMD64(t *testing.T) {
	requireCompilerDiagnostics(t)
	// Both functions return void and have no locals or operand spills. The caller
	// still needs its frame for call alignment, regardless of the spill policy.
	m := modFuncs(t,
		funcDef{body: []byte{0, 0x10, 1, 0x0b}},
		funcDef{body: []byte{0, 0x03, 0x40, 0x0b, 0x0b}},
	)
	for _, stackReg := range []bool{false, true} {
		var stats ModuleStats
		cm, err := CompileModuleWith(m, CompileOptions{Stats: &stats, Optimizations: map[string]bool{"stack-reg": stackReg, "frame-elide": true}})
		if err != nil {
			t.Fatal(err)
		}
		defer cm.CodeImage.Close()
		if stats.Funcs[0].FrameBytes == 0 || stats.Funcs[0].Peephole["frame-adjust-elide"] != 0 {
			t.Errorf("stack-reg=%v: call-making function elided its frame: %+v", stackReg, stats.Funcs[0])
		}
	}
}
