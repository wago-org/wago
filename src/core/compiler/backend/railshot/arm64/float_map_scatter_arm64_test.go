//go:build (linux || darwin) && arm64

package arm64

import (
	"bytes"
	"encoding/binary"
	"errors"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	rt "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"math"
	"testing"
)

func scatterFixture(t testing.TB, dst, src, coefficient, start, stride, count uint32) *wasm.Module {
	return scatterFixtureTyped(t, dst, src, coefficient, start, stride, count, true)
}
func scatterFixtureTyped(t testing.TB, dst, src, coefficient, start, stride, count uint32, wide bool) *wasm.Module {
	width, shift, typ, convert, add, mul, sub, load, store := uint32(8), uint32(3), byte(0x7c), byte(0xb8), byte(0xa0), byte(0xa2), byte(0xa1), byte(0x2b), byte(0x39)
	if !wide {
		width, shift, typ, convert, add, mul, sub, load, store = 4, 2, 0x7d, 0xb3, 0x92, 0x94, 0x93, 0x2a, 0x38
	}
	b := []byte{2, 5, 0x7f, 1, typ}
	c := func(v uint32) { b = append(b, 0x41); b = append(b, wasmtest.SLEB32(int32(v))...) }
	get := func(i byte) { b = append(b, 0x20, i) }
	fp := func(v float64) {
		if wide {
			b = append(b, 0x44)
			b = binary.LittleEndian.AppendUint64(b, math.Float64bits(v))
		} else {
			b = append(b, 0x43)
			b = binary.LittleEndian.AppendUint32(b, math.Float32bits(float32(v)))
		}
	}
	b = append(b, 0x03, 0x40)
	get(0)
	get(0)
	c(shift)
	b = append(b, 0x76, convert)
	fp(0.0625)
	b = append(b, mul)
	fp(-64)
	b = append(b, add, store, byte(shift), 0)
	get(0)
	c(width)
	b = append(b, 0x6a, 0x22, 0)
	c(65536)
	b = append(b, 0x47, 0x0d, 0, 0x0b)
	c(coefficient)
	fp(0.25)
	b = append(b, store, byte(shift), 0)
	for i, v := range []uint32{start, dst, src, coefficient, 0} {
		c(v)
		b = append(b, 0x21, byte(i))
	}
	c(64000)
	c(123)
	b = append(b, 0x03, 0x40)
	for lane := uint32(0); lane < 2; lane++ {
		get(0)
		get(1)
		b = append(b, 0x6a)
		if lane != 0 {
			c(stride)
			b = append(b, 0x6a)
		}
		b = append(b, 0x22, 4)
		get(4)
		b = append(b, load, byte(shift), 0)
		get(0)
		get(2)
		b = append(b, 0x6a)
		if lane != 0 {
			c(stride)
			b = append(b, 0x6a)
		}
		b = append(b, load, byte(shift), 0)
		get(3)
		b = append(b, load, byte(shift), 0, mul, sub, 0x22, 5, store, byte(shift), 0)
	}
	get(0)
	c(stride * 2)
	b = append(b, 0x6a, 0x22, 0)
	c(start + stride*count)
	b = append(b, 0x47, 0x0d, 0, 0x0b, 0x36, 2, 0)
	c(64008)
	get(0)
	b = append(b, 0x36, 2, 0)
	c(64012)
	get(4)
	b = append(b, 0x36, 2, 0)
	c(64016)
	get(5)
	b = append(b, store, byte(shift), 0)
	c(0)
	b = append(b, 0x0b)
	return modMem(t, 1, nil, []wasm.ValType{wasm.I32}, b)
}

func scatterOracle(dst, src, coefficient, start, stride, count uint32) ([]byte, bool) {
	return scatterOracleTyped(dst, src, coefficient, start, stride, count, true)
}
func scatterOracleTyped(dst, src, coefficient, start, stride, count uint32, wide bool) ([]byte, bool) {
	mem := make([]byte, 65536)
	width := uint32(8)
	if !wide {
		width = 4
	}
	put := func(at uint32, v float64) {
		if wide {
			binary.LittleEndian.PutUint64(mem[at:], math.Float64bits(v))
		} else {
			binary.LittleEndian.PutUint32(mem[at:], math.Float32bits(float32(v)))
		}
	}
	get := func(at uint32) float64 {
		if wide {
			return math.Float64frombits(binary.LittleEndian.Uint64(mem[at:]))
		}
		return float64(math.Float32frombits(binary.LittleEndian.Uint32(mem[at:])))
	}
	for i := uint32(0); i < uint32(len(mem)); i += width {
		put(i, float64(i/width)*0.0625-64)
	}
	if uint64(coefficient)+uint64(width) > 65536 {
		return mem, true
	}
	put(coefficient, 0.25)
	index := start
	var last uint32
	var value float64
	for iteration := 0; iteration < 10000; iteration++ {
		for lane := uint32(0); lane < 2; lane++ {
			d, s := dst+index+lane*stride, src+index+lane*stride
			if uint64(d)+uint64(width) > 65536 || uint64(s)+uint64(width) > 65536 {
				return mem, true
			}
			old := get(d)
			input := get(s)
			factor := get(coefficient)
			product := float64(input * factor)
			value = old - product
			if !wide {
				product = float64(float32(product))
				value = float64(float32(old - product))
			}
			last = d
			put(d, value)
		}
		index += stride * 2
		if index == start+stride*count {
			binary.LittleEndian.PutUint32(mem[64000:], 123)
			binary.LittleEndian.PutUint32(mem[64008:], index)
			binary.LittleEndian.PutUint32(mem[64012:], last)
			put(64016, value)
			return mem, false
		}
	}
	panic("fixture did not finish or trap")
}

func TestFloatMapScatterMemoryTrapsAndExits(t *testing.T) {
	old := floatMapScatterEnabled
	defer func() { floatMapScatterEnabled = old }()
	for fixture := 0; fixture < 192; fixture++ {
		stride := []uint32{16, 24, 40, 64, 640}[fixture%5]
		count := []uint32{2, 4, 6, 10, 16, 18, 24, 30, 34}[fixture%9]
		src := []uint32{0, 8, 4096, 8192, 0xfffffff8}[(fixture/5)%5]
		dst := []uint32{0, 1, 8, 24, src, src + 4, src + 8, src + stride, src + stride - 4, 65520, 0xffffff80}[fixture%11]
		start := []uint32{0, 0, 0, 8, 24, 128}[fixture%6]
		coefficient := uint32(60000)
		if fixture%7 == 0 {
			coefficient = dst + start + uint32(fixture%4)
		}
		if fixture%13 == 0 {
			coefficient = 65532
		}
		m := scatterFixture(t, dst, src, coefficient, start, stride, count)
		want, trap := scatterOracle(dst, src, coefficient, start, stride, count)
		for _, on := range []bool{false, true} {
			floatMapScatterEnabled = on
			for _, compact := range []bool{false, true} {
				got, err := runScalarUnrollMemory(t, m, CompileOptions{CompactNative: compact})
				var tr *rt.TrapError
				if (err != nil) != trap || err != nil && (!errors.As(err, &tr) || tr.Code != rt.TrapLinMemOutOfBounds) || !bytes.Equal(got, want) {
					t.Fatalf("case%d d%x s%x c%x start%x stride%d count%d on%v compact%v trap%v err%v equal%v", fixture, dst, src, coefficient, start, stride, count, on, compact, trap, err, bytes.Equal(got, want))
				}
			}
		}
	}
}

func TestFloatMapScatterAdmission(t *testing.T) {
	requireCompilerDiagnostics(t)
	for _, stride := range []uint32{16, 24, 40, 64, 640} {
		stats := compileWithStats(t, scatterFixture(t, 8, 0, 60000, 0, stride, 34), false).Funcs[0]
		if stats.Peephole["float-map-scatter-vector"] != 1 {
			t.Fatalf("stride%d rejected: %v", stride, stats.Peephole)
		}
	}
}

func TestFloatMapScatterF32MemoryAndExits(t *testing.T) {
	old := floatMapScatterEnabled
	defer func() { floatMapScatterEnabled = old }()
	for fixture := 0; fixture < 96; fixture++ {
		stride := []uint32{8, 12, 20, 32, 320}[fixture%5]
		count := []uint32{2, 4, 6, 10, 16, 18, 24, 30, 34}[fixture%9]
		src := []uint32{0, 4, 4096, 8192, 0xfffffffc}[(fixture/5)%5]
		dst := []uint32{0, 1, 4, 12, src, src + 2, src + 4, src + stride, src + stride - 2, 65528, 0xffffff80}[fixture%11]
		start := []uint32{0, 0, 0, 4, 12, 128}[fixture%6]
		coefficient := uint32(60000)
		if fixture%7 == 0 {
			coefficient = dst + start + uint32(fixture%4)
		}
		m := scatterFixtureTyped(t, dst, src, coefficient, start, stride, count, false)
		want, trap := scatterOracleTyped(dst, src, coefficient, start, stride, count, false)
		for _, on := range []bool{false, true} {
			floatMapScatterEnabled = on
			for _, compact := range []bool{false, true} {
				got, err := runScalarUnrollMemory(t, m, CompileOptions{CompactNative: compact})
				var tr *rt.TrapError
				if (err != nil) != trap || err != nil && (!errors.As(err, &tr) || tr.Code != rt.TrapLinMemOutOfBounds) || !bytes.Equal(got, want) {
					t.Fatalf("f32 case%d d%x s%x stride%d count%d on%v compact%v trap%v err%v equal%v", fixture, dst, src, stride, count, on, compact, trap, err, bytes.Equal(got, want))
				}
			}
		}
	}
}

func TestFloatMapScatterF32Admission(t *testing.T) {
	requireCompilerDiagnostics(t)
	for _, stride := range []uint32{8, 12, 20, 32, 320} {
		stats := compileWithStats(t, scatterFixtureTyped(t, 4, 0, 60000, 0, stride, 34, false), false).Funcs[0]
		if stats.Peephole["float-map-scatter-vector"] != 1 {
			t.Fatalf("f32 stride%d rejected: %v", stride, stats.Peephole)
		}
	}
}

// Keep a terminating, in-bounds interleaved case for every target stride and
// scalar width, independent of the adversarial fixture parameter cycles.
func TestFloatMapScatterEveryStrideExecutes(t *testing.T) {
	old := floatMapScatterEnabled
	defer func() { floatMapScatterEnabled = old }()
	for _, wide := range []bool{false, true} {
		width := uint32(4)
		if wide {
			width = 8
		}
		for _, scale := range []uint32{2, 3, 5, 8, 80} {
			stride := width * scale
			for _, phase := range []uint32{0, width, stride - width} {
				for _, count := range []uint32{2, 6, 18, 34} {
					dst, src := uint32(256)+phase, uint32(256)
					m := scatterFixtureTyped(t, dst, src, 60000, 0, stride, count, wide)
					want, trap := scatterOracleTyped(dst, src, 60000, 0, stride, count, wide)
					if trap {
						t.Fatal("invalid positive fixture")
					}
					for _, on := range []bool{false, true} {
						floatMapScatterEnabled = on
						for _, compact := range []bool{false, true} {
							got, err := runScalarUnrollMemory(t, m, CompileOptions{CompactNative: compact})
							if err != nil || !bytes.Equal(got, want) {
								t.Fatalf("wide%v scale%d phase%d count%d on%v compact%v err%v equal%v", wide, scale, phase, count, on, compact, err, bytes.Equal(got, want))
							}
						}
					}
				}
			}
		}
	}
}

func TestFloatMapScatterBudgetKeepsOrdinaryMaps(t *testing.T) {
	probe := fn{nLocals: 6, localType: []machineType{mtI32, mtI32, mtI32, mtI32, mtI32, mtF64}}
	for _, exhausted := range []bool{false, true} {
		for _, stride := range []uint32{8, 640} {
			probe.floatMapScatterWork = 0
			if exhausted {
				probe.floatMapScatterWork = floatMapScatterWorkLimit
			}
			m := scatterFixture(t, 8, 0, 60000, 0, stride, 34)
			body := m.Code[0].BodyBytes
			at := bytes.LastIndex(body, []byte{0x03, 0x40})
			r := wasm.ReaderFrom(body[at+2:])
			p, ok := probe.inspectFloatMapLoop(&r)
			want := stride == 8 || !exhausted
			if ok != want || r.Offset() != 0 {
				t.Fatalf("stride%d exhausted%v accepted%v sourceOffset%d", stride, exhausted, ok, r.Offset())
			}
			if ok && stride > 8 && (!p.scatterCandidate || p.work > p.scatterBudget) {
				t.Fatal("scatter analysis escaped its remaining budget")
			}
		}
	}
}
