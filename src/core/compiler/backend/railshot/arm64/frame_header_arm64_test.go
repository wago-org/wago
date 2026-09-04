//go:build (linux || darwin) && arm64

package arm64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
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
	var stats ModuleStats
	cm, err := CompileModuleWith(m, CompileOptions{CompactNative: true, Stats: &stats, Workers: 1})
	if err != nil {
		t.Fatal(err)
	}
	if cm.CodeImage != nil {
		defer cm.CodeImage.Close()
	}
	if got := stats.Funcs[0].Peephole["frame-header-elide"]; got != 1 {
		t.Fatalf("frame-header-elide hits = %d, want 1", got)
	}
	_ = runArm64Internal2(t, m, 0, 0)
}

func TestRegisterABICompactHeaderRemapsGCFrameLocalsArm64(t *testing.T) {
	plan := &shared.GCFrameRootPlan{
		Candidate: true,
		Locals:    []shared.GCFrameLocal{{Index: 1, Offset: 24}},
	}
	f := fn{
		nLocals:            2,
		localSlot:          []uint32{0, 1},
		localType:          []machineType{mtI32, mtI64},
		compactFrameHeader: true,
	}
	if !f.prepareCompactGCFrameHeader(plan) {
		t.Fatal("valid collector-local plan rejected")
	}
	if got := plan.Locals[0].Offset; got != 8 {
		t.Fatalf("remapped root offset = %d, want 8", got)
	}
	bad := &shared.GCFrameRootPlan{Candidate: true, Locals: []shared.GCFrameLocal{{Index: 2, Offset: 32}}}
	if f.prepareCompactGCFrameHeader(bad) {
		t.Fatal("out-of-range collector-local plan admitted")
	}
	withFixed := &shared.GCFrameRootPlan{Candidate: true}
	if !withFixed.SetFixedOffsets([]uint32{16}) {
		t.Fatal("failed to set fixed roots")
	}
	if f.prepareCompactGCFrameHeader(withFixed) {
		t.Fatal("fixed-root plan admitted")
	}
}
