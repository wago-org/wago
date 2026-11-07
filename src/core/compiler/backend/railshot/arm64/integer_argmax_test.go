//go:build (linux || darwin) && arm64

package arm64

import (
	"bytes"
	"encoding/binary"
	"errors"
	"math/rand"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	rt "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func integerArgmaxFixture(t testing.TB, words []uint32, simple bool, extra int) *wasm.Module {
	b := []byte{1}
	b = append(b, wasmtest.ULEB(uint32(9+extra))...)
	b = append(b, 0x7f)
	c := func(v uint32) { b = append(b, 0x41); b = append(b, wasmtest.SLEB32(int32(v))...) }
	get := func(x byte) { b = append(b, 0x20, x) }
	set := func(x byte) { b = append(b, 0x21, x) }
	for i, v := range words {
		c(uint32(i * 4))
		c(v)
		b = append(b, 0x36, 2, 0)
	}
	for i, src := range []byte{0, 1, 3} {
		get(src)
		set(byte(i + 4))
	}
	get(1)
	c(1)
	b = append(b, 0x6b)
	set(7)
	for i, value := range []uint32{0x3456789a, 0xf1234567, 0x85ebca6b, 0x9e3779b9} {
		if simple {
			value = 0
			if i == 2 {
				value = 1
			}
			c(value)
		} else {
			get(3)
			c(value)
			b = append(b, 0x73)
		}
		set(byte(i + 9))
	}
	b = append(b, 0x03, 0x40, 0x20, 9, 0x20, 4, 0x41, 0x7c, 0x6a, 0x28, 2, 0,
		0x6b, 0x20, 11, 0x6c, 0x20, 4, 0x28, 2, 0, 0x20, 10, 0x6b, 0x20, 12,
		0x6c, 0x6a, 0x22, 8, 0x20, 8, 0x41, 31, 0x75, 0x22, 8, 0x73,
		0x20, 8, 0x6b, 0x22, 8, 0x20, 6, 0x20, 8, 0x20, 6, 0x4b, 0x22, 8,
		0x1b, 0x21, 6, 0x20, 5, 0x20, 7, 0x20, 8, 0x1b, 0x21, 7,
		0x20, 4, 0x41, 8, 0x6a, 0x21, 4, 0x20, 2, 0x20, 5, 0x41, 1, 0x6a,
		0x22, 5, 0x47, 0x0d, 0, 0x0b)
	for i, local := range []byte{4, 5, 6, 7, 8} {
		c(64000 + uint32(i)*4)
		get(local)
		b = append(b, 0x36, 2, 0)
	}
	c(0)
	b = append(b, 0x0b)
	return modMem(t, 1, []wasm.ValType{wasm.I32, wasm.I32, wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, b)
}

func integerArgmaxOracle(words []uint32, simple bool, args [4]uint64) ([]byte, bool) {
	mem := make([]byte, 65536)
	for i, v := range words {
		binary.LittleEndian.PutUint32(mem[i*4:], v)
	}
	pointer, index, limit, seed := uint32(args[0]), uint32(args[1]), uint32(args[2]), uint32(args[3])
	maximum, selected := seed, index-1
	x, y, dy, dx := seed^0x3456789a, seed^0xf1234567, seed^0x85ebca6b, seed^0x9e3779b9
	if simple {
		x, y, dy, dx = 0, 0, 1, 0
	}
	for iteration := 0; iteration < 10000; iteration++ {
		xAddress := uint64(pointer - 4)
		if xAddress+4 > uint64(len(mem)) || uint64(pointer)+4 > uint64(len(mem)) {
			return mem, true
		}
		pointX, pointY := binary.LittleEndian.Uint32(mem[xAddress:]), binary.LittleEndian.Uint32(mem[pointer:])
		value := (x-pointX)*dy + (pointY-y)*dx
		sign := uint32(int32(value) >> 31)
		distance := (value ^ sign) - sign
		flag := uint32(0)
		if distance > maximum {
			maximum, selected, flag = distance, index, 1
		}
		pointer += 8
		index++
		if index == limit {
			for i, v := range []uint32{pointer, index, maximum, selected, flag} {
				binary.LittleEndian.PutUint32(mem[64000+i*4:], v)
			}
			return mem, false
		}
	}
	panic("fixture failed to stop or trap")
}

func TestIntegerArgmaxMemoryStateAndTraps(t *testing.T) {
	rng := rand.New(rand.NewSource(417))
	counts := [...]uint32{1, 4, 8, 9, 10, 15, 16, 17, 31, 32, 33, 63}
	for fixture := 0; fixture < 256; fixture++ {
		words := make([]uint32, 256)
		for i := range words {
			words[i] = rng.Uint32()
			if fixture%4 == 0 {
				words[i] = 0x80000000 // Many equal unsigned maxima, including abs(MinInt32).
			}
		}
		start, pointer := uint32(fixture%13), uint32(4+fixture%3)
		if fixture%16 == 0 {
			pointer = []uint32{0, 3, 65532, 0xfffffffc}[(fixture/16)%4]
		}
		if fixture%17 == 0 {
			start = 0xfffffff8
		}
		args := [4]uint64{uint64(pointer) | 0x8877665500000000, uint64(start) | 0x9988776600000000, uint64(start + counts[fixture%len(counts)]), uint64([]uint32{0, 1, 0x7fffffff, 0x80000000, 0xffffffff, rng.Uint32()}[fixture%6])}
		for _, simple := range []bool{false, true} {
			want, trap := integerArgmaxOracle(words, simple, args)
			for _, extra := range []int{0, 70} {
				m := integerArgmaxFixture(t, words, simple, extra)
				for _, enabled := range []bool{false, true} {
					got, err := runScalarUnrollMemory(t, m, CompileOptions{Optimizations: map[string]bool{"integer-argmax-vector": enabled}}, args[:]...)
					var trapped *rt.TrapError
					if (err != nil) != trap || err != nil && (!errors.As(err, &trapped) || trapped.Code != rt.TrapLinMemOutOfBounds) || !bytes.Equal(got, want) {
						t.Fatalf("fixture%d simple%v extra%d enabled%v args%x err%v trap%v equal%v", fixture, simple, extra, enabled, args, err, trap, bytes.Equal(got, want))
					}
				}
			}
		}
	}
}

func TestIntegerArgmaxSelection(t *testing.T) {
	requireCompilerDiagnostics(t)
	for _, enabled := range []bool{false, true} {
		stats := &ModuleStats{}
		cm, err := CompileModuleWith(integerArgmaxFixture(t, nil, false, 0), CompileOptions{Stats: stats, Optimizations: map[string]bool{"integer-argmax-vector": enabled}})
		if err != nil {
			t.Fatal(err)
		}
		cm.CodeImage.Close()
		if got := stats.Funcs[0].Peephole["integer-argmax-vector"]; (got != 0) != enabled {
			t.Fatalf("enabled%v admission%d", enabled, got)
		}
	}
}

func TestIntegerArgmaxReaderAndRejections(t *testing.T) {
	m := integerArgmaxFixture(t, nil, false, 0)
	f := fn{m: m, nLocals: 13, nParams: 4, localType: make([]machineType, 13)}
	for i := range f.localType {
		f.localType[i] = mtI32
	}
	body := m.Code[0].BodyBytes
	start := bytes.Index(body, []byte{0x03, 0x40, 0x20, 9, 0x20, 4, 0x41, 0x7c})
	if start < 0 {
		t.Fatal("missing loop fixture")
	}
	body = body[start+2:]
	r := wasm.NewReader(body)
	if _, ok := f.inspectIntegerArgmax(r); !ok || r.Offset() != 0 {
		t.Fatalf("admission%v changed reader to%d", ok, r.Offset())
	}
	end := bytes.Index(body, []byte{0x47, 0x0d, 0, 0x0b}) + 4
	for at := 0; at < end; at++ {
		if _, ok := f.inspectIntegerArgmax(wasm.NewReader(body[:at])); ok {
			t.Fatalf("accepted truncated loop at%d", at)
		}
	}
	for _, mutation := range []struct{ from, to []byte }{
		{[]byte{0x4b, 0x22, 8}, []byte{0x4a, 0x22, 8}},
		{[]byte{0x41, 31, 0x75}, []byte{0x41, 30, 0x75}},
		{[]byte{0x20, 4, 0x41, 8, 0x6a}, []byte{0x20, 4, 0x41, 4, 0x6a}},
		{[]byte{0x20, 9, 0x20, 4}, []byte{0x20, 4, 0x20, 4}},
		{[]byte{0x20, 5, 0x20, 7}, []byte{0x20, 5, 0x20, 6}},
		{[]byte{0x21, 7, 0x20, 4}, []byte{0x21, 7, 0x41, 0, 0x41, 1, 0x36, 2, 0, 0x20, 4}},
		{[]byte{0x47, 0x0d, 0, 0x0b}, []byte{0x47, 0x0d, 1, 0x0b}},
		{[]byte{0x28, 2, 0}, []byte{0x28, 2, 4}},
	} {
		if !bytes.Contains(body, mutation.from) {
			t.Fatal("missing mutation fixture")
		}
		changed := bytes.Replace(body, mutation.from, mutation.to, 1)
		if _, ok := f.inspectIntegerArgmax(wasm.NewReader(changed)); ok {
			t.Fatalf("accepted mutation%x", mutation.to)
		}
	}
}

func TestIntegerArgmaxCallerFPAndVectors(t *testing.T) {
	words := make([]uint32, 256)
	for i := range words {
		words[i] = uint32(i)*0x9e3779b9 + 0x85ebca6b
	}
	callee := integerArgmaxFixture(t, words, false, 0)
	args := [4]uint64{4, 3, 64, 0}
	want, trapped := integerArgmaxOracle(words, false, args)
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
	for i := byte(0); i < 4; i++ {
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
	m.FuncTypes = append(m.FuncTypes, m.FuncTypes[0])
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
			if diagnosticsEnabled && stats.Funcs[1].Peephole["integer-argmax-vector"] != 1 {
				t.Fatalf("callee did not vectorize: %v", stats.Funcs[1].Peephole)
			}
		}
	}
}
