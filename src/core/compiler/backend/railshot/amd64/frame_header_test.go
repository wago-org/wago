//go:build (linux || darwin || windows) && amd64 && (!wago_lean || wago_railshot_compact || wago_railshot_full)

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
	before := compactRegABIFrameHeader
	t.Cleanup(func() { compactRegABIFrameHeader = before })
	compile := func(enabled bool) (*ModuleStats, int) {
		compactRegABIFrameHeader = enabled
		var stats ModuleStats
		cm, err := CompileModuleWith(m, CompileOptions{CompactNative: true, Stats: &stats, Workers: 1})
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
	compactRegABIFrameHeader = true
	cm, err := CompileModuleWith(m, CompileOptions{CompactNative: true, Workers: 1})
	if err != nil {
		t.Fatal(err)
	}
	if cm.CodeImage != nil {
		defer cm.CodeImage.Close()
	}
	_ = runCompiledAmd64u(t, cm)
}

func TestRegisterABICompactHeaderRemapsGCFrameLocalsAMD64(t *testing.T) {
	plan := &shared.GCFrameRootPlan{
		Candidate:    true,
		LocalIndexes: []uint32{1},
		LocalOffsets: []uint32{24},
	}
	f := fn{
		nLocals:            2,
		localSlot:          []int{0, 8},
		localType:          []machineType{mtI32, mtI64},
		compactFrameHeader: true,
	}
	if !f.prepareCompactGCFrameHeader(plan) {
		t.Fatal("valid collector-local plan rejected")
	}
	if got := plan.LocalOffsets[0]; got != 8 {
		t.Fatalf("remapped root offset = %d, want 8", got)
	}
	bad := &shared.GCFrameRootPlan{Candidate: true, LocalIndexes: []uint32{2}, LocalOffsets: []uint32{32}}
	if f.prepareCompactGCFrameHeader(bad) {
		t.Fatal("out-of-range collector-local plan admitted")
	}
	withFixed := &shared.GCFrameRootPlan{Candidate: true, FixedOffsets: []uint32{16}}
	if f.prepareCompactGCFrameHeader(withFixed) {
		t.Fatal("fixed-root plan admitted")
	}
}
