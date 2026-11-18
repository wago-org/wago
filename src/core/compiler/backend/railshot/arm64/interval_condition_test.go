//go:build (linux || darwin) && arm64

package arm64

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func regionalConditionFixture(t testing.TB, mode int, calling bool) *wasm.Module {
	b := []byte{1, 80, 0x7f, 0x3f, 0, 0x1a, 0x02, 0x40, 0x20, 0, 0x45, 0x0d, 0, 0x03, 0x40}
	for x := byte(3); x <= 72; x++ {
		b = append(b, 0x20, x, 0x41)
		b = append(b, wasmtest.SLEB32(int32(x))...)
		b = append(b, 0x6a, 0x21, x)
	}
	sum := func(local byte) {
		b = append(b, 0x41, 0)
		for x := byte(3); x <= 72; x++ {
			b = append(b, 0x20, x, 0x6a)
		}
		b = append(b, 0x21, local)
	}
	sum(75)
	// A pure condition on a forward diamond; every regional local is still
	// read after the join and after the backedge.
	b = append(b, 0x20, 1, 0x41, 1, 0x46, 0x04, 0x40,
		0x20, 3, 0x41, 7, 0x6a, 0x21, 3,
		0x05, 0x20, 4, 0x41, 11, 0x6a, 0x21, 4, 0x0b)
	sum(76)
	b = append(b,
		0x20, 0, 0x41, 1, 0x6b, 0x21, 0,
		0x20, 3, // A live prefix is discarded on the taken edge, retained otherwise.
		0x20, 0)
	switch mode {
	case 0:
		b = append(b, 0x45, 0x45)
	case 1:
		b = append(b, 0x41, 0, 0x47)
	case 2:
		b = append(b, 0x41, 0, 0x4b)
	case 3:
		b = append(b, 0x41)
		b = append(b, wasmtest.SLEB32(65535)...)
		b = append(b, 0x71)
	}
	b = append(b, 0x0d, 0, 0x21, 74, 0x0b, 0x0b, 0x20, 74, 0x20, 75, 0x6a, 0x20, 76, 0x6a)
	for x := byte(3); x <= 72; x++ {
		b = append(b, 0x20, x, 0x6a)
	}
	if calling {
		b = append(b, 0x10, 1, 0x6a)
	}
	b = append(b, 0x0b)
	m := modFuncs(t, funcDef{params: []wasm.ValType{wasm.I32, wasm.I32}, results: []wasm.ValType{wasm.I32}, body: b},
		funcDef{results: []wasm.ValType{wasm.I32}, body: []byte{1, 1, 0x7f, 0x41, 1, 0x21, 0, 0x41, 17, 0x0b}})
	m.Memories = []wasm.MemType{{Limits: wasm.Limits{Min: 1}}}
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestRegionalConditionMemoryAndTrapOrder(t *testing.T) {
	for _, division := range []bool{false, true} {
		b := []byte{1, 80, 0x7f}
		for x := byte(3); x <= 72; x++ {
			b = append(b, 0x20, 2, 0x41)
			b = append(b, wasmtest.SLEB32(int32(x))...)
			b = append(b, 0x6a, 0x21, x)
		}
		b = append(b, 0x41, 0)
		for x := byte(3); x <= 72; x++ {
			b = append(b, 0x20, x, 0x6a)
		}
		b = append(b, 0x21, 75,
			0x41, 0, 0x41, 42, 0x3a, 0, 0,
			0x20, 0, 0x28, 0, 0) // A potentially trapping live prefix precedes the condition.
		if division {
			b = append(b, 0x41, 42, 0x20, 1, 0x6e)
		} else {
			b = append(b, 0x20, 1)
		}
		b = append(b, 0x41, 1, 0x46, 0x04, 0x40,
			0x20, 3, 0x41, 1, 0x6a, 0x21, 3,
			0x05, 0x20, 4, 0x41, 2, 0x6a, 0x21, 4, 0x0b, 0x21, 74,
			0x41, 4, 0x20, 74, 0x36, 0, 0,
			0x41, 8, 0x20, 3, 0x36, 0, 0,
			0x41, 12, 0x20, 4, 0x36, 0, 0,
			0x41, 0, 0x0b)
		m := modMem(t, 1, []wasm.ValType{wasm.I32, wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, b)
		for _, on := range []bool{false, true} {
			for _, address := range []uint64{0, 1, 65532, 65535, 0xffffffff, 0x8877665500000000} {
				for _, divisor := range []uint64{0, 1, 7, 42, 0xffffffff} {
					for _, seed := range []uint64{0, 0xffffffff, 0x8877665500000001} {
						want := make([]byte, 65536)
						want[0] = 42
						trap := ""
						if uint64(uint32(address))+4 > 65536 {
							trap = "out of bounds"
						} else if division && uint32(divisor) == 0 {
							trap = "division by zero"
						}
						if trap == "" {
							value := binary.LittleEndian.Uint32(want[uint32(address):])
							condition := uint32(divisor)
							if division {
								condition = 42 / condition
							}
							x, y := uint32(seed)+3, uint32(seed)+4
							if condition == 1 {
								x++
							} else {
								y += 2
							}
							binary.LittleEndian.PutUint32(want[4:], value)
							binary.LittleEndian.PutUint32(want[8:], x)
							binary.LittleEndian.PutUint32(want[12:], y)
						}
						got, err := runScalarUnrollMemory(t, m, CompileOptions{Optimizations: map[string]bool{"regional-condition-fusion": on}}, address, divisor, seed)
						if (err != nil) != (trap != "") || !bytes.Equal(got, want) || (err != nil && !strings.Contains(err.Error(), trap)) {
							t.Fatalf("division%v on%v address%x divisor%x seed%x err%v wantTrap%q equal%v", division, on, address, divisor, seed, err, trap, bytes.Equal(got, want))
						}
					}
				}
			}
		}
	}
}

func TestRegionalConditionPreservesLocalsPrefixAndCalls(t *testing.T) {
	for mode := 0; mode < 4; mode++ {
		for _, calling := range []bool{false, true} {
			m := regionalConditionFixture(t, mode, calling)
			for _, on := range []bool{false, true} {
				for _, compact := range []bool{false, true} {
					for _, n := range []uint64{0, 1, 2, 7, 31, 0x8877665500000007} {
						for _, selector := range []uint64{0, 1, 2, 0x8877665500000001} {
							var stats ModuleStats
							opts := CompileOptions{CompactNative: compact, Optimizations: map[string]bool{"regional-condition-fusion": on, "inline": false}}
							if diagnosticsEnabled {
								opts.Stats = &stats
							}
							got, err := runArm64WrapperWithOptions(t, m, opts, n, selector)
							var locals [82]uint32
							for trip := uint32(0); trip < uint32(n); trip++ {
								for x := 3; x <= 72; x++ {
									locals[x] += uint32(x)
								}
								locals[75] = 0
								for x := 3; x <= 72; x++ {
									locals[75] += locals[x]
								}
								if uint32(selector) == 1 {
									locals[3] += 7
								} else {
									locals[4] += 11
								}
								locals[76] = 0
								for x := 3; x <= 72; x++ {
									locals[76] += locals[x]
								}
								locals[74] = locals[3]
							}
							want := locals[74] + locals[75] + locals[76]
							for x := 3; x <= 72; x++ {
								want += locals[x]
							}
							if calling {
								want += 17
							}
							if err != nil || uint32(got) != want {
								t.Fatalf("mode%d calling%v on%v compact%v n%x selector%x got%d want%d err%v", mode, calling, on, compact, n, selector, got, want, err)
							}
							if diagnosticsEnabled && (stats.Funcs[0].Peephole["regional-condition-fusion"] != 0) != on {
								t.Fatalf("mode%d calling%v on%v compact%v stats%v", mode, calling, on, compact, stats.Funcs[0].Peephole)
							}
						}
					}
				}
			}
		}
	}
}
