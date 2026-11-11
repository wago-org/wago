//go:build (linux || darwin) && arm64

package arm64

import (
	"encoding/binary"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	enc "github.com/wago-org/wago/src/core/encoder/arm64"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/src/core/runtime/arm64spike"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

// Inject a concrete i32 register with dirty upper bits. This is a backend-state
// control, not evidence that a valid Wasm producer can create this state.
func TestVectorSelectDirtyUpperCondition(t *testing.T) {
	aValues := [2]uint64{0x123456789abcdef0, 0xfedcba9876543210}
	bValues := [2]uint64{0x8000000000000001, 0x7ff0000000001234}
	for lane := 0; lane < 2; lane++ {
		var a enc.Asm
		for i, value := range aValues {
			a.MovImm64(X9, value)
			a.NeonInsD(16, X9, byte(i))
		}
		for i, value := range bValues {
			a.MovImm64(X9, value)
			a.NeonInsD(17, X9, byte(i))
		}
		f := &fn{a: &a, s: newStack(), policy: currentCodegenPolicy()}
		f.pushVReg(16)
		f.pushVReg(17)
		f.pushReg(X0, mtI32)
		start := a.Len()
		f.emitSelect()
		if a.Len()-start != 8 {
			t.Fatalf("select length=%d, want branch plus full-vector copy", a.Len()-start)
		}
		branch := binary.LittleEndian.Uint32(a.B[start:])
		if branch&0xff00001f != 0x35000000 || branch>>5&0x7ffff != 2 {
			t.Errorf("select branch=%08x, want CBNZ W0 over one instruction", branch)
		}
		a.NeonUmovD(X0, f.s.back().st.reg, byte(lane))
		a.Ret()
		mem, entry, err := coreruntime.MapCode(a.B)
		if err != nil {
			t.Fatal(err)
		}
		for _, cond := range []uint64{0, 1 << 32, 1, 1<<32 | 1, ^uint64(0)} {
			want := bValues[lane]
			if uint32(cond) != 0 {
				want = aValues[lane]
			}
			got := uint64(arm64spike.Call2(entry, uintptr(cond), 0))
			if got != want {
				t.Errorf("lane=%d condition=%x: got=%x want=%x", lane, cond, got, want)
			}
		}
		coreruntime.Unmap(mem)
	}
}

func TestVectorSelectWrappedI64Condition(t *testing.T) {
	aValues := [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	bValues := [16]byte{0x80, 0, 0, 0, 0, 0, 0, 1, 0x7f, 0xf0, 0, 0, 0, 0, 0x12, 0x34}
	for _, cond := range []int64{1 << 32, 1<<32 | 1} {
		body := []byte{0}
		body = append(body, simdConst(aValues)...)
		body = append(body, simdConst(bValues)...)
		body = append(body, 0x42)
		body = append(body, wasmtest.SLEB64(cond)...)
		body = append(body, 0xa7, 0x1b, 0x0b) // i32.wrap_i64; select; end
		m := mod1(t, nil, []wasm.ValType{wasm.V128}, body)
		if err := wasm.ValidateModule(m); err != nil {
			t.Fatal(err)
		}
		want := bValues
		if uint32(cond) != 0 {
			want = aValues
		}
		if got := runArm64V128(t, m); got != want {
			t.Fatalf("wrapped condition=%x: got=%x want=%x", cond, got, want)
		}
	}
}
