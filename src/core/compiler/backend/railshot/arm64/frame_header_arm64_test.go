//go:build (linux || darwin) && arm64

package arm64

import (
	"testing"
)

func TestRegisterABIElidesWrapperFrameHeaderArm64(t *testing.T) {
	callee := make([]byte, 0, 202)
	callee = append(callee, 0x00)
	for range 200 {
		callee = append(callee, 0x01) // nop; keep the call above inline admission.
	}
	callee = append(callee, 0x0b)
	m := modFuncs(t,
		funcDef{body: []byte{0x00, 0x10, 0x01, 0x0b}},
		funcDef{body: callee},
	)
	before := compactRegABIFrameHeader
	t.Cleanup(func() { compactRegABIFrameHeader = before })
	compile := func(enabled bool) (*ModuleStats, int) {
		compactRegABIFrameHeader = enabled
		var stats ModuleStats
		objective := OptimizeSize
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
	if got := enabled.Funcs[0].Peephole["frame-header-elide"]; got != 1 {
		t.Fatalf("frame-header-elide hits = %d, want 1", got)
	}
	if got, want := enabled.Funcs[0].FrameBytes, rollback.Funcs[0].FrameBytes-16; got != want {
		t.Fatalf("enabled frame = %d, want rollback %d - 16 = %d", got, rollback.Funcs[0].FrameBytes, want)
	}
	if enabledBytes >= rollbackBytes {
		t.Fatalf("enabled code = %d bytes, rollback = %d", enabledBytes, rollbackBytes)
	}
	_ = runArm64Internal2(t, m, 0, 0)
}
