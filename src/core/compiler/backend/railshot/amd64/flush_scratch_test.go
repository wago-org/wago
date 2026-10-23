//go:build amd64

package amd64

import (
	"encoding/binary"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"math"
	"testing"
)

// Spill relocation must not borrow a register that still owns a pending load
// address. The integer predicate makes this independent of float-branch tuning.
func TestPrefixFlushPreservesPendingLoadAddresses(t *testing.T) {
	for _, wide := range []bool{false, true} {
		load, align, add, trunc := byte(0x2a), byte(2), byte(0x92), byte(0xa8)
		if wide {
			load, align, add, trunc = 0x2b, 3, 0xa0, 0xaa
		}
		body := []byte{1, 1, 0x7f}
		for i := byte(1); i <= 40; i++ {
			body = append(body, 0x20, 0, 0x41, i, 0x6c, load, align, 0)
		}
		body = append(body, 0x20, 0, 0x41, 8, 0x46, 0x04, 0x7f, 0x41, 1, 0x05, 0x41, 0, 0x0b, 0x21, 1)
		for range 39 {
			body = append(body, add)
		}
		body = append(body, trunc, 0x20, 1, 0x6a, 0x0b)
		m := mod1(t, []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, body)
		m.Memories = []wasm.MemType{{Limits: wasm.Limits{Min: 1}}}
		data := make([]byte, 512)
		for i := 1; i <= 40; i++ {
			if wide {
				binary.LittleEndian.PutUint64(data[i*8:], math.Float64bits(float64(i)))
			} else {
				binary.LittleEndian.PutUint32(data[i*8:], math.Float32bits(float32(i)))
			}
		}
		got, _, err := runMemAmd64WithOptions(t, m, CompileOptions{}, func(mem []byte) { copy(mem, data) }, 8)
		if err != nil || got != 821 {
			t.Fatalf("wide=%v: got=%d,%v; want 821", wide, got, err)
		}
	}
}
