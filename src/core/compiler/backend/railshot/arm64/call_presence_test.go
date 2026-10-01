//go:build (linux || darwin || windows) && arm64

package arm64

import (
	"encoding/binary"
	"testing"

	a64 "github.com/wago-org/wago/src/core/encoder/arm64"
)

func TestCallPresencePreventsFrameElisionARM64(t *testing.T) {
	requireCompilerDiagnostics(t)
	// Both functions return void and have no locals or operand spills. The caller
	// still needs its frame for call alignment, regardless of the spill policy.
	m := modFuncs(t,
		funcDef{body: []byte{0, 0x10, 1, 0x0b}},
		funcDef{body: []byte{0, 0x03, 0x40, 0x0b, 0x0b}},
	)
	for _, stackReg := range []bool{false, true} {
		var stats ModuleStats
		cm, err := CompileModuleWith(m, CompileOptions{Stats: &stats, Optimizations: map[string]bool{"stack-reg": stackReg, "frame-elide-reghomed": true}})
		if err != nil {
			t.Fatal(err)
		}
		defer cm.CodeImage.Close()
		// The compact body frame can be empty; the separate FP/LR record is not
		// included in FrameBytes. A physical caller must still avoid leaf elision.
		if stats.Funcs[0].Peephole["frame-adjust-elide"] != 0 {
			t.Errorf("stack-reg=%v: call-making function elided its frame: %+v", stackReg, stats.Funcs[0])
		}
	}
}

func TestCallPresenceDisablesConstantPreloadsARM64(t *testing.T) {
	f := fn{makesCalls: true, policy: currentCodegenPolicy()}
	f.preloadFloatConsts([]byte{0x44, 0, 0, 0, 0, 0, 0, 0xf0, 0x3f, 0x1a, 0x0b})
	vector := append([]byte{0xfd, 12, 1}, make([]byte, 15)...)
	f.preloadV128Consts(append(append(vector, 0x1a), append(vector, 0x1a, 0x0b)...))
	h := funcHintView{loopIntConstCount: 1, loopIntConstTypes: 2, loopIntConst: [4]int64{0x123456789abcdef}}
	f.preloadLoopIntConsts(&h)
	if len(f.fconsts) != 0 || len(f.vconsts) != 0 || f.iconstN != 0 {
		t.Fatalf("call-making function reserved constants with STACK_REG disabled")
	}
}

func TestCallPresencePreservesLinkRegisterARM64(t *testing.T) {
	m := modFuncs(t, funcDef{body: []byte{0, 0x10, 1, 0x0b}}, funcDef{body: []byte{0, 0x03, 0x40, 0x0b, 0x0b}})
	for _, regABI := range []bool{false, true} {
		cm, err := CompileModuleWith(m, CompileOptions{Optimizations: map[string]bool{"stack-reg": false, "reg-abi": regABI}})
		if err != nil {
			t.Fatal(err)
		}
		defer cm.CodeImage.Close()
		entry := cm.Entry[0]
		if regABI {
			entry = cm.InternalEntry[0]
		}
		restoresLink := false
		for at := entry; at+8 <= cm.Entry[1]; at += 4 {
			if binary.LittleEndian.Uint32(cm.Code[at:]) == 0xa8c17bfd && binary.LittleEndian.Uint32(cm.Code[at+4:]) == 0xd65f03c0 {
				restoresLink = true
			}
		}
		if !restoresLink {
			t.Errorf("reg-abi=%v: caller does not restore FP/LR before RET", regABI)
		}
		if got := binary.LittleEndian.Uint32(cm.Code[entry:]); got != 0xa9bf7bfd {
			t.Errorf("reg-abi=%v: caller starts with %#x, want STP FP,LR,[SP,#-16]!", regABI, got)
		}
	}
}

func TestCallPresenceTailFrameReleaseARM64(t *testing.T) {
	f := fn{a: &a64.Asm{}, makesCalls: true}
	f.emitTailFrameRelease()
	if got := binary.LittleEndian.Uint32(f.a.B[f.a.Len()-4:]); got != 0xa8c17bfd {
		t.Fatalf("tail release ends with %#x, want LDP FP,LR,[SP],#16", got)
	}
}

func TestCallPresenceFrameHeadroomARM64(t *testing.T) {
	f := fn{makesCalls: true}
	if err := f.validateFrameSize(nativeFrameStackFenceHeadroom(false)); err == nil {
		t.Fatal("caller frame did not reserve space for the FP/LR record")
	}
	if err := f.validateFrameSize(nativeFrameStackFenceHeadroom(true)); err != nil {
		t.Fatal(err)
	}
}
