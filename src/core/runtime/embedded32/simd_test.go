package embedded32

import (
	"encoding/binary"
	"math"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestSIMDHelperRegistryAndDispatch(t *testing.T) {
	count := 0
	for op := uint32(0); op <= 512; op++ {
		got := SIMDHelperValid(op)
		want := wasm.SIMDSubopcodeValid(op) && !simdDirectImmediate(op)
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
	if count != 218 {
		t.Fatalf("helper registry has %d opcodes, want 218", count)
	}
}

func simdDirectImmediate(op uint32) bool {
	if op == 12 || op == 13 || op <= 11 || op == 84 || op == 85 || op == 86 || op == 87 || op == 88 || op == 89 || op == 90 || op == 91 || op == 92 || op == 93 {
		return true
	}
	return op >= 21 && op <= 34
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
