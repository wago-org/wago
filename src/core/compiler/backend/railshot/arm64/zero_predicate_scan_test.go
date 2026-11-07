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

func zeroPredicateFixture(t testing.TB, offset uint32, initial []uint32, mutateRole bool) *wasm.Module {
	b := []byte{1, 3, 0x7f}
	c := func(v uint32) { b = append(b, 0x41); b = append(b, wasmtest.SLEB32(int32(v))...) }
	get := func(x byte) { b = append(b, 0x20, x) }
	set := func(x byte) { b = append(b, 0x21, x) }
	for i, v := range initial {
		if v == 0 {
			continue
		}
		c(uint32(i * 4))
		c(v)
		b = append(b, 0x36, 2, 0)
	}
	get(0)
	set(3)
	get(1)
	set(4)
	b = append(b, 0x03, 0x40, 0x02, 0x40)
	get(3)
	b = append(b, 0x28, 2)
	b = append(b, wasmtest.ULEB(offset)...)
	b = append(b, 0x45, 0x0d, 0)
	// Mark the following word. A stale preloaded predicate would miss this
	// store and suppress later writes and accumulator updates.
	get(3)
	c(4)
	b = append(b, 0x6b)
	get(2)
	b = append(b, 0x36, 2)
	b = append(b, wasmtest.ULEB(offset)...)
	get(5)
	get(4)
	b = append(b, 0x6a)
	set(5)
	if mutateRole {
		get(3)
		set(3)
	}
	b = append(b, 0x0b)
	get(3)
	c(0xfffffffc)
	b = append(b, 0x6a)
	set(3)
	get(4)
	c(0xffffffff)
	b = append(b, 0x6a, 0x22, 4, 0x0d, 0, 0x0b)
	for i, x := range []byte{3, 4, 5} {
		c(64000 + uint32(i)*4)
		get(x)
		b = append(b, 0x36, 2, 0)
	}
	c(0)
	b = append(b, 0x0b)
	return modMem(t, 1, []wasm.ValType{wasm.I32, wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, b)
}

func zeroPredicateOracle(initial []uint32, offset uint32, args [3]uint64) ([]byte, bool) {
	mem := make([]byte, 65536)
	for i, v := range initial {
		binary.LittleEndian.PutUint32(mem[i*4:], v)
	}
	p, count, accumulator := uint32(args[0]), uint32(args[1]), uint32(0)
	for i := 0; i < 20000; i++ {
		ea := uint64(p) + uint64(offset)
		if ea+4 > uint64(len(mem)) {
			return mem, true
		}
		if binary.LittleEndian.Uint32(mem[ea:]) != 0 {
			target := uint64(p-4) + uint64(offset)
			if target+4 > uint64(len(mem)) {
				return mem, true
			}
			binary.LittleEndian.PutUint32(mem[target:], uint32(args[2]))
			accumulator += count
		}
		p -= 4
		count--
		if count == 0 {
			for i, v := range []uint32{p, count, accumulator} {
				binary.LittleEndian.PutUint32(mem[64000+i*4:], v)
			}
			return mem, false
		}
	}
	panic("fixture neither terminated nor trapped")
}

func TestZeroPredicateScanMemoryLocalsAndTraps(t *testing.T) {
	rng := rand.New(rand.NewSource(34))
	for i := 0; i < 256; i++ {
		initial := make([]uint32, 128)
		for j := range initial {
			if rng.Intn(8) == 0 {
				initial[j] = rng.Uint32()
			}
		}
		if i%4 == 0 {
			clear(initial)
		}
		offset := []uint32{0, 1, 4, 31, 65500, 0xfffffff0}[i%6]
		p := []uint32{0, 3, 12, 16, 124, 511, 65532, 65533, 0xfffffffc}[i%9]
		counts := [...]uint32{0, 1, 4, 5, 8, 9, 10, 15, 16, 17, 31, 127}
		count := counts[i%len(counts)]
		args := [3]uint64{uint64(p) | 0x8877665500000000, uint64(count) | 0x9988776600000000, uint64(rng.Uint32())}
		want, trap := zeroPredicateOracle(initial, offset, args)
		m := zeroPredicateFixture(t, offset, initial, false)
		for _, on := range []bool{false, true} {
			got, err := runScalarUnrollMemory(t, m, CompileOptions{Optimizations: map[string]bool{"zero-predicate-scan": on}}, args[:]...)
			var trapped *rt.TrapError
			if (err != nil) != trap || err != nil && (!errors.As(err, &trapped) || trapped.Code != rt.TrapLinMemOutOfBounds) || !bytes.Equal(got, want) {
				t.Fatalf("fixture%d on%v pointer%x count%d offset%x err%v wantTrap%v equal%v", i, on, p, count, offset, err, trap, bytes.Equal(got, want))
			}
		}
	}
}

func TestZeroPredicateScanAdmission(t *testing.T) {
	requireCompilerDiagnostics(t)
	for _, mutate := range []bool{false, true} {
		for _, enabled := range []bool{false, true} {
			stats := &ModuleStats{}
			cm, err := CompileModuleWith(zeroPredicateFixture(t, 0, nil, mutate), CompileOptions{Stats: stats, Optimizations: map[string]bool{"zero-predicate-scan": enabled}})
			if err != nil {
				t.Fatal(err)
			}
			cm.CodeImage.Close()
			if got := stats.Funcs[0].Peephole["zero-predicate-scan"]; (got != 0) != (enabled && !mutate) {
				t.Fatalf("mutate%v enabled%v admission%d", mutate, enabled, got)
			}
		}
	}
}

func TestZeroPredicateScanPacketBoundaries(t *testing.T) {
	for _, offset := range []uint32{0, 1, 16, 65500, 0xfffffffc} {
		for _, pointer := range []uint32{12, 16, 28, 32, 36, 124, 65532} {
			for _, count := range []uint32{0, 4, 5, 8, 9, 12, 17} {
				for _, active := range []bool{false, true} {
					initial := make([]uint32, 128)
					if active {
						initial[3], initial[7], initial[12] = 1, 1, 1
					}
					args := [3]uint64{uint64(pointer), uint64(count), 1}
					want, trap := zeroPredicateOracle(initial, offset, args)
					m := zeroPredicateFixture(t, offset, initial, false)
					for _, enabled := range []bool{false, true} {
						got, err := runScalarUnrollMemory(t, m, CompileOptions{Optimizations: map[string]bool{"zero-predicate-scan": enabled}}, args[:]...)
						var trapped *rt.TrapError
						if (err != nil) != trap || err != nil && (!errors.As(err, &trapped) || trapped.Code != rt.TrapLinMemOutOfBounds) || !bytes.Equal(got, want) {
							t.Fatalf("pointer%x count%d offset%x active%v enabled%v err%v trap%v equal%v", pointer, count, offset, active, enabled, err, trap, bytes.Equal(got, want))
						}
					}
				}
			}
		}
	}
}
