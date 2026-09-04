//go:build (linux || darwin) && arm64

package arm64

import "testing"

func TestFrameElidesRegisterOnlyVoidLeafArm64(t *testing.T) {
	m := modFuncs(t, funcDef{body: []byte{0x00, 0x0b}})
	var stats ModuleStats
	cm, err := CompileModuleWith(m, CompileOptions{CompactNative: true, Stats: &stats, Workers: 1})
	if err != nil {
		t.Fatal(err)
	}
	if cm.CodeImage != nil {
		defer cm.CodeImage.Close()
	}
	if stats.Funcs[0].FrameBytes != 0 || stats.Funcs[0].Peephole["frame-adjust-elide"] != 1 || stats.Funcs[0].Peephole["frame-adjust-elide-void"] != 1 {
		t.Fatalf("void leaf stats = %+v", stats.Funcs[0])
	}
	_ = runArm64Internal2(t, m, 0, 0)
}
