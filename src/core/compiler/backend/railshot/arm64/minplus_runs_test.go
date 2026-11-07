//go:build (linux || darwin) && arm64

package arm64

import (
	"bytes"
	"encoding/binary"
	"errors"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	rt "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func minPlusRunsFixture(t testing.TB, dst, src, pivot, start, limit uint32) *wasm.Module {
	b := []byte{1, 9, 0x7f}
	c := func(v uint32) { b = append(b, 0x41); b = append(b, wasmtest.SLEB32(int32(v))...) }
	c(0x8142f301)
	b = append(b, 0x21, 7, 0x41, 0, 0x21, 8, 0x03, 0x40, 0x20, 8, 0x20, 7, 0x36, 2, 0, 0x20, 7)
	c(1664525)
	b = append(b, 0x6c)
	c(1013904223)
	b = append(b, 0x6a, 0x21, 7, 0x20, 8, 0x41, 4, 0x6a, 0x22, 8)
	c(65536)
	b = append(b, 0x47, 0x0d, 0, 0x0b)
	for local, v := range []uint32{dst, src, pivot, start} {
		c(v)
		b = append(b, 0x21, byte(local))
	}
	c(64016)
	c(123)
	b = append(b, 0x03, 0x40)
	for i := uint32(0); i < 2; i++ {
		b = append(b, 0x20, 0, 0x20, 3, 0x6a)
		if i != 0 {
			c(4)
			b = append(b, 0x6a)
		}
		b = append(b, 0x22, 4, 0x20, 4, 0x28, 2, 0, 0x22, 5, 0x20, 1, 0x20, 3, 0x6a)
		if i != 0 {
			c(4)
			b = append(b, 0x6a)
		}
		b = append(b, 0x28, 2, 0, 0x20, 2, 0x28, 2, 0, 0x6a, 0x22, 6, 0x20, 6, 0x20, 5, 0x4a, 0x1b, 0x36, 2, 0)
	}
	b = append(b, 0x20, 3, 0x41, 8, 0x6a, 0x22, 3)
	c(limit)
	b = append(b, 0x47, 0x0d, 0, 0x0b, 0x36, 2, 0)
	for i, local := range []byte{3, 4, 5, 6} {
		c(64000 + uint32(i)*4)
		b = append(b, 0x20, local, 0x36, 2, 0)
	}
	c(0)
	b = append(b, 0x0b)
	return modMem(t, 1, nil, []wasm.ValType{wasm.I32}, b)
}

func minPlusRunsOracle(dst, src, pivot, start, limit uint32) ([]byte, bool) {
	mem := make([]byte, 65536)
	value := uint32(0x8142f301)
	for off := 0; off < len(mem); off += 4 {
		binary.LittleEndian.PutUint32(mem[off:], value)
		value = value*1664525 + 1013904223
	}
	c := start
	var lastAddress, lastOld, lastSum uint32
	for iteration := 0; iteration < 10000; iteration++ {
		for off := uint32(0); off < 8; off += 4 {
			d, s := dst+c+off, src+c+off
			if uint64(d)+4 > 65536 {
				return mem, true
			}
			lastOld = binary.LittleEndian.Uint32(mem[d:])
			if uint64(s)+4 > 65536 || uint64(pivot)+4 > 65536 {
				return mem, true
			}
			lastAddress = d
			lastSum = binary.LittleEndian.Uint32(mem[s:]) + binary.LittleEndian.Uint32(mem[pivot:])
			v := lastSum
			if int32(lastOld) < int32(v) {
				v = lastOld
			}
			binary.LittleEndian.PutUint32(mem[d:], v)
		}
		c += 8
		if c == limit {
			binary.LittleEndian.PutUint32(mem[64016:], 123)
			for i, v := range []uint32{c, lastAddress, lastOld, lastSum} {
				binary.LittleEndian.PutUint32(mem[64000+i*4:], v)
			}
			return mem, false
		}
	}
	panic("fixture neither finished nor trapped")
}

func TestMinPlusRunsFullMemoryStateAndTraps(t *testing.T) {
	old := minPlusRunsEnabled
	defer func() { minPlusRunsEnabled = old }()
	for fixture := 0; fixture < 384; fixture++ {
		dst := []uint32{0, 1, 3, 64, 128, 65500, 0xfffffff0}[fixture%7]
		src := []uint32{0, 1, 4, 8192, 16384, 0xfffffff0}[fixture%6]
		limit := []uint32{264, 272, 280, 312, 328, 720, 744, 1080}[fixture%8]
		start := []uint32{0, 8, 16}[fixture%3]
		pivot := dst + []uint32{0, 1, 3, 4, 7, 8, 15, 16, 31, 32, 60, 63, 64, 65, 127, 128, 255, 256, 263, 264, limit - 1, limit, limit + 1}[fixture%23]
		if fixture%11 == 0 {
			pivot = []uint32{0, 65532, 65533, 0xfffffffc}[fixture/11%4]
		}
		if fixture%17 == 0 {
			start = 1
		}
		m := minPlusRunsFixture(t, dst, src, pivot, start, limit)
		want, trap := minPlusRunsOracle(dst, src, pivot, start, limit)
		for _, on := range []bool{false, true} {
			minPlusRunsEnabled = on
			for _, compact := range []bool{false, true} {
				got, err := runScalarUnrollMemory(t, m, CompileOptions{CompactNative: compact})
				var tr *rt.TrapError
				if (err != nil) != trap || err != nil && (!errors.As(err, &tr) || tr.Code != rt.TrapLinMemOutOfBounds) || !bytes.Equal(got, want) {
					t.Fatalf("case%d d%x s%x p%x start%d limit%d on%v compact%v err%v trap%v equal%v", fixture, dst, src, pivot, start, limit, on, compact, err, trap, bytes.Equal(got, want))
				}
			}
		}
	}
}

func TestMinPlusRunsAdmissionAndRollback(t *testing.T) {
	requireCompilerDiagnostics(t)
	old := minPlusRunsEnabled
	defer func() { minPlusRunsEnabled = old }()
	for _, limit := range []uint32{136, 256, 264, 272, 720} {
		for _, on := range []bool{false, true} {
			minPlusRunsEnabled = on
			stats := &ModuleStats{}
			cm, err := CompileModuleWith(minPlusRunsFixture(t, 0, 8192, 63, 0, limit), CompileOptions{Stats: stats})
			if err != nil {
				t.Fatal(err)
			}
			cm.CodeImage.Close()
			if got := stats.Funcs[0].Peephole["minplus-loop-runs"]; (got != 0) != (on && limit >= 264) {
				t.Fatalf("limit%d on%v admission%d", limit, on, got)
			}
		}
	}
}

func TestMinPlusRunsCallerFPAndVectors(t *testing.T) {
	callee := minPlusRunsFixture(t, 0, 8192, 63, 0, 720)
	args := [4]uint64{0, 0, 0, 0}
	want, trapped := minPlusRunsOracle(0, 8192, 63, 0, 720)
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
			if diagnosticsEnabled && stats.Funcs[1].Peephole["minplus-loop-runs"] != 1 {
				t.Fatalf("callee did not vectorize: %v", stats.Funcs[1].Peephole)
			}
		}
	}
}
