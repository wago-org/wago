//go:build (linux || darwin) && arm64

package arm64

import (
	"bytes"
	"encoding/binary"
	"errors"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	rt "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"math/rand"
	"testing"
)

func maskedXorStripeFixture(t testing.TB, words []uint32, bit, base, limit uint32, extra int) *wasm.Module {
	b := []byte{1}
	b = append(b, wasmtest.ULEB(uint32(3+extra))...)
	b = append(b, 0x7f)
	c := func(x uint32) { b = append(b, 0x41); b = append(b, wasmtest.SLEB32(int32(x))...) }
	get := func(x byte) { b = append(b, 0x20, x) }
	set := func(x byte) { b = append(b, 0x21, x) }
	for i, x := range words {
		c(uint32(i * 4))
		c(x)
		b = append(b, 0x36, 2, 0)
	}
	get(0)
	set(2)
	get(1)
	set(3)
	b = append(b, 0x03, 0x40, 0x02, 0x40)
	get(3)
	c(bit)
	b = append(b, 0x71, 0x22, 4, 0x45, 0x0d, 0)
	get(2)
	get(2)
	b = append(b, 0x28, 2, 0)
	get(3)
	c(0x3fffffff &^ (bit | 1))
	b = append(b, 0x71)
	c(2)
	b = append(b, 0x74)
	c(base)
	b = append(b, 0x6a, 0x28, 2, 0, 0x73, 0x36, 2, 0, 0x0b)
	b = append(b, 0x02, 0x40)
	get(4)
	b = append(b, 0x45, 0x0d, 0)
	get(2)
	c(4)
	b = append(b, 0x6a, 0x22, 4)
	get(4)
	b = append(b, 0x28, 2, 0)
	get(3)
	c(1)
	b = append(b, 0x6a)
	c(0x3fffffff &^ bit)
	b = append(b, 0x71)
	c(2)
	b = append(b, 0x74)
	c(base)
	b = append(b, 0x6a, 0x28, 2, 0, 0x73, 0x36, 2, 0, 0x0b)
	get(2)
	c(8)
	b = append(b, 0x6a)
	set(2)
	get(3)
	c(2)
	b = append(b, 0x6a, 0x22, 3)
	c(limit)
	b = append(b, 0x47, 0x0d, 0, 0x0b)
	for i, x := range []byte{2, 3, 4} {
		c(uint32(64000 + i*4))
		get(x)
		b = append(b, 0x36, 2, 0)
	}
	c(0)
	b = append(b, 0x0b)
	return modMem(t, 1, []wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, b)
}

func maskedXorStripeOracle(words []uint32, bit, base, limit uint32, args [2]uint64) ([]byte, bool) {
	mem := make([]byte, 65536)
	for i, x := range words {
		binary.LittleEndian.PutUint32(mem[i*4:], x)
	}
	pointer, index, temp := uint32(args[0]), uint32(args[1]), uint32(0)
	for iteration := 0; iteration < 10000; iteration++ {
		temp = index & bit
		if temp != 0 {
			for lane := uint32(0); lane < 2; lane++ {
				target := pointer + lane*4
				if lane == 1 {
					temp = target
				}
				if uint64(target)+4 > uint64(len(mem)) {
					return mem, true
				}
				value := binary.LittleEndian.Uint32(mem[target:])
				source := base + ((index+lane)&(0x3fffffff&^bit))*4
				if uint64(source)+4 > uint64(len(mem)) {
					return mem, true
				}
				value ^= binary.LittleEndian.Uint32(mem[source:])
				binary.LittleEndian.PutUint32(mem[target:], value)
			}
		}
		pointer += 8
		index += 2
		if index == limit {
			for i, x := range []uint32{pointer, index, temp} {
				binary.LittleEndian.PutUint32(mem[64000+i*4:], x)
			}
			return mem, false
		}
	}
	panic("fixture did not stop or trap")
}

func TestMaskedXorStripeMemoryStateAndTraps(t *testing.T) {
	rng := rand.New(rand.NewSource(814))
	for fixture := 0; fixture < 128; fixture++ {
		words := make([]uint32, 512)
		for i := range words {
			words[i] = rng.Uint32()
		}
		bit := uint32(4) << uint(fixture%8)
		base := []uint32{0, 1, 3, 16, 128, 65500, 0xfffffff0}[fixture%7]
		limit := []uint32{6, 8, 10, 14, 32, 64, 128, 256}[fixture%8]
		index := []uint32{0, 2, 4, limit - 2, limit - 4}[fixture%5]
		if fixture%19 == 0 {
			index = 0xfffffffc
			bit = 4
		}
		pointer := base + index*4
		if fixture%11 == 0 {
			pointer++
		}
		if fixture%17 == 0 {
			pointer = 65532
			index = 4
			bit = 4
		}
		args := [2]uint64{uint64(pointer) | 0x8877665500000000, uint64(index) | 0x9988776600000000}
		want, trap := maskedXorStripeOracle(words, bit, base, limit, args)
		for _, extra := range []int{0, 70} {
			m := maskedXorStripeFixture(t, words, bit, base, limit, extra)
			for _, on := range []bool{false, true} {
				got, err := runScalarUnrollMemory(t, m, CompileOptions{Optimizations: map[string]bool{"masked-xor-stripe": on}}, args[:]...)
				var trapped *rt.TrapError
				if (err != nil) != trap || err != nil && (!errors.As(err, &trapped) || trapped.Code != rt.TrapLinMemOutOfBounds) || !bytes.Equal(got, want) {
					t.Fatalf("fixture%d bit%d base%x limit%d args%x extra%d on%v err%v trap%v equal%v", fixture, bit, base, limit, args, extra, on, err, trap, bytes.Equal(got, want))
				}
			}
		}
	}
}

func TestMaskedXorStripeSelection(t *testing.T) {
	requireCompilerDiagnostics(t)
	for _, on := range []bool{false, true} {
		stats := &ModuleStats{}
		cm, err := CompileModuleWith(maskedXorStripeFixture(t, nil, 16, 0, 128, 0), CompileOptions{Stats: stats, Optimizations: map[string]bool{"masked-xor-stripe": on}})
		if err != nil {
			t.Fatal(err)
		}
		cm.CodeImage.Close()
		if got := stats.Funcs[0].Peephole["masked-xor-stripe"]; (got != 0) != on {
			t.Fatalf("on%v admission%d", on, got)
		}
	}
}

func TestMaskedXorStripeReaderAndRejections(t *testing.T) {
	m := maskedXorStripeFixture(t, nil, 16, 0, 128, 0)
	f := fn{m: m, nLocals: 5, nParams: 2, localType: []machineType{mtI32, mtI32, mtI32, mtI32, mtI32}}
	b := m.Code[0].BodyBytes
	start := bytes.Index(b, []byte{0x03, 0x40, 0x02, 0x40})
	if start < 0 {
		t.Fatal("missing loop")
	}
	b = b[start+2:]
	r := wasm.NewReader(b)
	if _, ok := f.inspectMaskedXorStripe(r); !ok || r.Offset() != 0 {
		t.Fatal("missing unchanged-reader admission")
	}
	end := bytes.Index(b, []byte{0x47, 0x0d, 0, 0x0b}) + 4
	for at := 0; at < end; at++ {
		if _, ok := f.inspectMaskedXorStripe(wasm.NewReader(b[:at])); ok {
			t.Fatalf("truncated%d admitted", at)
		}
	}
	for _, mutation := range []struct{ from, to []byte }{
		{[]byte{0x22, 4, 0x45}, []byte{0x22, 2, 0x45}},
		{[]byte{0x41, 8, 0x6a, 0x21, 2}, []byte{0x41, 4, 0x6a, 0x21, 2}},
		{[]byte{0x28, 2, 0}, []byte{0x28, 2, 4}},
		{[]byte{0x73, 0x36}, []byte{0x72, 0x36}},
		{[]byte{0x47, 0x0d, 0}, []byte{0x47, 0x0d, 1}},
		{[]byte{0x20, 4, 0x45}, []byte{0x20, 2, 0x45}},
	} {
		changed := bytes.Replace(b, mutation.from, mutation.to, 1)
		if bytes.Equal(changed, b) {
			t.Fatal("mutation not applied")
		}
		if _, ok := f.inspectMaskedXorStripe(wasm.NewReader(changed)); ok {
			t.Fatalf("unsafe mutation %x admitted", mutation.from)
		}
	}
	for _, params := range [][3]uint32{{2, 0, 128}, {3, 0, 128}, {16, 0, 127}, {16, 0, 4}, {16, 0xfffffffc, 128}} {
		bad := maskedXorStripeFixture(t, nil, params[0], params[1], params[2], 0).Code[0].BodyBytes
		pos := bytes.Index(bad, []byte{0x03, 0x40, 0x02, 0x40})
		if _, ok := f.inspectMaskedXorStripe(wasm.NewReader(bad[pos+2:])); ok {
			t.Fatalf("unsafe parameters%x admitted", params)
		}
	}
}

func TestMaskedXorStripeCallerFPAndVectors(t *testing.T) {
	words := make([]uint32, 256)
	for i := range words {
		words[i] = uint32(i)*0x9e3779b9 + 0x85ebca6b
	}
	callee := maskedXorStripeFixture(t, words, 16, 0, 128, 0)
	args := [4]uint64{0, 0, 0, 0}
	want, trapped := maskedXorStripeOracle(words, 16, 0, 128, [2]uint64{args[0], args[1]})
	if trapped {
		t.Fatal("invalid caller fixture")
	}
	b := []byte{2, 12, 0x7c, 4, 0x7b}
	for i := byte(0); i < 12; i++ {
		value := uint64(0x7ff0000012345678) + uint64(i)*0x100000001
		b = append(b, 0x44)
		b = binary.LittleEndian.AppendUint64(b, value)
		b = append(b, 0x21, i+4)
		binary.LittleEndian.PutUint64(want[32768+int(i)*8:], value)
	}
	for i := byte(0); i < 4; i++ {
		b = append(b, 0xfd, 0x0c)
		for lane := byte(0); lane < 16; lane++ {
			b = append(b, i*32+lane+1)
			want[33024+int(i)*16+int(lane)] = i*32 + lane + 1
		}
		b = append(b, 0x21, i+16)
	}
	b = append(b, 0x41)
	b = append(b, wasmtest.SLEB32(33280)...)
	b = append(b, 0x20, 16)
	for i := byte(0); i < 2; i++ {
		b = append(b, 0x20, i)
	}
	b = append(b, 0x10, 1, 0x1a, 0xfd, 0x0b, 0, 0)
	copy(want[33280:33296], want[33024:33040])
	for i := byte(0); i < 12; i++ {
		b = append(b, 0x41)
		b = append(b, wasmtest.SLEB32(32768+int32(i)*8)...)
		b = append(b, 0x20, i+4, 0x39, 0, 0)
	}
	for i := byte(0); i < 4; i++ {
		b = append(b, 0x41)
		b = append(b, wasmtest.SLEB32(33024+int32(i)*16)...)
		b = append(b, 0x20, i+16, 0xfd, 0x0b, 0, 0)
	}
	b = append(b, 0x41, 0, 0x0b)
	m := modMem(t, 1, []wasm.ValType{wasm.I32, wasm.I32, wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, b)
	m.Types = append(m.Types, callee.Types[0])
	m.FuncTypes = append(m.FuncTypes, wasm.TypeIdx{Index: 1})
	m.Code = append(m.Code, callee.Code[0])
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	for _, compact := range []bool{false, true} {
		for _, stackReg := range []bool{false, true} {
			stats := &ModuleStats{}
			opts := CompileOptions{CompactNative: compact, Optimizations: map[string]bool{"inline": false, "stack-reg": stackReg}}
			if diagnosticsEnabled {
				opts.Stats = stats
			}
			got, err := runScalarUnrollMemory(t, m, opts, args[:]...)
			if err != nil || !bytes.Equal(got, want) {
				t.Fatalf("compact%v stackReg%v err%v equal%v", compact, stackReg, err, bytes.Equal(got, want))
			}
			if diagnosticsEnabled && stats.Funcs[1].Peephole["masked-xor-stripe"] != 1 {
				t.Fatalf("callee did not vectorize: %v", stats.Funcs[1].Peephole)
			}
		}
	}
}

func TestMaskedXorStripeLargeLimitsAndPartialTraps(t *testing.T) {
	words := make([]uint32, 512)
	for i := range words {
		words[i] = uint32(i)*0x85ebca6b + 17
	}
	for _, c := range [][5]uint32{
		{4, 0, 8192, 0, 0}, {8, 1, 8192, 1, 0}, {16, 0, 16384, 0, 0},
		{4, 65500, 16, 65500, 0}, {4, 65501, 16, 65501, 0},
		{4, 0, 128, 65532, 4}, {4, 0xfffffff0, 16, 0xfffffff0, 4},
		{1 << 29, 0, 128, 0, 0},
	} {
		args := [2]uint64{uint64(c[3]), uint64(c[4])}
		want, trap := maskedXorStripeOracle(words, c[0], c[1], c[2], args)
		for _, on := range []bool{false, true} {
			got, err := runScalarUnrollMemory(t, maskedXorStripeFixture(t, words, c[0], c[1], c[2], 0), CompileOptions{Optimizations: map[string]bool{"masked-xor-stripe": on}}, args[:]...)
			var tr *rt.TrapError
			if (err != nil) != trap || err != nil && (!errors.As(err, &tr) || tr.Code != rt.TrapLinMemOutOfBounds) || !bytes.Equal(got, want) {
				t.Fatalf("case%x on%v err%v trap%v equal%v", c, on, err, trap, bytes.Equal(got, want))
			}
		}
	}
}
