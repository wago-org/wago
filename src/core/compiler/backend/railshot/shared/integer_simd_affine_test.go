package shared

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"testing"
)

func TestAffineUniformIntegerLanes(t *testing.T) {
	for _, wide := range []bool{false, true} {
		c, splat, extract, lanes, width := byte(0x41), byte(17), byte(27), byte(4), 4
		add, mul := uint32(174), uint32(181)
		if wide {
			c, splat, extract, lanes, width = 0x42, 18, 29, 2, 8
			add, mul = 206, 213
		}
		for lane := byte(0); lane < lanes; lane++ {
			body := []byte{0x20, 2, 0xfd, splat, c, 3, 0xfd, splat, 0xfd}
			body = append(body, []byte{byte(add&127) | 128, byte(add >> 7)}...)
			body = append(body, c, 7, 0xfd, splat, 0xfd)
			body = append(body, []byte{byte(mul&127) | 128, byte(mul >> 7)}...)
			body = append(body, 0xfd, extract, lane, 0x0b)
			r := wasm.ReaderFrom(body)
			got, ok := inspectIntegerAffineTerm(&r, true)
			if !ok || got.coefficient != 7 || got.constant != 21 || got.width != width {
				t.Fatalf("wide=%t lane=%d: %+v ok=%t", wide, lane, got, ok)
			}
			// Extraction must remain explicit; a vector is not an integer sum term.
			r = wasm.ReaderFrom(append(append([]byte{}, body[:len(body)-4]...), 0x0b))
			if _, ok := inspectIntegerAffineTerm(&r, true); ok {
				t.Fatal("accepted vector result")
			}
		}
	}
	for name, body := range map[string][]byte{
		"mixed-operands":       {0x20, 2, 0xfd, 17, 0x41, 3, 0x6a, 0x0b},
		"wrong-lane":           {0x20, 2, 0xfd, 17, 0xfd, 27, 4, 0x0b},
		"mixed-lane-width":     {0x20, 2, 0xfd, 17, 0xfd, 29, 0, 0x0b},
		"float-convert-vector": {0x20, 2, 0xfd, 17, 0xb3, 0x8c, 0xfc, 1, 0x0b},
		"nonlinear":            {0x20, 2, 0xfd, 17, 0x20, 2, 0xfd, 17, 0xfd, 0xb5, 1, 0xfd, 27, 0, 0x0b},
	} {
		r := wasm.ReaderFrom(body)
		if _, ok := inspectIntegerAffineTerm(&r, true); ok {
			t.Fatalf("accepted %s", name)
		}
	}
}

func TestAffineUniformLaneSubtraction(t *testing.T) {
	for _, wide := range []bool{false, true} {
		c, splat, extract, sub := byte(0x41), byte(17), byte(27), uint32(177)
		expected := uint64(0xfffffffd)
		if wide {
			c, splat, extract, sub = 0x42, 18, 29, 209
			expected = ^uint64(2)
		}
		body := []byte{0x20, 2, 0xfd, splat, c, 3, 0xfd, splat, 0xfd, byte(sub&127) | 128, byte(sub >> 7), 0xfd, extract, 0, 0x0b}
		r := wasm.ReaderFrom(body)
		got, ok := inspectIntegerAffineTerm(&r, true)
		if !ok || got.coefficient != 1 || got.constant != expected {
			t.Fatalf("wide=%t: %+v ok=%t", wide, got, ok)
		}
	}
}
