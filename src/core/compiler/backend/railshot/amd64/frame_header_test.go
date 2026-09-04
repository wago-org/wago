//go:build (linux || darwin || windows) && amd64

package amd64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
)

func TestRegisterABIElidesWrapperFrameHeaderAMD64(t *testing.T) {
	// Fourteen i64 locals put the old frame at 136 bytes and the compact frame at
	// 120, crossing AMD64's imm8 frame-adjustment boundary as well as proving the
	// physical slot offsets move with the header.
	m := modFuncs(t, funcDef{body: []byte{
		0x01, 0x0e, 0x7e, // one local run: 14 x i64
		0x42, 0x01, 0x21, 0x0d, // local.set 13 (i64.const 1)
		0x20, 0x0d, 0x1a, // local.get 13; drop
		0x0b,
	}})
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
	_ = runCompiledAmd64u(t, cm)
}

func TestRegisterABICompactHeaderRemapsGCFrameLocalsAMD64(t *testing.T) {
	plan := &shared.GCFrameRootPlan{
		Candidate: true,
		Locals:    []shared.GCFrameLocal{{Index: 1, Offset: 24}},
	}
	f := fn{
		nLocals:            2,
		localSlot:          []uint32{0, 8},
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
