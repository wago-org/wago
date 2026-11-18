//go:build (linux || darwin || windows) && arm64

package arm64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func nzcvHolds(c Cond, nzcv uint8) bool {
	n, z, cflag, v := nzcv&8 != 0, nzcv&4 != 0, nzcv&2 != 0, nzcv&1 != 0
	switch c {
	case condE:
		return z
	case condNE:
		return !z
	case condB:
		return !cflag
	case condAE:
		return cflag
	case condBE:
		return !cflag || z
	case condA:
		return cflag && !z
	case condL:
		return n != v
	case condGE:
		return n == v
	case condLE:
		return z || n != v
	case condG:
		return !z && n == v
	default:
		return false
	}
}

func TestCompareAndFalseFlags(t *testing.T) {
	for _, op := range []wOp{opEq, opNe, opLtS, opLtU, opGtS, opGtU, opLeS, opLeU, opGeS, opGeU} {
		cc := condOf(op)
		if nzcvHolds(cc, falseNZCV(cc)) {
			t.Fatalf("op %v condition %v holds for false NZCV %#x", op, cc, falseNZCV(cc))
		}
	}
}

func compareAndModule(t *testing.T) *wasm.Module {
	// (a <s b) & (b >=u c) & (c != d)
	body := []byte{0,
		0x20, 0, 0x20, 1, 0x48,
		0x20, 1, 0x20, 2, 0x4f,
		0x71,
		0x20, 2, 0x20, 3, 0x47,
		0x71, 0x0b,
	}
	i32 := []wasm.ValType{wasm.I32}
	return mod1(t, []wasm.ValType{wasm.I32, wasm.I32, wasm.I32, wasm.I32}, i32, body)
}

func TestCompareAndChainExecution(t *testing.T) {
	m := compareAndModule(t)
	inputs := [][4]uint64{
		{1, 2, 1, 0},          // all true
		{2, 1, 0, 1},          // first false
		{1, 2, 3, 0},          // middle false
		{1, 2, 1, 1},          // last false
		{0xffffffff, 0, 0, 1}, // signed first true, last false
	}
	for _, enabled := range []bool{false, true} {
		opts := CompileOptions{Optimizations: map[string]bool{"guarded-test-ccmp": enabled}}
		if diagnosticsEnabled {
			stats := &ModuleStats{}
			opts.Stats = stats
			cm, err := CompileModuleWith(m, opts)
			if err != nil {
				t.Fatal(err)
			}
			if cm.CodeImage != nil {
				cm.CodeImage.Close()
			}
			if got := stats.Funcs[0].Peephole["compare-and-chain"]; (got != 0) != enabled {
				t.Fatalf("enabled=%v compare-and-chain=%d", enabled, got)
			}
			opts.Stats = nil
		}
		for _, in := range inputs {
			want := uint64(0)
			if int32(in[0]) < int32(in[1]) && uint32(in[1]) >= uint32(in[2]) && uint32(in[2]) != uint32(in[3]) {
				want = 1
			}
			got, err := runArm64WrapperWithOptions(t, m, opts, in[:]...)
			if err != nil {
				t.Fatal(err)
			}
			if got != want {
				t.Fatalf("enabled=%v input=%x got=%d want=%d", enabled, in, got, want)
			}
		}
	}
}

func compareAndAllConditionsModule(t *testing.T, i64 bool) *wasm.Module {
	ops := []byte{0x46, 0x47, 0x48, 0x49, 0x4a, 0x4b, 0x4c, 0x4d, 0x4e, 0x4f}
	typ := wasm.I32
	if i64 {
		typ = wasm.I64
		for i := range ops {
			ops[i] += 0x0b
		}
	}
	params := make([]wasm.ValType, 2*len(ops))
	for i := range params {
		params[i] = typ
	}
	body := []byte{0}
	for i, op := range ops {
		body = append(body, 0x20, byte(2*i), 0x20, byte(2*i+1), op)
		if i != 0 {
			body = append(body, 0x71)
		}
	}
	body = append(body, 0x0b)
	return mod1(t, params, []wasm.ValType{wasm.I32}, body)
}

func TestCompareAndChainAllIntegerConditions(t *testing.T) {
	truePairs := [][2]int64{
		{1, 1},  // eq
		{1, 2},  // ne
		{-1, 0}, // lt_s
		{0, 1},  // lt_u
		{0, -1}, // gt_s
		{1, 0},  // gt_u
		{-1, 0}, // le_s
		{0, 1},  // le_u
		{0, -1}, // ge_s
		{1, 0},  // ge_u
	}
	falsePairs := [][2]int64{
		{1, 2},  // eq
		{1, 1},  // ne
		{0, -1}, // lt_s
		{1, 0},  // lt_u
		{-1, 0}, // gt_s
		{0, 1},  // gt_u
		{1, 0},  // le_s
		{1, 0},  // le_u
		{-1, 0}, // ge_s
		{0, 1},  // ge_u
	}
	for _, i64 := range []bool{false, true} {
		m := compareAndAllConditionsModule(t, i64)
		for falseAt := -1; falseAt < len(truePairs); falseAt++ {
			args := make([]uint64, 0, 2*len(truePairs))
			for i, pair := range truePairs {
				if i == falseAt {
					pair = falsePairs[i]
				}
				for _, v := range pair {
					if i64 {
						args = append(args, uint64(v))
					} else {
						args = append(args, uint64(uint32(v)))
					}
				}
			}
			want := uint64(1)
			if falseAt >= 0 {
				want = 0
			}
			got, err := runArm64WrapperWithOptions(t, m, CompileOptions{}, args...)
			if err != nil {
				t.Fatal(err)
			}
			if got != want {
				t.Fatalf("i64=%v falseAt=%d got=%d want=%d", i64, falseAt, got, want)
			}
		}
	}
}

func TestCompareAndChainConstants(t *testing.T) {
	// (a != 0) & (b <u 31) & (c >u 63) & (d >s -1). This exercises the
	// ordinary first comparison, CCMP's five-bit immediate, and the fallback
	// constant-register form in one chain.
	body := []byte{0,
		0x20, 0, 0x41, 0, 0x47,
		0x20, 1, 0x41, 31, 0x49, 0x71,
		0x20, 2, 0x41, 63, 0x4b, 0x71,
		0x20, 3, 0x41, 0x7f, 0x4a, 0x71,
		0x0b,
	}
	m := mod1(t, []wasm.ValType{wasm.I32, wasm.I32, wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, body)
	for _, tc := range []struct {
		args [4]uint64
		want uint64
	}{
		{args: [4]uint64{1, 30, 64, 0}, want: 1},
		{args: [4]uint64{0, 30, 64, 0}},
		{args: [4]uint64{1, 31, 64, 0}},
		{args: [4]uint64{1, 30, 63, 0}},
		{args: [4]uint64{1, 30, 64, 0xffffffff}},
	} {
		got, err := runArm64WrapperWithOptions(t, m, CompileOptions{}, tc.args[:]...)
		if err != nil {
			t.Fatal(err)
		}
		if got != tc.want {
			t.Fatalf("input=%x got=%d want=%d", tc.args, got, tc.want)
		}
	}
}
