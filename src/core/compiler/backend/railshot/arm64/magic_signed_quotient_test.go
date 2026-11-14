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

func signedQuotientFixture(t testing.TB, d int32, mode int) *wasm.Module {
	body := []byte{1, 1, 0x7f, 0x03, 0x7f}
	c := func(x int32) { body = append(body, 0x41); body = append(body, wasmtest.SLEB32(x)...) }
	if mode == 1 || mode == 2 {
		body = append(body, 0x20, 0)
	}
	body = append(body, 0x20, 0)
	if mode == 3 {
		c(7)
		body = append(body, 0x6c)
		c(-19)
		body = append(body, 0x6a)
	}
	c(d)
	body = append(body, 0x6d)
	switch mode {
	case 1:
		body = append(body, 0x6a)
	case 2:
		body = append(body, 0x21, 0, 0x20, 0, 0x6a)
	case 4:
		body = append(body, 0x21, 1, 0x20, 0, 0x20, 1, 0x6a)
	}
	c(0)
	body = append(body, 0x0d, 0, 0x0b, 0x0b)
	return mod1(t, []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, body)
}

func TestDirectSignedQuotientSourcesAssignmentsAndWidths(t *testing.T) {
	divisors := []int32{3, -3, 5, -5, 7, -7, 10, -10, 17, -17, 19, -19, 31, -31, 97, -97, 1000, -1000, 65537, -65537, 0x7fffffff, -0x7fffffff, 0x40000001, -0x40000001}
	values := []uint32{0, 1, 2, 3, 7, 10, 0x7fffffff, 0x80000000, 0x80000001, 0xfffffffe, 0xffffffff}
	state := uint32(0x91e10da5)
	for range 32 {
		state ^= state << 13
		state ^= state >> 17
		state ^= state << 5
		values = append(values, state)
	}
	for _, d := range divisors {
		for mode := 0; mode < 5; mode++ {
			m := signedQuotientFixture(t, d, mode)
			for _, on := range []bool{false, true} {
				for _, workers := range []int{1, 4} {
					for _, n := range values {
						dividend := n
						if mode == 3 {
							dividend = dividend*7 - 19
						}
						want := uint32(int32(dividend) / d)
						if mode == 1 || mode == 2 || mode == 4 {
							want += n
						}
						stats := &ModuleStats{}
						opts := CompileOptions{Workers: workers, Optimizations: map[string]bool{"signed-quotient-direct": on, "value-facts": false, "interval-region-pins": false}}
						if diagnosticsEnabled {
							opts.Stats = stats
						}
						got, err := runArm64WrapperWithOptions(t, m, opts, uint64(n)|0xfeedface00000000)
						if err != nil || got != uint64(want) {
							t.Fatalf("d%d mode%d n%x on%v workers%d got%x err%v want%x", d, mode, n, on, workers, got, err, want)
						}
						if diagnosticsEnabled && (stats.Funcs[0].Peephole["signed-quotient-direct"] != 0) != on {
							t.Fatalf("missing toggle d%d mode%d on%v", d, mode, on)
						}
					}
				}
			}
		}
	}
}

func TestDirectSignedQuotientMagicAndRemainderGates(t *testing.T) {
	requireCompilerDiagnostics(t)
	for _, op := range []divOp{{"signed", 0x6d, true, false}, {"remainder", 0x6f, true, true}, {"unsigned", 0x6e, false, false}} {
		for _, wide := range []bool{false, true} {
			for _, magic := range []bool{false, true} {
				actual := op
				if wide {
					actual.opcode = map[byte]byte{0x6d: 0x7f, 0x6f: 0x81, 0x6e: 0x80}[op.opcode]
				}
				stats := &ModuleStats{}
				cm, err := CompileModuleWith(divConstMod(t, wide, actual, 10), CompileOptions{Stats: stats, Optimizations: map[string]bool{"magic-div": magic}})
				if err != nil {
					t.Fatal(err)
				}
				cm.CodeImage.Close()
				if got := stats.Funcs[0].Peephole["signed-quotient-direct"]; (got != 0) != (magic && !wide && op.opcode == 0x6d) {
					t.Fatalf("op%s wide%v magic%v got%d", op.name, wide, magic, got)
				}
			}
		}
	}
}

func TestDirectSignedQuotientLoadedDividendTrapOrder(t *testing.T) {
	for _, d := range []int32{3, -3, 7, -7, 10, -10} {
		b := []byte{0, 0x41, 0, 0x20, 0, 0x36, 2, 0, 0x41, 8, 0x20, 0, 0x20, 1, 0x28, 2, 0, 0x41, 7, 0x6c, 0x41, 0x6d, 0x6a, 0x41}
		b = append(b, wasmtest.SLEB32(d)...)
		b = append(b, 0x6d, 0x21, 0, 0x36, 2, 0, 0x41, 4, 0x20, 0, 0x36, 2, 0, 0x41, 0, 0x0b)
		m := modMem(t, 1, []wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, b)
		for _, n := range []uint32{0, 1, 0x7fffffff, 0x80000000, 0xffffffff} {
			for _, addr := range []uint32{0, 1, 4, 65532, 65533} {
				want := make([]byte, 65536)
				binary.LittleEndian.PutUint32(want, n)
				trap := uint64(addr)+4 > 65536
				if !trap {
					value := binary.LittleEndian.Uint32(want[addr:])*7 - 19
					q := uint32(int32(value) / d)
					binary.LittleEndian.PutUint32(want[8:], n)
					binary.LittleEndian.PutUint32(want[4:], q)
				}
				for _, on := range []bool{false, true} {
					got, err := runScalarUnrollMemory(t, m, CompileOptions{Optimizations: map[string]bool{"signed-quotient-direct": on}}, uint64(n)|0xfeedface00000000, uint64(addr)|0x9876543200000000)
					var tr *rt.TrapError
					if (err != nil) != trap || err != nil && (!errors.As(err, &tr) || tr.Code != rt.TrapLinMemOutOfBounds) || !bytes.Equal(got, want) {
						t.Fatalf("d%d n%x addr%d on%v err%v trap%v equal%v", d, n, addr, on, err, trap, bytes.Equal(got, want))
					}
				}
			}
		}
	}
}
