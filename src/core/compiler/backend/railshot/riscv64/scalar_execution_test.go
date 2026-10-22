//go:build linux && !tinygo

package riscv64

import (
	"context"
	"math/rand"
	"runtime"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
	native "github.com/wago-org/wago/src/core/runtime/riscv64"
)

func reference(op shared.IntOp, w bool, a, b uint64) uint64 {
	bits := uint64(63)
	if !w {
		a = uint64(uint32(a))
		b = uint64(uint32(b))
		bits = 31
	}
	sa, sb := int64(a), int64(b)
	if !w {
		sa = int64(int32(a))
		sb = int64(int32(b))
	}
	var v uint64
	switch op {
	case shared.IntAdd:
		v = a + b
	case shared.IntSub:
		v = a - b
	case shared.IntMul:
		v = a * b
	case shared.IntAnd:
		v = a & b
	case shared.IntOr:
		v = a | b
	case shared.IntXor:
		v = a ^ b
	case shared.IntShl:
		v = a << (b & bits)
	case shared.IntShrS:
		v = uint64(sa >> (b & bits))
	case shared.IntShrU:
		v = a >> (b & bits)
	default:
		var pred bool
		switch op {
		case shared.IntEq:
			pred = a == b
		case shared.IntNe:
			pred = a != b
		case shared.IntLtS:
			pred = sa < sb
		case shared.IntLtU:
			pred = a < b
		case shared.IntGtS:
			pred = sa > sb
		case shared.IntGtU:
			pred = a > b
		case shared.IntLeS:
			pred = sa <= sb
		case shared.IntLeU:
			pred = a <= b
		case shared.IntGeS:
			pred = sa >= sb
		case shared.IntGeU:
			pred = a >= b
		default:
			panic("unknown reference")
		}
		if pred {
			v = 1
		}
	}
	if !w {
		v = uint64(uint32(v))
	}
	return v
}

func TestNativeRawReturnHookAndPrologueFence(t *testing.T) {
	if runtime.GOARCH != "riscv64" {
		t.Skip("RV64 execution only")
	}
	e, err := native.NewEngine(native.MinNativeStackBytes)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	call, err := native.NewCallFrame(1)
	if err != nil {
		t.Fatal(err)
	}
	defer call.Close()
	for _, w := range []bool{false, true} {
		f := target{}
		primitivePrefix(&f, 16)
		f.a.Sd(enc.X11, enc.SP, 8)
		f.a.Ld(enc.X10, enc.X10, 0)
		f.Return(10, w, 0)
		primitiveSuffix(&f, 16)
		p := primitiveProgram(t, &f)
		for _, v := range dirtyInputs() {
			want := v[0]
			if !w {
				want = uint64(uint32(want))
			}
			got, err := call.Call(e, p, 0, []uint64{v[0]}, 16)
			if err != nil || got != want {
				t.Fatal("raw Return", w, v[0], got, want, err)
			}
		}
		p.Close()
	}
	c, err := Compile(binaryModule(fixtureFunc{params: []byte{0x7f}, result: 0x7f, body: []byte{0x20, 0, 0x0b}}))
	if err != nil {
		t.Fatal(err)
	}
	frame := c.functions[0].FrameBytes
	for _, mode := range []string{"exact", "too-small", "reversed"} {
		f := target{}
		primitivePrefix(&f, 16)
		const sentinel int64 = 0x1234567812345678
		if mode != "exact" {
			f.a.Constant64(enc.X5, sentinel)
			for off := -frame; off < 0; off += 8 {
				f.a.Sd(enc.X5, enc.SP, int32(off))
			}
		}
		budget := frame
		if mode == "too-small" {
			budget -= 16
		}
		if mode == "reversed" {
			budget = -16
		}
		f.a.Constant64(enc.Scratch0, int64(budget))
		f.a.Sub(enc.X13, enc.SP, enc.Scratch0)
		site := f.a.JumpLong(true)
		if mode != "exact" {
			f.a.Constant64(enc.X6, sentinel)
			for off := -frame; off < 0; off += 8 {
				f.a.Ld(enc.X5, enc.SP, int32(off))
				skip := f.a.BranchLong(enc.EQ, enc.X5, enc.X6)
				f.a.Addi(enc.X5, enc.Zero, 2)
				f.a.Sw(enc.X5, enc.X12, 0)
				if err = f.a.PatchLong(skip, int64(f.Position())); err != nil {
					t.Fatal(err)
				}
			}
		}
		primitiveSuffix(&f, 16)
		if err = f.a.PatchLong(site, int64(f.Position())); err != nil {
			t.Fatal(err)
		}
		f.a.B = append(f.a.B, c.code...)
		p := primitiveProgram(t, &f)
		got, err := call.Call(e, p, 0, []uint64{0xdeadbeefffffffff}, 16+frame)
		if mode == "exact" {
			if err != nil || got != 0xffffffff {
				t.Fatal(mode, got, err)
			}
		} else {
			tr, ok := err.(*native.TrapError)
			if !ok || tr.Code != 1 {
				t.Fatal("pre-write frame sentinels", mode, got, err)
			}
		}
		p.Close()
	}
}

func TestSharedWorkerStorageReleasedAfterReuse(t *testing.T) {
	m, err := wasm.DecodeModule(binaryModule(pressureFixture(), joinFixture()))
	if err != nil {
		t.Fatal(err)
	}
	if err = wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	var worker shared.ScalarState
	for n := 0; n < 8; n++ {
		for idx := range m.Code {
			ft, _ := m.LocalFuncType(idx)
			summary := shared.AdmitScalarFunction(&m.Code[idx], ft)
			code, info, err := compileFunction(&worker, &m.Code[idx], ft, summary, scalarRegisters[:])
			if err != nil || len(code) == 0 || info.FrameBytes < 2048 {
				t.Fatal(info, err)
			}
		}
	}
	retained, discarded := worker.Memory(), worker.Discarded
	if retained == 0 {
		t.Fatal("did not retain reusable worker storage")
	}
	worker.FinishWorker()
	if worker.Memory() != 0 || worker.Discarded != discarded+retained {
		t.Fatal("worker lifetime/accounting")
	}
}

func TestNativeHighArityLargeParameterHomes(t *testing.T) {
	if runtime.GOARCH != "riscv64" {
		t.Skip("RV64 execution only")
	}
	params := make([]byte, 256)
	args := make([]uint64, 256)
	for n := range params {
		params[n] = 0x7f
		args[n] = 0xa5a5a5a580000000 + uint64(n)
	}
	f := fixtureFunc{params: params, result: 0x7f, body: []byte{0x20, 255, 1, 0x41, 1, 0x6a, 0x0b}}
	c, err := Compile(binaryModule(f))
	if err != nil {
		t.Fatal(err)
	}
	if c.functions[0].FrameBytes <= 2047 {
		t.Fatal(c.functions)
	}
	i, err := c.Instantiate(native.MinNativeStackBytes)
	if err != nil {
		t.Fatal(err)
	}
	defer i.Close()
	if got, err := i.Invoke(context.Background(), "a", args...); err != nil || got != uint64(uint32(args[255]+1)) {
		t.Fatal("large parameter homes", got, err)
	}
}

var scalarOps = []shared.IntOp{shared.IntAdd, shared.IntSub, shared.IntMul, shared.IntAnd, shared.IntOr, shared.IntXor, shared.IntShl, shared.IntShrS, shared.IntShrU, shared.IntEq, shared.IntNe, shared.IntLtS, shared.IntLtU, shared.IntGtS, shared.IntGtU, shared.IntLeS, shared.IntLeU, shared.IntGeS, shared.IntGeU}

func dirtyInputs() [][2]uint64 {
	edges := []uint64{0, 1, 0xffffffff, 0x80000000, 0x7fffffff, 1 << 63, ^uint64(0), 0xdeadbeef00000000, 0xa5a5a5a580000001, 31, 32, 63, 64}
	var out [][2]uint64
	for _, a := range edges {
		for _, b := range edges {
			out = append(out, [2]uint64{a, b})
		}
	}
	r := rand.New(rand.NewSource(64))
	for i := 0; i < 128; i++ {
		out = append(out, [2]uint64{r.Uint64(), r.Uint64()})
	}
	return out
}
func primitivePrefix(f *target, frame int) {
	f.a.Constant64(enc.Scratch0, int64(frame))
	f.a.Sub(enc.SP, enc.SP, enc.Scratch0)
	f.a.Sd(enc.RA, enc.SP, 0)
}
func primitiveSuffix(f *target, frame int) {
	f.a.Ld(enc.RA, enc.SP, 0)
	f.a.Constant64(enc.Scratch0, int64(frame))
	f.a.Add(enc.SP, enc.SP, enc.Scratch0)
	f.a.Ret()
}
func primitiveProgram(t *testing.T, f *target) *native.Program {
	t.Helper()
	if f.err != nil || f.a.Err() != nil {
		t.Fatal(f.err, f.a.Err())
	}
	p, err := native.MapCode(f.a.B)
	if err != nil {
		if p != nil {
			p.Close()
		}
		t.Fatal(err)
	}
	return p
}
func predicateCode(f *target, site int) {
	f.a.Addi(enc.X10, enc.Zero, 1)
	end := f.Jump()
	if err := f.Patch(site, f.Position()); err != nil {
		panic(err)
	}
	f.a.Addi(enc.X10, enc.Zero, 0)
	if err := f.Patch(end, f.Position()); err != nil {
		panic(err)
	}
}
func TestNativeRawWidthHooksAndAliases(t *testing.T) {
	if runtime.GOARCH != "riscv64" {
		t.Skip("RV64 execution only")
	}
	e, err := native.NewEngine(native.MinNativeStackBytes)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	inputs := dirtyInputs()
	for _, w := range []bool{false, true} {
		for _, op := range scalarOps {
			for _, dst := range []uint8{10, 11, 5} {
				f := target{}
				primitivePrefix(&f, 16)
				f.Binary(op, w, dst, 10, shared.ScalarOperand{Kind: shared.ScalarRegister, Reg: 11})
				f.Move(10, dst, true)
				primitiveSuffix(&f, 16)
				p := primitiveProgram(t, &f)
				for _, v := range inputs {
					want := reference(op, w, v[0], v[1])
					got, err := e.Call(p, 0, v[0], v[1], 16)
					if err != nil || got != want {
						t.Fatalf("raw op=%d wide=%v dst=%d a=%x b=%x got=%x want=%x err=%v", op, w, dst, v[0], v[1], got, want, err)
					}
				}
				if err = p.Close(); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	for _, w := range []bool{false, true} {
		for _, op := range scalarOps[9:] {
			f := target{}
			primitivePrefix(&f, 16)
			site := f.BranchCompare(op, w, 10, shared.ScalarOperand{Kind: shared.ScalarRegister, Reg: 11})
			predicateCode(&f, site)
			primitiveSuffix(&f, 16)
			p := primitiveProgram(t, &f)
			for _, v := range inputs {
				want := reference(op, w, v[0], v[1])
				got, err := e.Call(p, 0, v[0], v[1], 16)
				if err != nil || got != want {
					t.Fatalf("false edge op=%d wide=%v a=%x b=%x got=%x want=%x err=%v", op, w, v[0], v[1], got, want, err)
				}
			}
			p.Close()
		}
	}
	for _, mode := range []string{"move-alias", "move-distinct", "normalize", "branch-zero", "spill-small", "spill-large"} {
		f := target{}
		frame := 16
		if mode == "spill-small" || mode == "spill-large" {
			frame = 4096
		}
		primitivePrefix(&f, frame)
		switch mode {
		case "move-alias":
			f.Move(10, 10, false)
		case "move-distinct":
			f.Move(5, 10, false)
			f.Move(10, 5, true)
		case "normalize":
			f.NormalizeI32(10)
		case "branch-zero":
			predicateCode(&f, f.BranchZero(10))
		case "spill-small", "spill-large":
			off := int32(16)
			if mode == "spill-large" {
				off = 3000
			}
			f.Store(off, 10, true)
			f.Load(10, off, false)
			f.Store(off, 10, false)
			f.Load(10, off, false)
		}
		primitiveSuffix(&f, frame)
		p := primitiveProgram(t, &f)
		for _, v := range inputs {
			want := uint64(uint32(v[0]))
			if mode == "branch-zero" {
				want = 0
				if uint32(v[0]) != 0 {
					want = 1
				}
			}
			got, err := e.Call(p, 0, v[0], 0, frame)
			if err != nil || got != want {
				t.Fatalf("mode=%s a=%x got=%x want=%x err=%v", mode, v[0], got, want, err)
			}
		}
		p.Close()
	}
}

func opcode(op shared.IntOp, w bool) byte {
	for b := 0; b < 256; b++ {
		o, wide, ok := shared.ScalarOpcode(byte(b))
		if ok && o == op && wide == w {
			return byte(b)
		}
	}
	panic("opcode absent")
}
func TestNativeSharedArithmeticAndComparisons(t *testing.T) {
	if runtime.GOARCH != "riscv64" {
		t.Skip("RV64 execution only")
	}
	inputs := dirtyInputs()
	for _, w := range []bool{false, true} {
		for _, op := range scalarOps {
			for _, branch := range []bool{false, true} {
				if branch && op < shared.IntEq {
					continue
				}
				c, err := Compile(binaryModule(numericFixture(w, opcode(op, w), branch)))
				if err != nil {
					t.Fatal(err)
				}
				i, err := c.Instantiate(native.MinNativeStackBytes)
				if err != nil {
					t.Fatal(err)
				}
				for _, v := range inputs {
					want := reference(op, w, v[0], v[1])
					if branch {
						if want == 1 {
							want = 11
						} else {
							want = 22
						}
					}
					got, err := i.Invoke(context.Background(), "a", v[0], v[1])
					if err != nil || got != want {
						t.Fatalf("shared op=%d wide=%v branch=%v a=%x b=%x got=%x want=%x err=%v", op, w, branch, v[0], v[1], got, want, err)
					}
				}
				if err = i.Close(); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
}

func TestNativeConstantsReturnAndResultReuse(t *testing.T) {
	if runtime.GOARCH != "riscv64" {
		t.Skip("RV64 execution only")
	}
	for _, v := range []int64{0, 1, -1, -1 << 63, 1<<63 - 1, -1 << 31, 1<<31 - 1, 0x123456789abcdef} {
		body := sleb([]byte{0x42}, v)
		body = append(body, 0x0b)
		c, err := Compile(binaryModule(fixtureFunc{result: 0x7e, body: body}, fixtureFunc{params: []byte{0x7f}, result: 0x7f, body: []byte{0x20, 0, 0x0f, 0x0b}}))
		if err != nil {
			t.Fatal(err)
		}
		i, err := c.Instantiate(native.MinNativeStackBytes)
		if err != nil {
			t.Fatal(err)
		}
		for n := 0; n < 16; n++ {
			if got, err := i.Invoke(context.Background(), "a"); err != nil || got != uint64(v) {
				t.Fatal(got, v, err)
			}
			if got, err := i.Invoke(context.Background(), "b", 0xdeadbeefffffffff); err != nil || got != 0xffffffff {
				t.Fatal("dirty direct i32 return/reuse", got, err)
			}
		}
		if a := testing.AllocsPerRun(100, func() {
			if got, err := i.Invoke(context.Background(), "b", 0x1234567880000000); err != nil || got != 0x80000000 {
				panic("scalar invoke")
			}
		}); a != 0 {
			t.Fatalf("steady-state invocation allocations %g", a)
		}
		i.Close()
	}
}

func TestPhysicalReservationsAndRegisterOnlyContract(t *testing.T) {
	f := target{regs: scalarRegisters[:]}
	regs, reserved := f.Registers()
	seen := uint64(0)
	for _, r := range regs {
		if r > 31 || seen&(1<<r) != 0 || reserved&(1<<r) != 0 {
			t.Fatal("invalid physical pool", r)
		}
		seen |= 1 << r
	}
	if reserved != enc.ReservedMask || f.ScaledAdd(false, 5, 6, 7, 1) || f.Operand(shared.IntAdd, false, shared.ScalarOperand{Kind: shared.ScalarConstant}) {
		t.Fatal("unexpected optional lowering")
	}
	if _, incoming := f.ParameterRegister(0); incoming {
		t.Fatal("unexpected parameter transfer")
	}
}
