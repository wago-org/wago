//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package riscv64

import (
	"bytes"
	"encoding/binary"
	"errors"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"math"
	"reflect"
	"testing"
)

func floatBits(x float64, wide bool) uint64 {
	if wide {
		return math.Float64bits(x)
	}
	return uint64(math.Float32bits(float32(x)))
}
func floatValue(bits uint64, wide bool) float64 {
	if wide {
		return math.Float64frombits(bits)
	}
	return float64(math.Float32frombits(uint32(bits)))
}
func floatInputs(wide bool) []uint64 {
	vals := []float64{0, math.Copysign(0, -1), 1, -1, 1.5, -1.5, 2.5, -2.5, math.Inf(1), math.Inf(-1), math.NaN(), math.Ldexp(1, 24), math.Ldexp(1, 53), math.Ldexp(1, 63), math.Ldexp(1, 64)}
	bits := make([]uint64, 0, len(vals)+8)
	for _, x := range vals {
		bits = append(bits, floatBits(x, wide))
	}
	if wide {
		bits = append(bits, 1, 0x8000000000000001, 0x7fefffffffffffff, 0xffefffffffffffff, 0x7ff0000000000001, 0xfff80000deadbeef, 0x3fefffffffffffff, 0xbfefffffffffffff)
	} else {
		bits = append(bits, 1, 0x80000001, 0x7f7fffff, 0xff7fffff, 0x7f800001, 0xffc0dead, 0x3f7fffff, 0xbf7fffff)
	}
	return bits
}
func floatMathExpected(n byte, left, right uint64, wide bool) uint64 {
	sign := uint64(1) << 31
	nan := uint64(0x7fc00000)
	if wide {
		sign = uint64(1) << 63
		nan = 0x7ff8000000000000
	}
	switch n {
	case 0:
		return left &^ sign
	case 1:
		return left ^ sign
	case 13:
		return left&^sign | right&sign
	}
	x, y := floatValue(left, wide), floatValue(right, wide)
	var z float64
	switch n {
	case 2:
		z = math.Ceil(x)
	case 3:
		z = math.Floor(x)
	case 4:
		z = math.Trunc(x)
	case 5:
		z = math.RoundToEven(x)
	case 6:
		z = math.Sqrt(x)
	case 7:
		if wide {
			z = x + y
		} else {
			z = float64(float32(x) + float32(y))
		}
	case 8:
		if wide {
			z = x - y
		} else {
			z = float64(float32(x) - float32(y))
		}
	case 9:
		if wide {
			z = x * y
		} else {
			z = float64(float32(x) * float32(y))
		}
	case 10:
		if wide {
			z = x / y
		} else {
			z = float64(float32(x) / float32(y))
		}
	case 11:
		if math.IsNaN(x) || math.IsNaN(y) {
			return nan
		}
		z = math.Min(x, y)
	case 12:
		if math.IsNaN(x) || math.IsNaN(y) {
			return nan
		}
		z = math.Max(x, y)
	}
	if math.IsNaN(z) {
		return nan
	}
	return floatBits(z, wide)
}

func TestProductionFloatArithmeticAllScalarOperations(t *testing.T) {
	for _, wide := range []bool{false, true} {
		base := byte(0x8b)
		if wide {
			base = 0x99
		}
		var fs []fixtureFunc
		for n := byte(0); n < 14; n++ {
			fs = append(fs, floatFixture(wide, base+n, n <= 6, false))
		}
		f := productionCallsWithOptions(t, binaryModule(fs...), ProductionOptions{Interruptible: true})
		vals := floatInputs(wide)
		for n := byte(0); n < 14; n++ {
			for _, left := range vals {
				for _, right := range vals {
					args := []uint64{left, right}
					if !wide {
						args[0] = dirty(uint32(left))
						args[1] = dirty(uint32(right))
					}
					var out [1]uint64
					want := floatMathExpected(n, left, right, wide)
					if err := f.invoke(int(n), args, out[:]); err != nil || out[0] != want {
						t.Fatalf("wide=%t op=%#x args=%x,%x got=%x want=%x err=%v", wide, base+n, left, right, out[0], want, err)
					}
				}
			}
		}
	}
}

func TestProductionFloatComparisonsAndDirtyCarriers(t *testing.T) {
	for _, wide := range []bool{false, true} {
		base := byte(0x5b)
		if wide {
			base = 0x61
		}
		var fs []fixtureFunc
		for n := byte(0); n < 6; n++ {
			fs = append(fs, floatFixture(wide, base+n, false, true))
		}
		f := productionCalls(t, binaryModule(fs...), false)
		vals := floatInputs(wide)
		for n := byte(0); n < 6; n++ {
			for _, left := range vals {
				for _, right := range vals {
					x, y := floatValue(left, wide), floatValue(right, wide)
					truth := false
					switch n {
					case 0:
						truth = x == y
					case 1:
						truth = x != y
					case 2:
						truth = x < y
					case 3:
						truth = x > y
					case 4:
						truth = x <= y
					case 5:
						truth = x >= y
					}
					want := uint64(0)
					if truth {
						want = 1
					}
					args := []uint64{left, right}
					if !wide {
						args[0] = dirty(uint32(left))
						args[1] = dirty(uint32(right))
					}
					var out [1]uint64
					if err := f.invoke(int(n), args, out[:]); err != nil || out[0] != want {
						t.Fatalf("wide=%t compare=%#x args=%x,%x got=%x want=%x err=%v", wide, base+n, left, right, out[0], want, err)
					}
				}
			}
		}
	}
}

func truncExpected(x float64, resultWide, unsigned, saturating bool) (uint64, bool) {
	if math.IsNaN(x) {
		return 0, !saturating
	}
	x = math.Trunc(x)
	bits := 32
	if resultWide {
		bits = 64
	}
	lower, upper := -math.Ldexp(1, bits-1), math.Ldexp(1, bits-1)
	maxResult := uint64(1)<<uint(bits-1) - 1
	minResult := uint64(1) << uint(bits-1)
	if unsigned {
		lower = 0
		upper = math.Ldexp(1, bits)
		maxResult = ^uint64(0)
		if !resultWide {
			maxResult = math.MaxUint32
		}
		minResult = 0
	}
	if x < lower {
		return minResult, !saturating
	}
	if x >= upper {
		return maxResult, !saturating
	}
	var value uint64
	if unsigned {
		value = uint64(x)
	} else {
		value = uint64(int64(x))
	}
	if !resultWide {
		value = uint64(uint32(value))
	}
	return value, false
}

func TestProductionFloatTrappingAndSaturatingConversions(t *testing.T) {
	var fs []fixtureFunc
	for sub := byte(0); sub < 8; sub++ {
		in, result := byte(0x7d), byte(0x7f)
		if sub&2 != 0 {
			in = 0x7c
		}
		if sub&4 != 0 {
			result = 0x7e
		}
		op := byte(0xa8) + sub
		if sub >= 4 {
			op = 0xae + sub - 4
		}
		fs = append(fs, fixtureFunc{params: []byte{in}, result: result, body: []byte{0x20, 0, op, 0x0b}})
	}
	for sub := byte(0); sub < 8; sub++ {
		fn := fs[sub]
		fn.body = []byte{0x20, 0, 0xfc, sub, 0x0b}
		fs = append(fs, fn)
	}
	fs = append(fs, fixtureFunc{result: 0x7d, body: []byte{0x43, 0, 0, 0x80, 0x3f, 0x0b}})
	f := productionCallsWithOptions(t, binaryModule(fs...), ProductionOptions{Interruptible: true})
	for sub := byte(0); sub < 8; sub++ {
		wide, resultWide, unsigned := sub&2 != 0, sub&4 != 0, sub&1 != 0
		vals := floatInputs(wide)
		for _, edge := range []float64{math.Ldexp(1, 31), math.Ldexp(1, 32), math.Ldexp(1, 63), math.Ldexp(1, 64), -math.Ldexp(1, 31), -math.Ldexp(1, 63)} {
			for _, value := range []float64{edge, math.Nextafter(edge, math.Inf(-1)), math.Nextafter(edge, math.Inf(1))} {
				vals = append(vals, floatBits(value, wide))
			}
		}
		for _, value := range []float64{-0.999, -1.001, -2147483648.75, 2147483647.75, 4294967295.75, -2147483649, 4294967296} {
			vals = append(vals, floatBits(value, wide))
		}
		for _, saturating := range []bool{false, true} {
			for _, bits := range vals {
				want, traps := truncExpected(floatValue(bits, wide), resultWide, unsigned, saturating)
				arg := bits
				if !wide {
					arg = dirty(uint32(bits))
				}
				got := []uint64{0xfeedbeef}
				index := int(sub)
				if saturating {
					index += 8
				}
				err := f.invoke(index, []uint64{arg}, got)
				if traps {
					var trap *coreruntime.TrapError
					if !errors.As(err, &trap) || trap.Code != coreruntime.TrapTruncOverflow || got[0] != 0xfeedbeef || len(trap.Frames) != 1 || trap.Frames[0].FunctionIndex != uint32(index) || trap.Frames[0].ProgramCounter != 3 {
						t.Fatalf("conversion trap sub=%d bits=%x got=%x err=%v", sub, bits, got, err)
					}
					if err := f.invoke(16, nil, got); err != nil || got[0] != 0x3f800000 {
						t.Fatal("FP conversion trap reuse", got, err)
					}
				} else if err != nil || got[0] != want {
					t.Fatalf("sat=%t sub=%d bits=%x got=%x want=%x err=%v", saturating, sub, bits, got[0], want, err)
				}
			}
		}
	}
}

func TestProductionFloatIntegerPrecisionAndRawConversions(t *testing.T) {
	var fs []fixtureFunc
	for op := byte(0xb2); op <= 0xbf; op++ {
		wide, _, resultWide, _ := floatOpcode(op)
		in, out := byte(0x7f), byte(0x7d)
		if wide {
			in = 0x7e
		}
		if resultWide {
			out = 0x7c
		}
		switch op {
		case 0xb6:
			in = 0x7c
		case 0xbb:
			in = 0x7d
		case 0xbc:
			in = 0x7d
			out = 0x7f
		case 0xbd:
			in = 0x7c
			out = 0x7e
		}
		fs = append(fs, fixtureFunc{params: []byte{in}, result: out, body: []byte{0x20, 0, op, 0x0b}})
	}
	f := productionCalls(t, binaryModule(fs...), false)
	ints := []uint64{0, 1, 0xffffffff, 0x80000000, 0x7fffffff, 0x100000001, 0x1000001, 0x20000000000001, 0x7fffffffffffffff, 0x8000000000000000, ^uint64(0)}
	for op := byte(0xb2); op <= 0xbf; op++ {
		inputWide, _, outputWide, _ := floatOpcode(op)
		vals := ints
		if op == 0xb6 || op == 0xbb || op >= 0xbc {
			vals = append(append([]uint64{}, ints...), floatInputs(inputWide)...)
		}
		for _, raw := range vals {
			arg := raw
			if !inputWide {
				arg = dirty(uint32(raw))
			}
			value := raw
			if !inputWide {
				value = uint64(uint32(raw))
			}
			var want uint64
			switch op {
			case 0xb2:
				want = uint64(math.Float32bits(float32(int32(value))))
			case 0xb3:
				want = uint64(math.Float32bits(float32(uint32(value))))
			case 0xb4:
				want = uint64(math.Float32bits(float32(int64(value))))
			case 0xb5:
				want = uint64(math.Float32bits(float32(value)))
			case 0xb6:
				want = floatBits(floatValue(value, true), false)
			case 0xb7:
				want = math.Float64bits(float64(int32(value)))
			case 0xb8:
				want = math.Float64bits(float64(uint32(value)))
			case 0xb9:
				want = math.Float64bits(float64(int64(value)))
			case 0xba:
				want = math.Float64bits(float64(value))
			case 0xbb:
				want = floatBits(floatValue(value, false), true)
			default:
				want = value
			}
			if (op == 0xb6 || op == 0xbb) && math.IsNaN(floatValue(value, inputWide)) {
				want = 0x7fc00000
				if outputWide {
					want = 0x7ff8000000000000
				}
			}
			var got [1]uint64
			if err := f.invoke(int(op-0xb2), []uint64{arg}, got[:]); err != nil || got[0] != want {
				t.Fatalf("conversion op=%#x raw=%x got=%x want=%x err=%v", op, raw, got[0], want, err)
			}
		}
	}
}

func TestProductionFloatMixed256CallsAndLiveValues(t *testing.T) {
	types := make([]byte, 256)
	args, want := make([]uint64, 256), make([]uint64, 256)
	identity := fixtureFunc{params: types, results: types}
	caller := fixtureFunc{params: types, results: types}
	for i := range types {
		types[i] = []byte{0x7d, 0x7c, 0x7f, 0x7e}[i%4]
		args[i] = []uint64{dirty(0x7f800001 + uint32(i)), 0xfff80000deadbeef + uint64(i), dirty(uint32(i)), 0x123456789abcdef0 + uint64(i)}[i%4]
		want[i] = args[i]
		if i%4 == 0 || i%4 == 2 {
			want[i] = uint64(uint32(args[i]))
		}
		identity.body = uleb(append(identity.body, 0x20), uint32(i))
		caller.body = uleb(append(caller.body, 0x20), uint32(i))
	}
	identity.body = append(identity.body, 0x0b)
	caller.body = append(caller.body, 0x10, 1, 0x0b)
	f := productionCallsWithOptions(t, binaryModule(caller, identity), ProductionOptions{Interruptible: true})
	got := make([]uint64, 256)
	for range 8 {
		if err := f.invoke(0, args, got); err != nil || !reflect.DeepEqual(got, want) {
			t.Fatal("mixed256 call/return carrier", got, want, err)
		}
	}
	if allocations := testing.AllocsPerRun(100, func() {
		if err := f.invoke(0, args, got); err != nil {
			panic(err)
		}
	}); allocations != 0 {
		t.Fatal("mixed FP warm allocations", allocations)
	}
}

func TestProductionFloatRawTransportControlAndNestedCalls(t *testing.T) {
	pair := []byte{0x7d, 0x7c}
	funcs := []fixtureFunc{
		{params: pair, results: []byte{0x7d, 0x7c, 0x7c}, body: []byte{0x20, 0, 0x20, 1, 0x20, 1, 0x20, 1, 0x10, 1, 0x0b}},
		floatFixture(true, 0xa0, false, false),
		{params: []byte{0x7d, 0x7c, 0x7f}, results: pair, body: []byte{0x20, 0, 0x20, 1, 0x20, 2, 4, 3, 0x01, 5, 0x01, 0x0b, 0x0b}},
		{params: pair, results: pair, body: []byte{0x20, 0, 0x20, 1, 3, 3, 0x0b, 0x0b}},
		{params: []byte{0x7d, 0x7d, 0x7f}, result: 0x7d, body: []byte{0x20, 0, 0x20, 1, 0x20, 2, 0x1b, 0x0b}},
		{params: []byte{0x7c, 0x7c, 0x7f}, result: 0x7c, body: []byte{0x20, 0, 0x20, 1, 0x20, 2, 0x1c, 1, 0x7c, 0x0b}},
		{params: []byte{0x7d}, result: 0x7d, body: []byte{2, 0x7d, 0x20, 0, 0x0c, 0, 0x0b, 0x0b}},
		{params: []byte{0x7c}, result: 0x7c, body: []byte{2, 0x7c, 0x20, 0, 0x0b, 0x0b}},
	}
	f := productionCallsWithOptions(t, binaryModule(funcs...), ProductionOptions{Interruptible: true, DeferCodeMapping: true})
	for _, narrow := range []uint32{0x80000000, 0x7f800001, 0xffc0dead} {
		for _, wide := range []uint64{0x8000000000000000, 0x7ff0000000000001, math.Float64bits(1.5)} {
			input := []uint64{dirty(narrow), wide}
			got := make([]uint64, 3)
			if err := f.invoke(0, input, got); err != nil || got[0] != uint64(narrow) || got[1] != wide || got[2] != floatMathExpected(7, wide, wide, true) {
				t.Fatal("raw FP value below FP call", got, input, err)
			}
			for _, index := range []int{2, 3} {
				for _, cond := range []uint32{0, 1, 0x80000000} {
					got := make([]uint64, 2)
					args := input
					if index == 2 {
						args = append(append([]uint64{}, input...), dirty(cond))
					}
					if err := f.invoke(index, args, got); err != nil || !reflect.DeepEqual(got, []uint64{uint64(narrow), wide}) {
						t.Fatal("FP indexed join/loop params", index, got, err)
					}
				}
			}
			for _, cond := range []uint32{0, 1} {
				var out [1]uint64
				wantNarrow := uint64(0x80000000)
				if cond != 0 {
					wantNarrow = uint64(narrow)
				}
				if err := f.invoke(4, []uint64{dirty(narrow), dirty(0x80000000), dirty(cond)}, out[:]); err != nil || out[0] != wantNarrow {
					t.Fatal("raw F32 select", out, err)
				}
				wantWide := uint64(0x8000000000000000)
				if cond != 0 {
					wantWide = wide
				}
				if err := f.invoke(5, []uint64{wide, 0x8000000000000000, dirty(cond)}, out[:]); err != nil || out[0] != wantWide {
					t.Fatal("raw F64 typed select", out, err)
				}
			}
			var out [1]uint64
			if err := f.invoke(6, input[:1], out[:]); err != nil || out[0] != uint64(narrow) {
				t.Fatal("F32 inline block result", out, err)
			}
			if err := f.invoke(7, input[1:], out[:]); err != nil || out[0] != wide {
				t.Fatal("F64 inline block result", out, err)
			}
		}
	}
}

func TestProductionFloatGlobalsAndUnalignedMemoryRawBits(t *testing.T) {
	fs := []fixtureFunc{
		{results: []byte{0x7d, 0x7c}, body: []byte{0x23, 0, 0x23, 1, 0x0b}},
		{params: []byte{0x7d, 0x7c}, results: []byte{0x7d, 0x7c}, body: []byte{0x20, 0, 0x24, 0, 0x20, 1, 0x24, 1, 0x23, 0, 0x23, 1, 0x0b}},
	}
	for _, wide := range []bool{false, true} {
		typ, store, load := byte(0x7d), byte(0x38), byte(0x2a)
		if wide {
			typ, store, load = 0x7c, 0x39, 0x2b
		}
		fs = append(fs, fixtureFunc{params: []byte{0x7f, typ}, result: typ, body: []byte{0x20, 0, 0x20, 1, store, 0, 0, 0x20, 0, load, 0, 0, 0x0b}})
	}
	data := stateModule(fs, false, 1, 1, true, nil, nil)
	globals := []byte{2, 0x7d, 1, 0x43}
	globals = binary.LittleEndian.AppendUint32(globals, 0x80000000)
	globals = append(globals, 0x0b, 0x7c, 1, 0x44)
	globals = binary.LittleEndian.AppendUint64(globals, 0x7ff0000000000001)
	globals = append(globals, 0x0b)
	data = modifySection(data, 7, 6, globals, false)
	f := productionCallsWithOptions(t, data, ProductionOptions{Interruptible: true})
	var pair [2]uint64
	if err := f.invoke(0, nil, pair[:]); err != nil || pair != [2]uint64{0x80000000, 0x7ff0000000000001} {
		t.Fatal("FP literal global", pair, err)
	}
	for _, narrow := range []uint32{0x80000000, 0x7f800001, 0xffc0dead} {
		for _, wide := range []uint64{0x8000000000000000, 0x7ff0000000000001, 0xfff80000deadbeef} {
			if err := f.invoke(1, []uint64{dirty(narrow), wide}, pair[:]); err != nil || pair != [2]uint64{uint64(narrow), wide} {
				t.Fatal("FP global normalization/raw bits", pair, err)
			}
			for _, cell := range f.globalCells {
				if binary.LittleEndian.Uint64(cell[8:]) != 0xfeedcabed00dcafe {
					t.Fatal("FP global crossed cell")
				}
			}
			for i, value := range []uint64{dirty(narrow), wide} {
				size := 4
				if i == 1 {
					size = 8
				}
				for _, address := range []uint32{1, 9, 65536 - uint32(size)} {
					memory := f.memory.HostBytes()
					for k := range memory {
						memory[k] = 0x55
					}
					var out [1]uint64
					want := value
					if i == 0 {
						want = uint64(narrow)
					}
					if err := f.invoke(2+i, []uint64{dirty(address), value}, out[:]); err != nil || out[0] != want {
						t.Fatal("unaligned raw FP access", out, err)
					}
					if address > 0 && memory[address-1] != 0x55 || uint64(address)+uint64(size) < uint64(len(memory)) && memory[int(address)+size] != 0x55 {
						t.Fatal("FP access crossed width")
					}
				}
				before := append([]byte{}, f.memory.HostBytes()...)
				result := []uint64{0x5555}
				err := f.invoke(2+i, []uint64{dirty(65537 - uint32(size)), value}, result)
				productionBoundsTrap(t, err)
				if !bytes.Equal(before, f.memory.HostBytes()) || result[0] != 0x5555 {
					t.Fatal("FP OOB store modified memory/result")
				}
			}
		}
	}
}

func TestProductionFloatIndexedLoopAndStaleFlags(t *testing.T) {
	body := []byte{0x20, 1, 0x20, 2, 3, 1, 0x21, 2, 0x21, 1, 0x20, 0, 0x45, 4, 0x40, 0x20, 1, 0x20, 2, 0x0f, 0x0b, 0x20, 0, 0x41, 1, 0x6b, 0x21, 0, 0x20, 1, 0x20, 2, 0x44}
	body = binary.LittleEndian.AppendUint64(body, math.Float64bits(1.5))
	body = append(body, 0xa0, 0x0c, 0, 0x0b, 0x00, 0x0b)
	f := productionCallsWithOptions(t, binaryModule(
		fixtureFunc{params: []byte{0x7f, 0x7d, 0x7c}, results: []byte{0x7d, 0x7c}, body: body},
		fixtureFunc{params: []byte{0x7d, 0x7c}, results: []byte{0x7d, 0x7c}, unexported: true, body: []byte{0x20, 0, 0x20, 1, 0x0b}},
	), ProductionOptions{Interruptible: true})
	var got [2]uint64
	for _, count := range []uint32{0, 1, 7, 20} {
		if err := f.invoke(0, []uint64{dirty(count), dirty(0x7f800001), math.Float64bits(2)}, got[:]); err != nil || got != [2]uint64{0x7f800001, math.Float64bits(2 + float64(count)*1.5)} {
			t.Fatal("indexed FP loop backedge", count, got, err)
		}
	}
	// Prior invalid FP arithmetic must not poison an otherwise valid truncation.
	var funcs []fixtureFunc
	for _, wide := range []bool{false, true} {
		typ, constant, div, truncate := byte(0x7d), byte(0x43), byte(0x95), byte(0xa8)
		zeros := 4
		if wide {
			typ, constant, div, truncate = 0x7c, 0x44, 0xa3, 0xaa
			zeros = 8
		}
		code := append([]byte{constant}, make([]byte, zeros)...)
		code = append(code, constant)
		code = append(code, make([]byte, zeros)...)
		code = append(code, div, 0x1a, 0x20, 0, truncate, 0x0b)
		funcs = append(funcs, fixtureFunc{params: []byte{typ}, result: 0x7f, body: code})
	}
	f = productionCalls(t, binaryModule(funcs...), false)
	for i, wide := range []bool{false, true} {
		arg := floatBits(-17.75, wide)
		if !wide {
			arg = dirty(uint32(arg))
		}
		var result [1]uint64
		if err := f.invoke(i, []uint64{arg}, result[:]); err != nil || result[0] != uint64(uint32(math.MaxUint32-16)) {
			t.Fatal("stale NV poisoned truncation", result, err)
		}
	}
}
