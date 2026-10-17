//go:build linux && !tinygo

package riscv64

import (
	"context"
	"errors"
	"fmt"
	"math/bits"
	"runtime"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	native "github.com/wago-org/wago/src/core/runtime/riscv64"
)

func integerFixture(op byte, wide, unary, resultWide bool) fixtureFunc {
	typ, result := byte(0x7f), byte(0x7f)
	if wide {
		typ = 0x7e
	}
	if resultWide {
		result = 0x7e
	}
	f := fixtureFunc{params: []byte{typ}, result: result, body: []byte{0x20, 0}}
	if !unary {
		f.params = append(f.params, typ)
		f.body = append(f.body, 0x20, 1)
	}
	f.body = append(f.body, op, 0x0b)
	return f
}

func integerInstance(t *testing.T, f fixtureFunc, force bool) *Instance {
	t.Helper()
	c, err := CompileWithOptions(binaryModule(f), CompileOptions{ForceFallback: force})
	if err != nil {
		t.Fatal(err)
	}
	if !c.functions[0].Fallback {
		t.Fatal("expected actual whole-function fallback", c.functions)
	}
	if runtime.GOARCH != "riscv64" {
		t.Skip("RV64 execution only; admission passed")
	}
	i, err := c.Instantiate(native.MinNativeStackBytes)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := i.Close(); err != nil {
			t.Error(err)
		}
	})
	return i
}

func expectInteger(t *testing.T, i *Instance, want uint64, trap uint32, args ...uint64) {
	t.Helper()
	got, err := i.Invoke(context.Background(), "a", args...)
	if trap != 0 {
		var tr *native.TrapError
		if !errors.As(err, &tr) || tr.Code != trap {
			t.Fatalf("args=%x got=%x trap=%v want=%d", args, got, err, trap)
		}
	} else if err != nil || got != want {
		t.Fatalf("args=%x got=%x want=%x err=%v", args, got, want, err)
	}
}

func TestIntegerWholeFunctionSelection(t *testing.T) {
	good := fixtureFunc{result: 0x7f, body: []byte{0x41, 1, 0x0b}}
	for name, f := range map[string]fixtureFunc{
		"tiny-leaf": {params: []byte{0x7f, 0x7f}, result: 0x7f, body: []byte{0x20, 0, 0x20, 1, 0x6a, 0x0b}},
		"loop":      {result: 0x7f, body: []byte{0x03, 0x40, 0x0b, 0x41, 0, 0x0b}},
		"division":  integerFixture(0x6d, false, false, false),
	} {
		t.Run(name, func(t *testing.T) {
			f.unexported = true
			c, err := Compile(binaryModule(good, f))
			if err != nil || c.functions[0].Fallback || !c.functions[1].Fallback {
				t.Fatal(c, err)
			}
		})
	}
	c, err := CompileWithOptions(binaryModule(good, numericFixture(false, 0x6a, false)), CompileOptions{ForceFallback: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range c.functions {
		if !f.Fallback {
			t.Fatal(f)
		}
	}
	// Reject even valid unsupported operations buried in unreachable code.
	deadFP := fixtureFunc{result: 0x7f, body: []byte{0, 2, 0x40, 0x43, 0, 0, 0, 0, 0x1a, 0x0b, 0x0b}}
	if c, err := Compile(binaryModule(good, deadFP)); c != nil || !errors.Is(err, ErrUnsupported) {
		t.Fatal(c, err)
	}
}

func TestIntegerAdmissionBounds(t *testing.T) {
	for name, body := range map[string][]byte{
		"instructions": append(append(makeNops(integerBudget+1), 0x41, 0), 0x0b),
		"controls":     nestedBlocks(integerBudget + 1),
		"operands":     excessiveOperands(513),
		"branch-table": excessiveTable(65536),
	} {
		t.Run(name, func(t *testing.T) {
			if c, err := CompileWithOptions(binaryModule(fixtureFunc{result: 0x7f, body: body}), CompileOptions{ForceFallback: true}); c != nil || !errors.Is(err, ErrUnsupported) {
				t.Fatal(c, err)
			}
		})
	}
}
func makeNops(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = 1
	}
	return b
}
func nestedBlocks(n int) []byte {
	var b []byte
	for range n {
		b = append(b, 2, 0x40)
	}
	for range n {
		b = append(b, 0x0b)
	}
	return append(b, 0x41, 0, 0x0b)
}
func excessiveOperands(n int) []byte {
	var b []byte
	for range n {
		b = append(b, 0x41, 0)
	}
	for range n - 1 {
		b = append(b, 0x1a)
	}
	return append(b, 0x0b)
}
func excessiveTable(n int) []byte {
	b := []byte{0x41, 7, 0x41, 0, 0x0e}
	b = uleb(b, uint32(n))
	for range n + 1 {
		b = append(b, 0)
	}
	return append(b, 0x0b)
}

func TestNativeIntegerDivisionRemainderAndRotate(t *testing.T) {
	for _, wide := range []bool{false, true} {
		base := byte(0x6d)
		if wide {
			base = 0x7f
		}
		for k := 0; k < 6; k++ {
			op := base + byte(k)
			if k >= 4 {
				op = 0x77 + byte(k-4)
				if wide {
					op = 0x89 + byte(k-4)
				}
			}
			t.Run(fmt.Sprintf("%x", op), func(t *testing.T) {
				i := integerInstance(t, integerFixture(op, wide, false, wide), false)
				for _, pair := range dirtyInputs() {
					a, b := pair[0], pair[1]
					if !wide {
						a = uint64(uint32(a))
						b = uint64(uint32(b))
					}
					sa, sb := int64(a), int64(b)
					min := int64(-1 << 63)
					if !wide {
						sa = int64(int32(a))
						sb = int64(int32(b))
						min = -1 << 31
					}
					var want uint64
					var trap uint32
					switch k {
					case 0, 1, 2, 3:
						if b == 0 {
							trap = TrapDivideByZero
						} else if k == 0 && sa == min && sb == -1 {
							trap = TrapIntegerOverflow
						} else {
							switch k {
							case 0:
								want = uint64(sa / sb)
							case 1:
								want = a / b
							case 2:
								want = uint64(sa % sb)
							case 3:
								want = a % b
							}
						}
					case 4, 5:
						shift := int(b & 63)
						if !wide {
							shift = int(b & 31)
						}
						if k == 5 {
							shift = -shift
						}
						if wide {
							want = bits.RotateLeft64(a, shift)
						} else {
							want = uint64(bits.RotateLeft32(uint32(a), shift))
						}
					}
					if !wide {
						want = uint64(uint32(want))
					}
					expectInteger(t, i, want, trap, pair[0], pair[1])
				}
				// Each trap is followed by success on the same stack/trap/result cells.
				if k < 4 {
					expectInteger(t, i, 0, TrapDivideByZero, 17, 0)
				}
				want := uint64(2)
				if k == 2 || k == 3 {
					want = 1
				}
				if k == 4 {
					want = 14
				}
				if k == 5 {
					if wide {
						want = 0x8000000000000003
					} else {
						want = 0x80000003
					}
				}
				b := uint64(1)
				if k < 4 {
					b = 3
				}
				expectInteger(t, i, want, 0, 7, b)
			})
		}
	}
}

func TestNativeIntegerUnaryAndConversions(t *testing.T) {
	for _, op := range []byte{0x45, 0x50, 0x67, 0x68, 0x69, 0x79, 0x7a, 0x7b, 0xa7, 0xac, 0xad, 0xc0, 0xc1, 0xc2, 0xc3, 0xc4} {
		t.Run(fmt.Sprintf("%x", op), func(t *testing.T) {
			_, w, _, rw, ok := integerOpcode(op)
			if !ok {
				t.Fatal(op)
			}
			i := integerInstance(t, integerFixture(op, w, true, rw), false)
			for _, p := range dirtyInputs() {
				raw := p[0]
				a := raw
				if !w {
					a = uint64(uint32(a))
				}
				var want uint64
				switch op {
				case 0x45, 0x50:
					if a == 0 {
						want = 1
					}
				case 0x67:
					want = uint64(bits.LeadingZeros32(uint32(a)))
				case 0x68:
					want = uint64(bits.TrailingZeros32(uint32(a)))
				case 0x69:
					want = uint64(bits.OnesCount32(uint32(a)))
				case 0x79:
					want = uint64(bits.LeadingZeros64(a))
				case 0x7a:
					want = uint64(bits.TrailingZeros64(a))
				case 0x7b:
					want = uint64(bits.OnesCount64(a))
				case 0xa7, 0xad:
					want = uint64(uint32(a))
				case 0xac, 0xc4:
					want = uint64(int64(int32(a)))
				case 0xc0, 0xc2:
					want = uint64(int64(int8(a)))
				case 0xc1, 0xc3:
					want = uint64(int64(int16(a)))
				}
				if !rw {
					want = uint64(uint32(want))
				}
				expectInteger(t, i, want, 0, raw)
			}
		})
	}
}

type controlCase struct {
	name  string
	f     fixtureFunc
	calls []controlCall
}
type controlCall struct {
	args []uint64
	want uint64
	trap uint32
}

func integerControls() []controlCase {
	f := func(body []byte, params bool) fixtureFunc {
		x := fixtureFunc{result: 0x7f, body: body}
		if params {
			x.params = []byte{0x7f}
		}
		return x
	}
	return []controlCase{
		{"br-if-payload", f([]byte{2, 0x7f, 0x41, 10, 0x41, 20, 0x20, 0, 0x0d, 0, 0x6a, 0x0b, 0x0b}, true), []controlCall{{[]uint64{0xdeadbeef00000000}, 30, 0}, {[]uint64{1}, 20, 0}}},
		{"function-label", f([]byte{0x41, 7, 2, 0x40, 0x41, 42, 0x0c, 1, 0x0b, 0x0b}, false), []controlCall{{nil, 42, 0}}},
		{"early-return", f([]byte{2, 0x7f, 0x41, 42, 0x0f, 0x0b, 0x0b}, false), []controlCall{{nil, 42, 0}}},
		{"if-trap-without-else", f([]byte{0x20, 0, 4, 0x40, 0, 0x0b, 0x41, 9, 0x0b}, true), []controlCall{{[]uint64{1}, 0, TrapUnreachable}, {[]uint64{0}, 9, 0}}},
		{"dead-nested", f([]byte{0, 2, 0x7f, 0x41, 1, 0x0b, 0x1a, 0x41, 9, 0x0b}, false), []controlCall{{nil, 0, TrapUnreachable}}},
		{"loop-result-backedge", f([]byte{0x41, 0xe4, 0, 3, 0x7f, 0x41, 7, 0x20, 0, 0x41, 1, 0x6b, 0x21, 0, 0x20, 0, 0x0d, 0, 0x0b, 0x6a, 0x0b}, true), []controlCall{{[]uint64{3}, 107, 0}, {[]uint64{1}, 107, 0}}},
		{"loop-exit-deep-result", f([]byte{0x41, 0xe4, 0, 2, 0x7f, 3, 0x7f, 0x20, 0, 0x45, 4, 0x40, 0x41, 7, 0x0c, 2, 0x0b, 0x20, 0, 0x41, 1, 0x6b, 0x21, 0, 0x0c, 0, 0x0b, 0x0b, 0x6a, 0x0b}, true), []controlCall{{[]uint64{0}, 107, 0}, {[]uint64{9}, 107, 0}}},
		{"br-table-result", f([]byte{2, 0x7f, 2, 0x7f, 0x41, 10, 0x20, 0, 0x0e, 1, 0, 1, 0x0b, 0x41, 1, 0x6a, 0x0b, 0x0b}, true), []controlCall{{[]uint64{0}, 11, 0}, {[]uint64{1}, 10, 0}, {[]uint64{0xffffffff}, 10, 0}, {[]uint64{0xdeadbeef00000000}, 11, 0}}},
		{"br-table-repeated-last-default", f([]byte{2, 0x7f, 2, 0x7f, 2, 0x7f, 0x41, 10, 0x20, 0, 0x0e, 3, 0, 1, 0, 2, 0x0b, 0x41, 1, 0x6a, 0x0b, 0x41, 2, 0x6a, 0x0b, 0x0b}, true), []controlCall{{[]uint64{0}, 13, 0}, {[]uint64{1}, 12, 0}, {[]uint64{2}, 13, 0}, {[]uint64{3}, 10, 0}, {[]uint64{0xffffffff}, 10, 0}, {[]uint64{0xdeadbeef00000002}, 13, 0}}},
		{"untyped-i32-select", fixtureFunc{params: []byte{0x7f, 0x7f, 0x7f}, result: 0x7f, body: []byte{0x20, 0, 0x20, 1, 0x20, 2, 0x1b, 0x0b}}, []controlCall{{[]uint64{0xdeadbeefffffffff, 22, 0xdeadbeef00000000}, 22, 0}, {[]uint64{0xdeadbeefffffffff, 22, 1}, 0xffffffff, 0}}},
		{"untyped-i64-select", fixtureFunc{params: []byte{0x7e, 0x7e, 0x7f}, result: 0x7e, body: []byte{0x20, 0, 0x20, 1, 0x20, 2, 0x1b, 0x0b}}, []controlCall{{[]uint64{0xdeadbeefffffffff, 22, 0}, 22, 0}, {[]uint64{0xdeadbeefffffffff, 22, 1}, 0xdeadbeefffffffff, 0}}},
		{"typed-select", fixtureFunc{params: []byte{0x7e, 0x7e, 0x7f}, result: 0x7e, body: []byte{0x20, 0, 0x20, 1, 0x20, 2, 0x1c, 1, 0x7e, 0x0b}}, []controlCall{{[]uint64{11, 22, 0xdeadbeef00000000}, 22, 0}, {[]uint64{11, 22, 1}, 11, 0}}},
		{"zero-local-reuse", fixtureFunc{params: []byte{0x7f}, result: 0x7f, locals: []byte{1, 1, 0x7f}, body: []byte{0x20, 1, 0x20, 0, 0x21, 1, 0x0b}}, []controlCall{{[]uint64{0xffffffff}, 0, 0}, {[]uint64{9}, 0, 0}}},
	}
}

func deepIntegerFixture() fixtureFunc {
	f := fixtureFunc{result: 0x7f, locals: append(uleb([]byte{1}, 256), 0x7f)}
	for range 300 {
		f.body = append(f.body, 0x41, 1)
	}
	f.body = append(f.body, 2, 0x7f, 0x41, 20, 0x0c, 0, 0x0b)
	for range 300 {
		f.body = append(f.body, 0x6a)
	}
	f.body = append(f.body, 0x0b)
	return f
}

func TestNativeIntegerDeepFrameAndZeroAlloc(t *testing.T) {
	f := deepIntegerFixture()
	c, err := CompileWithOptions(binaryModule(f), CompileOptions{ForceFallback: true})
	if err != nil {
		t.Fatal(err)
	}
	if c.functions[0].SpillSlots != 301 || c.functions[0].FrameBytes != 4480 {
		t.Fatal(c.functions)
	}
	i := integerInstance(t, f, true)
	expectInteger(t, i, 320, 0)
	alloc := testing.AllocsPerRun(100, func() { expectInteger(t, i, 320, 0) })
	if alloc != 0 {
		t.Fatal("fallback steady-state allocations", alloc)
	}
}
func TestIntegerControlAdmission(t *testing.T) {
	for _, c := range integerControls() {
		t.Run(c.name, func(t *testing.T) {
			if _, err := CompileWithOptions(binaryModule(c.f), CompileOptions{ForceFallback: true}); err != nil {
				t.Fatal(err)
			}
		})
	}
}
func TestNativeIntegerControl(t *testing.T) {
	for _, c := range integerControls() {
		t.Run(c.name, func(t *testing.T) {
			i := integerInstance(t, c.f, true)
			for repeat := 0; repeat < 3; repeat++ {
				for _, call := range c.calls {
					expectInteger(t, i, call.want, call.trap, call.args...)
				}
			}
		})
	}
}

func TestNativeForcedFallbackOverlap(t *testing.T) {
	if runtime.GOARCH != "riscv64" {
		t.Skip("RV64 execution only")
	}
	for _, w := range []bool{false, true} {
		for _, op := range scalarOps {
			for _, branch := range []bool{false, true} {
				if branch && op < shared.IntEq {
					continue
				}
				f := numericFixture(w, opcode(op, w), branch)
				a, err := Compile(binaryModule(f))
				if err != nil {
					t.Fatal(err)
				}
				if a.functions[0].Fallback {
					t.Fatal("overlap must exercise shared", op)
				}
				sharedI, err := a.Instantiate(native.MinNativeStackBytes)
				if err != nil {
					t.Fatal(err)
				}
				fallbackI := integerInstance(t, f, true)
				for _, p := range dirtyInputs() {
					want := reference(op, w, p[0], p[1])
					if branch {
						if want == 1 {
							want = 11
						} else {
							want = 22
						}
					}
					expectInteger(t, sharedI, want, 0, p[0], p[1])
					expectInteger(t, fallbackI, want, 0, p[0], p[1])
				}
				if err := sharedI.Close(); err != nil {
					t.Fatal(err)
				}
				if err := fallbackI.Close(); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
}
