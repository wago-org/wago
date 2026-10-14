//go:build (linux || darwin || windows) && arm64 && !tinygo

package runtime

import (
	"bytes"
	"encoding/binary"
	goruntime "runtime"
	"strings"
	"testing"

	"github.com/wago-org/wago/internal/runtimebridge"
)

//go:noinline
//go:norace
func nativeCallPureLeafTest(v int32) int32 { return v + 1 }

//go:noinline
//go:norace
func nativeCallForwardTest(v int32) int32 { return nativeCallPureLeafTest(v) }
func TestNativeScalarSinglePureCallProof(t *testing.T) {
	v := goruntime.Version()
	if !scalarGoABI(v) || (v != "go1.27" && !strings.HasPrefix(v, "go1.27.")) {
		t.Skip("single-call proof is restricted to Go1.27")
	}
	t.Setenv("WAGO_ARM64_NATIVE_SCALAR_CALL", "")
	ctrl := make([]byte, ctrlFrameSize)
	t.Cleanup(func() { unregisterNativeScalarLeaf(ctrl) })
	ok, err := RegisterNativeScalarLeaf(runtimebridge.GrantHostScalarCall(), ctrl, nativeCallForwardTest, 1)
	if err != nil || !ok {
		t.Fatalf("pure single-helper wrapper rejected: %v,%v", ok, err)
	}
	value, _ := nativeLeafHosts.Load(slicePtr(ctrl))
	leaf := value.(nativeScalarLeaf)
	direct := int(leaf.direct - slicePtr(leaf.code))
	if binary.LittleEndian.Uint32(leaf.code[direct:]) != 0xaa0303e2 {
		t.Fatal("callee writes consumed the argument-wrapper result register")
	}
	calleeCtrl := make([]byte, ctrlFrameSize)
	t.Cleanup(func() { unregisterNativeScalarLeaf(calleeCtrl) })
	ok, err = RegisterNativeScalarLeaf(runtimebridge.GrantHostScalarCall(), calleeCtrl, nativeCallPureLeafTest, 1)
	if err != nil || !ok {
		t.Fatalf("callee registration: %v,%v", ok, err)
	}
	calleeValue, _ := nativeLeafHosts.Load(slicePtr(calleeCtrl))
	callee := calleeValue.(nativeScalarLeaf)
	if !bytes.Equal(leaf.code, callee.code) {
		t.Fatal("pure wrapper must lower to exactly the independently proven callee body")
	}

}

var nativeCallEffectState int32

//go:noinline
//go:norace
func nativeCallStatefulLeafTest(v int32) int32 {
	nativeCallEffectState++
	return v + nativeCallEffectState
}

//go:noinline
//go:norace
func nativeCallStatefulForwardTest(v int32) int32 { return nativeCallStatefulLeafTest(v) }

//go:noinline
//go:norace
func nativeCallTwiceTest(v int32) int32 { return nativeCallPureLeafTest(nativeCallPureLeafTest(v)) }

//go:noinline
//go:norace
func nativeCallPostMathTest(v int32) int32 { return nativeCallPureLeafTest(v) + 7 }
func TestNativeScalarSingleCallRejectsEffectsAndOtherBodies(t *testing.T) {
	t.Setenv("WAGO_ARM64_NATIVE_SCALAR_CALL", "1")
	captured := int32(5)
	for _, fn := range []any{nativeCallStatefulForwardTest, nativeCallTwiceTest, nativeCallPostMathTest, func(v int32) int32 { captured++; return nativeCallPureLeafTest(v + captured) }, func(v int64) int64 { return v + 1 }} {
		ctrl := make([]byte, ctrlFrameSize)
		ok, err := RegisterNativeScalarLeaf(runtimebridge.GrantHostScalarCall(), ctrl, fn, 1)
		if err != nil || ok {
			if ok {
				unregisterNativeScalarLeaf(ctrl)
			}
			t.Fatalf("unsafe or unsupported callback admitted: %T %v,%v", fn, ok, err)
		}
	}
}
func TestNativeScalarSingleCallKillSwitch(t *testing.T) {
	for _, mode := range []string{"0"} {
		t.Setenv("WAGO_ARM64_NATIVE_SCALAR_CALL", mode)
		ctrl := make([]byte, ctrlFrameSize)
		ok, err := RegisterNativeScalarLeaf(runtimebridge.GrantHostScalarCall(), ctrl, nativeCallForwardTest, 1)
		if err != nil || ok {
			if ok {
				unregisterNativeScalarLeaf(ctrl)
			}
			t.Fatalf("mode=%q admitted=%t err=%v", mode, ok, err)
		}
	}
}
func TestScalarRelativeTargetChecksBounds(t *testing.T) {
	for _, tc := range []struct {
		pc     uintptr
		word   uint32
		ok     bool
		target uintptr
	}{{64, 0x94000003, true, 76}, {64, 0x97ffffff, true, 60}, {0, 0x97ffffff, false, 0}, {^uintptr(0) - 3, 0x94000001, false, 0}, {64, 0xd65f03c0, false, 0}} {
		got, ok := scalarRelativeTarget(tc.pc, tc.word, 0x94000000)
		if ok != tc.ok || ok && got != tc.target {
			t.Fatalf("pc=%x word=%x got=%x,%t want=%x,%t", tc.pc, tc.word, got, ok, tc.target, tc.ok)
		}
	}
}
