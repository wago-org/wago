package embedded32

import (
	"encoding/binary"
	"math"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestSIMDPseudoMinMaxPreservesFirstOperandOnUnorderedOrTiedValues(t *testing.T) {
	if got := pminmax32(0, 0x80000000, false); got != 0 {
		t.Fatalf("f32 pmin(+0,-0)=%#x", got)
	}
	if got := pminmax32(0x80000000, 0, false); got != 0x80000000 {
		t.Fatalf("f32 pmin(-0,+0)=%#x", got)
	}
	if got := pminmax32(0x7fc01234, math.Float32bits(1), true); got != 0x7fc01234 {
		t.Fatalf("f32 pmax(NaN,1)=%#x", got)
	}
	if got := pminmax64(0, 0x8000000000000000, true); got != 0 {
		t.Fatalf("f64 pmax(+0,-0)=%#x", got)
	}
	if got := pminmax64(0x7ff8000000001234, math.Float64bits(1), false); got != 0x7ff8000000001234 {
		t.Fatalf("f64 pmin(NaN,1)=%#x", got)
	}
}

func TestSIMDHelperRegistryAndDispatch(t *testing.T) {
	count := 0
	for op := uint32(0); op <= 512; op++ {
		got := SIMDHelperValid(op)
		want := wasm.SIMDSubopcodeValid(op)
		if got != want {
			t.Errorf("opcode %d: helper=%v want=%v", op, got, want)
		}
		if got {
			count++
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Errorf("opcode %d panicked: %v", op, r)
					}
				}()
				f := SIMDFrame{Op: op}
				RunSIMD(&f)
			}()
		}
	}
	if count != 256 {
		t.Fatalf("helper registry has %d opcodes, want 256", count)
	}
}

func TestSIMDPackedArithmeticAndSaturation(t *testing.T) {
	var f SIMDFrame
	f.Op = 110
	for i := range f.A {
		f.A[i], f.B[i] = 0xff, 2
	}
	RunSIMD(&f)
	for i, x := range f.Out {
		if x != 1 {
			t.Fatalf("i8x16.add lane %d = %d", i, x)
		}
	}
	f.Op = 111
	for i := range f.A {
		f.A[i], f.B[i] = 0x7f, 1
	}
	RunSIMD(&f)
	for i, x := range f.Out {
		if x != 0x7f {
			t.Fatalf("i8x16.add_sat_s lane %d = %#x", i, x)
		}
	}
}

func TestSIMDFloatEdgesAndConversions(t *testing.T) {
	var f SIMDFrame
	f.Op = 244
	binary.LittleEndian.PutUint64(f.A[0:], 0)
	binary.LittleEndian.PutUint64(f.B[0:], 1<<63)
	binary.LittleEndian.PutUint64(f.A[8:], 0x7ff0000000000001)
	binary.LittleEndian.PutUint64(f.B[8:], math.Float64bits(1))
	RunSIMD(&f)
	if got := binary.LittleEndian.Uint64(f.Out[0:]); got != 1<<63 {
		t.Fatalf("f64x2.min zero = %#x", got)
	}
	if got := binary.LittleEndian.Uint64(f.Out[8:]); got != 0x7ff8000000000001 {
		t.Fatalf("f64x2.min NaN = %#x", got)
	}

	f = SIMDFrame{Op: 252}
	binary.LittleEndian.PutUint64(f.A[0:], math.Float64bits(math.NaN()))
	binary.LittleEndian.PutUint64(f.A[8:], math.Float64bits(-12.75))
	RunSIMD(&f)
	if got := binary.LittleEndian.Uint32(f.Out[0:]); got != 0 {
		t.Fatalf("trunc NaN = %#x", got)
	}
	if got := binary.LittleEndian.Uint32(f.Out[4:]); got != 0xfffffff4 {
		t.Fatalf("trunc -12.75 = %#x", got)
	}
	if got := binary.LittleEndian.Uint64(f.Out[8:]); got != 0 {
		t.Fatalf("zero upper lanes = %#x", got)
	}
}

func TestSIMDRoundingPreservesIntegralBitsAndQuietsNaNs(t *testing.T) {
	for _, op := range []uint32{103, 104, 105, 106} {
		f := SIMDFrame{Op: op}
		for i, bits := range []uint32{0x6c7f4d7b, 0x6511a2b4, 0x7fa00000, 0xffa00000} {
			binary.LittleEndian.PutUint32(f.A[i*4:], bits)
		}
		RunSIMD(&f)
		for i, want := range []uint32{0x6c7f4d7b, 0x6511a2b4, 0x7fe00000, 0xffe00000} {
			if got := binary.LittleEndian.Uint32(f.Out[i*4:]); got != want {
				t.Errorf("f32 op %d lane %d = %#x, want %#x", op, i, got, want)
			}
		}
	}

	for _, op := range []uint32{116, 117, 122, 148} {
		f := SIMDFrame{Op: op}
		binary.LittleEndian.PutUint64(f.A[0:], 0x458fe9af5b5e16fa)
		binary.LittleEndian.PutUint64(f.A[8:], 0xfff4000000000000)
		RunSIMD(&f)
		if got := binary.LittleEndian.Uint64(f.Out[0:]); got != 0x458fe9af5b5e16fa {
			t.Errorf("f64 op %d integral lane = %#x", op, got)
		}
		if got := binary.LittleEndian.Uint64(f.Out[8:]); got != 0xfffc000000000000 {
			t.Errorf("f64 op %d signaling NaN lane = %#x", op, got)
		}
	}
}

func TestSIMDPromoteQuietsSignalingNaNs(t *testing.T) {
	f := SIMDFrame{Op: 95}
	binary.LittleEndian.PutUint32(f.A[0:], 0x7fa00000)
	binary.LittleEndian.PutUint32(f.A[4:], 0xffa00000)
	RunSIMD(&f)
	if got := binary.LittleEndian.Uint64(f.Out[0:]); got != 0x7ffc000000000000 {
		t.Errorf("positive signaling NaN = %#x", got)
	}
	if got := binary.LittleEndian.Uint64(f.Out[8:]); got != 0xfffc000000000000 {
		t.Errorf("negative signaling NaN = %#x", got)
	}
}

func TestSIMDImmediateLaneAndMemoryOperations(t *testing.T) {
	var imm V128
	for i := range imm {
		imm[i] = byte(15 - i)
	}
	f := SIMDFrame{Op: 12, Immediate: imm}
	RunSIMD(&f)
	if f.Out != imm {
		t.Fatal("v128.const mismatch")
	}
	f = SIMDFrame{Op: 13, A: imm, B: V128{16, 17, 18, 19}, Immediate: V128{0, 15, 16, 17, 1, 14, 18, 19}}
	RunSIMD(&f)
	want := V128{15, 0, 16, 17, 14, 1, 18, 19}
	for i := 0; i < 8; i++ {
		if f.Out[i] != want[i] {
			t.Fatalf("shuffle byte %d=%d want=%d", i, f.Out[i], want[i])
		}
	}

	f = SIMDFrame{Op: 23, A: imm, Scalar: 0xaa, Lane: 3}
	RunSIMD(&f)
	if f.Out[3] != 0xaa {
		t.Fatalf("replace lane=%#x", f.Out[3])
	}
	f = SIMDFrame{Op: 21, A: V128{0x80}, Lane: 0}
	RunSIMD(&f)
	if f.ScalarOut != 0xffffff80 {
		t.Fatalf("extract signed=%#x", f.ScalarOut)
	}

	mem := make([]byte, 32)
	for i := range mem {
		mem[i] = byte(i)
	}
	f = SIMDFrame{Op: 0, Memory: mem, Address: 3}
	RunSIMD(&f)
	if f.Trap != TrapNone || f.Out[0] != 3 || f.Out[15] != 18 {
		t.Fatalf("v128.load trap=%d out=%v", f.Trap, f.Out)
	}
	before := append([]byte(nil), mem...)
	f = SIMDFrame{Op: 11, Memory: mem, Address: 20, A: imm}
	RunSIMD(&f)
	if f.Trap != TrapMemoryOutOfBounds {
		t.Fatalf("store trap=%d", f.Trap)
	}
	for i := range mem {
		if mem[i] != before[i] {
			t.Fatalf("OOB store changed byte %d", i)
		}
	}
	f = SIMDFrame{Op: 87, Memory: mem, Address: 5, A: imm, Lane: 1}
	RunSIMD(&f)
	if f.Trap != TrapNone || binary.LittleEndian.Uint64(f.Out[8:]) != binary.LittleEndian.Uint64(mem[5:13]) {
		t.Fatal("load64_lane mismatch")
	}
}

func TestSIMDRelaxedDeterministicProjections(t *testing.T) {
	f := SIMDFrame{Op: 265}
	for i := range f.A {
		f.A[i], f.B[i], f.C[i] = 0xaa, 0x55, 0xf0
	}
	RunSIMD(&f)
	for i, x := range f.Out {
		if x != 0xa5 {
			t.Fatalf("laneselect byte %d = %#x", i, x)
		}
	}
	f = SIMDFrame{Op: 261}
	for i := 0; i < 4; i++ {
		binary.LittleEndian.PutUint32(f.A[i*4:], math.Float32bits(2))
		binary.LittleEndian.PutUint32(f.B[i*4:], math.Float32bits(3))
		binary.LittleEndian.PutUint32(f.C[i*4:], math.Float32bits(4))
	}
	RunSIMD(&f)
	for i := 0; i < 4; i++ {
		if got := math.Float32frombits(binary.LittleEndian.Uint32(f.Out[i*4:])); got != 10 {
			t.Fatalf("madd lane %d = %v", i, got)
		}
	}
}
