// Package ruleguide supplies bounded, typed instruction-selection recipes for tests.
// No production package imports it.
package ruleguide

import (
	"encoding/binary"
	"fmt"
	"math/bits"
	"math/rand"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

const Seed int64 = 809
const Budget = 96

// Recipe holds operand choices, not a prediction copied from compiler output.
// Every expression has a fixed typed operand/result stack. Context inserts it
// only where that result type is required, so sampling cannot create underflow.
type Recipe struct {
	Family, Context string
	Mask            int64
	Rotate          int32
	Second          byte
	Variable        bool
	Count           int32
}

func (r Recipe) Rule() string {
	switch r.Family {
	case "bswap":
		return "i32-bswap-tee"
	case "swar":
		return "swar-mask-test"
	default:
		return "simd-shift-imm"
	}
}
func (r Recipe) Selected() bool {
	switch r.Family {
	case "bswap":
		return r.Mask == 0x00ff00ff && r.Rotate == 8 && r.Second == 0
	case "swar":
		return r.Mask != 0 && !r.Variable
	default:
		return !r.Variable
	}
}
func (r Recipe) Name() string {
	return fmt.Sprintf("%s/%s/m%x-r%d-l%d-v%t-c%d", r.Family, r.Context, uint64(r.Mask), r.Rotate, r.Second, r.Variable, r.Count)
}

// Cases breaks one premise at a time. Generic below uses the same operand
// alphabet and types, but can break several premises at once.
func Cases() []Recipe {
	var out []Recipe
	for _, c := range []string{"plain", "local-update", "branch-result", "live-call"} {
		b := Recipe{Family: "bswap", Context: c, Mask: 0x00ff00ff, Rotate: 8}
		out = append(out, b)
		m := b
		m.Mask ^= 1
		out = append(out, m)
		m = b
		m.Rotate = 9
		out = append(out, m)
		m = b
		m.Second = 1
		out = append(out, m)
		s := Recipe{Family: "swar", Context: c, Mask: -9187201950435737472}
		out = append(out, s)
		m = s
		m.Mask = 0
		out = append(out, m)
		m = s
		m.Variable = true
		out = append(out, m)
		v := Recipe{Family: "simd", Context: c, Count: 7}
		out = append(out, v)
		v.Variable = true
		out = append(out, v)
	}
	return out
}

// Generate is a typed-template sampler, not a general Wasm fuzzer. Both modes
// have the same 96-case budget, root shapes, contexts, operand alphabet and RNG.
// Directed mode constrains rule premises; generic mode samples them independently.
// Generic CAN reach every rule and context, including the exact byte-swap rule.
func Generate(seed int64, directed bool) []Recipe {
	rng := rand.New(rand.NewSource(seed))
	out := make([]Recipe, Budget)
	families := []string{"bswap", "swar", "simd"}
	contexts := []string{"plain", "local-update", "branch-result", "live-call"}
	counts := []int32{0, 1, 7, 31, 32, 33, -1}
	for i := range out {
		r := Recipe{Family: families[i%3], Context: contexts[rng.Intn(4)], Mask: 0x00ff00ff, Rotate: 8, Count: counts[rng.Intn(len(counts))]}
		switch r.Family {
		case "bswap":
			r.Mask ^= int64(rng.Intn(2))
			r.Rotate += int32(rng.Intn(2))
			r.Second = byte(rng.Intn(2))
			if directed {
				r.Mask = 0x00ff00ff
				r.Rotate = 8
				r.Second = 0
			}
		case "swar":
			r.Mask = []int64{0, 0x80, -9187201950435737472}[rng.Intn(3)]
			r.Variable = rng.Intn(2) != 0
			if directed {
				if r.Mask == 0 {
					r.Mask = 0x80
				}
				r.Variable = false
			}
		case "simd":
			r.Variable = rng.Intn(2) != 0
			if directed {
				r.Variable = false
			}
		}
		out[i] = r
	}
	return out
}

func (r Recipe) Wasm() []byte {
	var params []wasm.ValType
	result := wasm.I32
	var expr []byte
	switch r.Family {
	case "bswap":
		params = []wasm.ValType{wasm.I32, wasm.I32}
		expr = []byte{0x20, 0, 0x22, 0, 0x41}
		expr = append(expr, wasmtest.SLEB32(int32(r.Mask))...)
		expr = append(expr, 0x71, 0x41)
		expr = append(expr, wasmtest.SLEB32(r.Rotate)...)
		expr = append(expr, 0x78, 0x20, r.Second, 0x41, 24, 0x78, 0x41)
		expr = append(expr, wasmtest.SLEB32(0x00ff00ff)...)
		expr = append(expr, 0x71, 0x72)
	case "swar":
		params = []wasm.ValType{wasm.I64, wasm.I64}
		expr = []byte{0x20, 0}
		if r.Variable {
			expr = append(expr, 0x20, 1)
		} else {
			expr = append(expr, 0x42)
			expr = append(expr, wasmtest.SLEB64(r.Mask)...)
		}
		expr = append(expr, 0x83, 0x50)
	case "simd":
		params = []wasm.ValType{wasm.V128, wasm.I32}
		result = wasm.V128
		expr = []byte{0x20, 0}
		if r.Variable {
			expr = append(expr, 0x20, 1)
		} else {
			expr = append(expr, 0x41)
			expr = append(expr, wasmtest.SLEB32(r.Count)...)
		}
		expr = append(expr, 0xfd, 0xad, 1) // i32x4.shr_u: all four lanes are observable.
	default:
		panic("unknown family")
	}
	body := []byte{0}
	switch r.Context {
	case "plain":
		body = append(body, expr...)
	case "local-update":
		body = []byte{1, 1, wasm.MustEncodeValType(result)}
		body = append(body, expr...)
		body = append(body, 0x21, byte(len(params)))
		// Change the source local after saving the result. A retained local alias
		// must not change the result when the original parameter is overwritten.
		switch params[0] {
		case wasm.I32:
			body = append(body, 0x41, 0)
		case wasm.I64:
			body = append(body, 0x42, 0)
		default:
			body = append(body, 0xfd, 12)
			body = append(body, make([]byte, 16)...)
		}
		body = append(body, 0x21, 0, 0x20, byte(len(params)))
	case "branch-result":
		body = append(body, 0x02, wasm.MustEncodeValType(result))
		body = append(body, expr...)
		body = append(body, 0x0c, 0, 0x00, 0x0b) // br 0; unreachable; end
	case "live-call":
		body = append(body, expr...)
	default:
		panic("unknown context")
	}
	if r.Context == "live-call" {
		body = append(body, 0x10, 1)
	} else {
		body = append(body, 0x41, 37)
	}
	body = append(body, 0x0b)
	code := append(wasmtest.ULEB(uint32(len(body))), body...)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(params, []wasm.ValType{result, wasm.I32}), wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, []byte{2, 0, 1}),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(code, wasmtest.Code([]byte{0x41, 37, 0x0b}))),
	)
}

// Inputs have fixed ABI slots, including two adjacent slots for v128. Edge
// vectors have distinct lanes. Random inputs use a private deterministic RNG.
func Inputs() [][3]uint64 {
	out := [][3]uint64{{0, 0, 0}, {1, 0x80000000, 1}, {0xffffffffffffffff, 0x0123456789abcdef, 31}, {0x800000007fffffff, 0x1234567887654321, 32}, {0x7f7f7f7f7f7f7f7f, 0x8080808080808080, 33}, {0x8080808080808080, 0x7f7f7f7f7f7f7f7f, 0xffffffff}, {0x0123456789abcdef, 0xfedcba9876543210, 7}}
	rng := rand.New(rand.NewSource(Seed))
	for range 16 {
		out = append(out, [3]uint64{rng.Uint64(), rng.Uint64(), uint64(rng.Uint32())})
	}
	return out
}

// Model implements Wasm bit-vector semantics independently of the compiler.
// It returns every semantic result byte; unused upper i32 ABI bits are omitted.
func (r Recipe) Model(in [3]uint64) []byte {
	var out []byte
	switch r.Family {
	case "bswap":
		second := uint32(in[0])
		if r.Second != 0 {
			second = uint32(in[1])
		}
		x := bits.RotateLeft32(uint32(in[0])&uint32(r.Mask), -int(r.Rotate&31)) | bits.RotateLeft32(second, -24)&0x00ff00ff
		out = binary.LittleEndian.AppendUint32(out, x)
	case "swar":
		mask := uint64(r.Mask)
		if r.Variable {
			mask = in[1]
		}
		x := uint32(0)
		if in[0]&mask == 0 {
			x = 1
		}
		out = binary.LittleEndian.AppendUint32(out, x)
	case "simd":
		count := uint32(r.Count)
		if r.Variable {
			count = uint32(in[2])
		}
		for _, v := range in[:2] {
			for _, lane := range []uint32{uint32(v), uint32(v >> 32)} {
				out = binary.LittleEndian.AppendUint32(out, lane>>(count&31))
			}
		}
	}
	return binary.LittleEndian.AppendUint32(out, 37)
}
