//go:build (linux || darwin) && arm64

package arm64

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func integerByteMapFixture(t testing.TB, divisor, delta, offset, limit uint32, extra int, zero bool) *wasm.Module {
	t.Helper()
	b := []byte{1}
	b = append(b, wasmtest.ULEB(uint32(extra+4))...)
	b = append(b, 0x7f)
	base := byte(0)
	if zero {
		base = 4
	} // Declared zero homes, rather than parameter homes.
	get := func(x byte) { b = append(b, 0x20, x+base) }
	set := func(x byte, tee bool) {
		op := byte(0x21)
		if tee {
			op = 0x22
		}
		b = append(b, op, x+base)
	}
	c := func(x uint32) { b = append(b, 0x41); b = append(b, wasmtest.SLEB32(int32(x))...) }
	if zero {
		c(1)
		set(1, false)
		c(delta / 2)
		set(3, false)
	}
	b = append(b, 0x03, 0x40)
	for _, odd := range []bool{true, false} {
		get(1)
		v := offset
		if odd {
			v++
		}
		c(v)
		b = append(b, 0x6a)
		scaled, counter := byte(2), byte(0)
		if odd {
			scaled, counter = 3, 1
		}
		get(scaled)
		get(counter)
		c(divisor)
		b = append(b, 0x6e, 0x6a, 0x3a, 0, 0)
	}
	for _, x := range []byte{2, 3, 0, 1} {
		get(x)
		step := delta
		if x < 2 {
			step = 2
		}
		c(step)
		b = append(b, 0x6a)
		set(x, x == 1)
	}
	c(limit)
	b = append(b, 0x47, 0x0d, 0, 0x0b)
	for x := byte(0); x < 4; x++ {
		c(uint32(x) * 4)
		get(x)
		b = append(b, 0x36, 2, 0)
	}
	c(0)
	b = append(b, 0x0b)
	return modMem(t, 1, []wasm.ValType{wasm.I32, wasm.I32, wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, b)
}

func integerByteMapOracle(args [4]uint64, divisor, delta, offset, limit uint32) ([]byte, bool) {
	mem := make([]byte, 65536)
	even, odd, se, so := uint32(args[0]), uint32(args[1]), uint32(args[2]), uint32(args[3])
	for i := 0; i < 40000; i++ {
		for _, v := range []struct{ address, value uint32 }{{odd + offset + 1, so + odd/divisor}, {odd + offset, se + even/divisor}} {
			if v.address >= uint32(len(mem)) {
				return mem, true
			}
			mem[v.address] = byte(v.value)
		}
		se += delta
		so += delta
		even += 2
		odd += 2
		if odd == limit {
			for x, v := range []uint32{even, odd, se, so} {
				binary.LittleEndian.PutUint32(mem[x*4:], v)
			}
			return mem, false
		}
	}
	panic("oracle fixture failed to terminate or trap")
}

func TestIntegerByteMapMemoryAndLocals(t *testing.T) {
	for _, divisor := range []uint32{1, 2, 3, 7, 31, 127, 65537, 0x80000000, 0xfffffffb, 0xffffffff} {
		for _, delta := range []uint32{0, 262, 0xfffffffe} {
			for _, count := range []uint32{1, 2, 16, 17, 18, 19, 33, 64} {
				limit := 1 + 2*count
				args := [4]uint64{0, 1, 0xffffffb0, uint64(uint32(0xffffffb0 + delta/2))}
				want, trap := integerByteMapOracle(args, divisor, delta, 63, limit)
				for _, extra := range []int{0, 70} {
					m := integerByteMapFixture(t, divisor, delta, 63, limit, extra, false)
					got, err := runScalarUnrollMemory(t, m, CompileOptions{}, args[:]...)
					if (err != nil) != trap || !bytes.Equal(got, want) {
						t.Fatalf("d%d delta%x count%d extra%d err%v trap%v equal%v", divisor, delta, count, extra, err, trap, bytes.Equal(got, want))
					}
				}
			}
		}
	}
}

func TestIntegerByteMapFallbackAndWrap(t *testing.T) {
	for i, tc := range []struct {
		args                 [4]uint64
		delta, offset, limit uint32
	}{
		{[4]uint64{0xffffffff, 0, 0xfffffffe, 129}, 262, 64, 64},
		{[4]uint64{0x8877665500000000, 0x8877665500000001, 0x8877665500000000, 0x8877665500000083}, 262, 64, 65},
		{[4]uint64{0, 1, 0, 131}, 262, 65500, 65},                // Partial memory effects before trap.
		{[4]uint64{0, 1, 0, 132}, 262, 64, 65},                   // Coefficient guard.
		{[4]uint64{2, 1, 0, 131}, 262, 64, 65},                   // Counter relation guard.
		{[4]uint64{0, 1, 0, 131}, 262, 64, 64},                   // Unreachable parity, eventually traps.
		{[4]uint64{0xfffffffe, 0xffffffff, 0, 131}, 262, 66, 63}, // Counter wrap fallback.
		{[4]uint64{0, 1, 0, 0x80000083}, 262, 64, 65},            // Other solution to 2*C=delta.
		{[4]uint64{0, 1, 0, 131}, 262, 0xfffffffe, 65},           // Address wrap, then partial trap.
	} {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			want, trap := integerByteMapOracle(tc.args, 7, tc.delta, tc.offset, tc.limit)
			for _, on := range []bool{false, true} {
				m := integerByteMapFixture(t, 7, tc.delta, tc.offset, tc.limit, 0, false)
				got, err := runScalarUnrollMemory(t, m, CompileOptions{Optimizations: map[string]bool{"integer-byte-map": on}}, tc.args[:]...)
				if (err != nil) != trap || !bytes.Equal(got, want) {
					t.Fatalf("on%v err%v trap%v equal%v", on, err, trap, bytes.Equal(got, want))
				}
			}
		})
	}
}

func TestIntegerByteMapDeclaredZeroAndSelection(t *testing.T) {
	for _, extra := range []int{0, 70} {
		m := integerByteMapFixture(t, 7, 262, 64, 65, extra, true)
		args := [4]uint64{0, 1, 0, 131}
		want, _ := integerByteMapOracle(args, 7, 262, 64, 65)
		for _, on := range []bool{false, true} {
			var stats ModuleStats
			opts := CompileOptions{Optimizations: map[string]bool{"integer-byte-map": on}}
			if diagnosticsEnabled {
				opts.Stats = &stats
			}
			got, err := runScalarUnrollMemory(t, m, opts, 999, 999, 999, 999)
			if err != nil || !bytes.Equal(got, want) {
				t.Fatalf("extra%d on%v err%v equal%v", extra, on, err, bytes.Equal(got, want))
			}
			if diagnosticsEnabled && (stats.Funcs[0].Peephole["integer-byte-map-vector"] != 0) != on {
				t.Fatalf("extra%d on%v stats%v", extra, on, stats.Funcs[0].Peephole)
			}
		}
	}
}

func TestIntegerByteMapPreservesCallerFPAndVectors(t *testing.T) {
	callee := integerByteMapFixture(t, 7, 262, 64, 65, 0, false)
	args := [4]uint64{0, 1, 0, 131}
	want, _ := integerByteMapOracle(args, 7, 262, 64, 65)
	b := []byte{2, 12, 0x7c, 4, 0x7b}
	for i := byte(0); i < 12; i++ {
		value := uint64(0x7ff0000012345678) + uint64(i)*0x100000001
		b = append(b, 0x44)
		b = binary.LittleEndian.AppendUint64(b, value)
		b = append(b, 0x21, i+4)
		binary.LittleEndian.PutUint64(want[512+int(i)*8:], value)
	}
	for i := byte(0); i < 4; i++ {
		b = append(b, 0xfd, 0x0c)
		for lane := byte(0); lane < 16; lane++ {
			b = append(b, i*32+lane+1)
			want[1024+int(i)*16+int(lane)] = i*32 + lane + 1
		}
		b = append(b, 0x21, i+16)
	}
	// A live vector operand, in addition to pinned locals, spans the call.
	b = append(b, 0x41)
	b = append(b, wasmtest.SLEB32(2048)...)
	b = append(b, 0x20, 16)
	for i := byte(0); i < 4; i++ {
		b = append(b, 0x20, i)
	}
	b = append(b, 0x10, 1, 0x1a, 0xfd, 0x0b, 0, 0)
	copy(want[2048:2064], want[1024:1040])
	for i := byte(0); i < 12; i++ {
		b = append(b, 0x41)
		b = append(b, wasmtest.SLEB32(512+int32(i)*8)...)
		b = append(b, 0x20, i+4, 0x39, 0, 0)
	}
	for i := byte(0); i < 4; i++ {
		b = append(b, 0x41)
		b = append(b, wasmtest.SLEB32(1024+int32(i)*16)...)
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
			var stats ModuleStats
			opts := CompileOptions{CompactNative: compact, Optimizations: map[string]bool{"inline": false, "stack-reg": stackReg}}
			if diagnosticsEnabled {
				opts.Stats = &stats
			}
			got, err := runScalarUnrollMemory(t, m, opts, args[:]...)
			if err != nil || !bytes.Equal(got, want) {
				t.Fatalf("compact%v stackReg%v err%v equal%v", compact, stackReg, err, bytes.Equal(got, want))
			}
			if diagnosticsEnabled && stats.Funcs[1].Peephole["integer-byte-map-vector"] != 1 {
				t.Fatalf("callee did not vectorize: %v", stats.Funcs[1].Peephole)
			}
		}
	}
}

func TestIntegerByteMapMatcherLeavesReaderAndRejectsEffects(t *testing.T) {
	m := integerByteMapFixture(t, 7, 262, 64, 65, 0, false)
	f := fn{m: m, nLocals: 8, localType: make([]machineType, 8)}
	for i := range f.localType {
		f.localType[i] = mtI32
	}
	body := m.Code[0].BodyBytes[2:] // Skip the void loop opcode/type.
	r := wasm.NewReader(body)
	if _, ok := f.inspectIntegerByteMap(r); !ok || r.Offset() != 0 {
		t.Fatalf("valid=%v offset=%d", ok, r.Offset())
	}
	for end := 0; end < bytes.Index(body, []byte{0x47, 0x0d, 0, 0x0b})+4; end++ {
		if _, ok := f.inspectIntegerByteMap(wasm.NewReader(body[:end])); ok {
			t.Fatalf("accepted truncation at %d", end)
		}
	}
	for _, tc := range []struct{ from, to []byte }{
		{[]byte{0x6e}, []byte{0x6d}},                   // Signed division.
		{[]byte{0x3a, 0, 0}, []byte{0x36, 0, 0}},       // Wider store.
		{[]byte{0x3a, 0, 0}, []byte{0x3a, 0, 1}},       // Nonzero memory offset.
		{[]byte{0x0d, 0, 0x0b}, []byte{0x0d, 1, 0x0b}}, // Another branch target.
		{[]byte{0x41, 7, 0x6e}, []byte{0x41, 0, 0x6e}}, // Trapping divisor.
		{[]byte{0x47, 0x0d}, []byte{0x46, 0x0d}},       // Different termination.
	} {
		x := bytes.Replace(body, tc.from, tc.to, 1)
		if bytes.Equal(x, body) {
			t.Fatalf("missing mutation %x", tc.from)
		}
		if _, ok := f.inspectIntegerByteMap(wasm.NewReader(x)); ok {
			t.Fatalf("accepted %x -> %x", tc.from, tc.to)
		}
	}
	large := append(append([]byte(nil), body...), make([]byte, 1<<20)...)
	if _, ok := f.inspectIntegerByteMap(wasm.NewReader(large)); !ok {
		t.Fatal("following code changed bounded match")
	}
}

func TestIntegerByteMapWorkersAndOptionOwnership(t *testing.T) {
	m := integerByteMapFixture(t, 7, 262, 64, 65, 0, false)
	m.Code = append(m.Code, m.Code[0], m.Code[0])
	m.FuncTypes = append(m.FuncTypes, m.FuncTypes[0], m.FuncTypes[0])
	var expected [2][]byte
	for _, workers := range []int{1, 3, 1} {
		for state, on := range []bool{false, true} {
			var stats ModuleStats
			opts := CompileOptions{Workers: workers, Optimizations: map[string]bool{"integer-byte-map": on}}
			if diagnosticsEnabled {
				opts.Stats = &stats
			}
			cm, err := CompileModuleWith(m, opts)
			if err != nil {
				t.Fatal(err)
			}
			image := append([]byte(nil), cm.Code...)
			if cm.CodeImage != nil {
				cm.CodeImage.Close()
			}
			if expected[state] == nil {
				expected[state] = image
			} else if !bytes.Equal(image, expected[state]) {
				t.Fatalf("workers%d on%v image changed", workers, on)
			}
			if diagnosticsEnabled {
				for _, s := range stats.Funcs {
					if (s.Peephole["integer-byte-map-vector"] != 0) != on {
						t.Fatalf("workers%d on%v stats%v", workers, on, s.Peephole)
					}
				}
			}
		}
	}
	if bytes.Equal(expected[0], expected[1]) {
		t.Fatal("option fixture did not change native image")
	}
}
