package shared

import (
	"bytes"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"math"
	"math/rand"
	"testing"
)

func TestBooleanFloatIntegerRounding(t *testing.T) {
	values := []uint32{0, 1, 0x7fffffff, 0x80000000, 0xffffffff, 16777217, 16777219, 33554431}
	random := rand.New(rand.NewSource(9451))
	for i := 0; i < 1000; i++ {
		values = append(values, random.Uint32())
	}
	for _, v := range values {
		for _, signed := range []bool{false, true} {
			for _, wide := range []bool{false, true} {
				number := float64(v)
				if signed {
					number = float64(int32(v))
				}
				want := math.Float64bits(number)
				if !wide {
					want = uint64(math.Float32bits(float32(number)))
				}
				if got := BooleanFloatBits(v, wide, signed); got != want {
					t.Fatalf("v=%x signed=%t wide=%t got=%x want=%x", v, signed, wide, got, want)
				}
			}
		}
	}
}

func TestBooleanFloatProofRejectsNonlocalAndEffectfulExpressions(t *testing.T) {
	body := []byte{0x41, 1, 0x20, 3, 0x41, 1, 0x71, 0x41, 1, 0x74, 0x6a, 0xb3}
	r := wasm.ReaderFrom(body)
	p, ok := InspectBooleanFloat(&r)
	if !ok || p.Values != ([2]uint32{1, 3}) || p.Variable != 3 || p.Wide || p.Signed || r.Offset() != 0 {
		t.Fatalf("plan=%+v ok=%t cursor=%d", p, ok, r.Offset())
	}
	for cut := 0; cut < len(body); cut++ {
		r = wasm.ReaderFrom(body[:cut])
		if _, ok := InspectBooleanFloat(&r); ok || r.Offset() != 0 {
			t.Fatalf("accepted cut %d", cut)
		}
	}
	for name, b := range map[string][]byte{
		"mask3":       bytes.Replace(body, []byte{0x41, 1, 0x71}, []byte{0x41, 3, 0x71}, 1),
		"unmasked":    bytes.Replace(body, []byte{0x41, 1, 0x71}, nil, 1),
		"global":      bytes.Replace(body, []byte{0x20, 3}, []byte{0x23, 0}, 1),
		"memory":      bytes.Replace(body, []byte{0x20, 3}, []byte{0x41, 0, 0x28, 2, 0}, 1),
		"call":        bytes.Replace(body, []byte{0x71}, []byte{0x71, 0x10, 0}, 1),
		"write":       bytes.Replace(body, []byte{0x71}, []byte{0x71, 0x22, 3}, 1),
		"division":    bytes.Replace(body, []byte{0x74}, []byte{0x6e}, 1),
		"other-local": bytes.Replace(body, []byte{0x41, 1, 0x74}, []byte{0x20, 4, 0x41, 1, 0x71, 0x74}, 1),
	} {
		r = wasm.ReaderFrom(b)
		if _, ok := InspectBooleanFloat(&r); ok || r.Offset() != 0 {
			t.Fatalf("accepted %s", name)
		}
	}
}
