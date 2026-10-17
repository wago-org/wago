//go:build (linux || darwin || windows) && amd64

package amd64

import (
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"testing"
	"unsafe"
)

func integerReductionProductFixture(t *testing.T) *wasm.Module {
	// Four sum parameters use arbitrary initial bits. x and y span the whole
	// u32 range, so their full unsigned product requires both halves of i64.
	body := []byte{2, 1, 0x7f, 2, 0x7e,
		0x02, 0x40, 0x20, 0, 0x45, 0x0d, 0, 0x03, 0x40,
		0x20, 1, 0xad, 0x22, 7, 0x20, 2, 0x7c, 0x21, 2,
		0x20, 7, 0x20, 7, 0x7e, 0x20, 3, 0x7c, 0x21, 3,
		0x20, 1, 0x41, 1, 0x6a, 0x22, 6, 0xad, 0x22, 8, 0x20, 4, 0x7c, 0x21, 4,
		0x20, 7, 0x20, 8, 0x7e, 0x20, 5, 0x7c, 0x21, 5,
		0x20, 1, 0x41, 7, 0x6a, 0x21, 1,
		0x20, 0, 0x41, 0x7f, 0x6a, 0x22, 0, 0x0d, 0, 0x0b, 0x0b,
		0x20, 2, 0x20, 3, 0x85, 0x20, 4, 0x85, 0x20, 5, 0x85,
		0x20, 1, 0xad, 0x85, 0x20, 6, 0xad, 0x85, 0x20, 7, 0x85, 0x20, 8, 0x85, 0x0b}
	return mod1(t, []wasm.ValType{wasm.I32, wasm.I32, wasm.I64, wasm.I64, wasm.I64, wasm.I64}, []wasm.ValType{wasm.I64}, body)
}

func TestIntegerReductionFourProductsExact(t *testing.T) {
	saved := integerReductionLoopEnabled
	defer func() { integerReductionLoopEnabled = saved }()
	m := integerReductionProductFixture(t)
	for _, features := range []shared.AMD64Features{0, shared.AMD64SSE41, shared.AMD64ModernBaseline} {
		for _, count := range []uint32{0, 1, 2, 3, 4, 7, 15, 16, 17, 31} {
			for _, start := range []uint32{0, 0x7ffffffe, 0xfffffff0, 0xffffffff} {
				var sums = [4]uint64{^uint64(0) - 17, 1 << 63, 0x0123456789abcdef, ^uint64(0)}
				x := start
				var lastX, lastY uint64
				for i := uint32(0); i < count; i++ {
					lastX, lastY = uint64(x), uint64(x+1)
					sums[0] += lastX
					sums[1] += lastX * lastX
					sums[2] += lastY
					sums[3] += lastX * lastY
					x += 7
				}
				want := sums[0] ^ sums[1] ^ sums[2] ^ sums[3] ^ uint64(x) ^ lastY ^ lastX ^ lastY
				for _, on := range []bool{false, true} {
					integerReductionLoopEnabled = on
					var stats ModuleStats
					got, _, err := runMemAmd64WithOptions(t, m, CompileOptions{AMD64FeaturesSet: true, AMD64Features: features, Stats: optionalTestStats(&stats)}, nil, uint64(count)|0xdeadbeef00000000, uint64(start)|0xfeedface00000000, ^uint64(0)-17, 1<<63, 0x0123456789abcdef, ^uint64(0))
					if err != nil || got != want {
						t.Fatal(features, count, start, on, got, want, err)
					}
					if on && diagnosticsEnabled && features.Has(shared.AMD64SSE41) && stats.Funcs[0].Peephole["integer-reduction-loop"] != 1 {
						t.Fatal("no product vector path", stats.Funcs[0].Peephole)
					}
					if on && diagnosticsEnabled && !features.Has(shared.AMD64SSE41) && stats.Funcs[0].Peephole["integer-reduction-loop"] != 0 {
						t.Fatal("CPU gate failed")
					}
				}
			}
		}
	}
}

func TestIntegerReductionPlannerRejectsEffectsDependenciesAndBounds(t *testing.T) {
	// sum += zero_extend(index), index += 1, --count, br_if 0.
	body := []byte{0x20, 2, 0x20, 1, 0xad, 0x7c, 0x21, 2, 0x20, 1, 0x41, 1, 0x6a, 0x21, 1, 0x20, 0, 0x41, 0x7f, 0x6a, 0x22, 0, 0x0d, 0, 0x0b}
	types := []machineType{mtI32, mtI32, mtI64}
	var p integerReductionPlan
	if !inspectIntegerReductionLoop(wasm.ReaderFrom(body), types, &p) {
		t.Fatal("baseline plan rejected")
	}
	t.Logf("fixed planner bytes=%d; nodes=%d; locals=%d; XMM=%d", unsafe.Sizeof(p), p.nodeN, p.localN, p.registers)
	cases := [][]byte{
		append([]byte{0x01}, body...),                         // Unmodeled operation.
		append([]byte{0x20, 1, 0x24, 0}, body...),             // Global effect.
		append([]byte{0x20, 1, 0x28, 0, 0, 0x1a}, body...),    // Memory read/trap.
		append([]byte{0x10, 0}, body...),                      // Call.
		append([]byte{0x20, 1, 0x41, 0, 0x6e, 0x1a}, body...), // Trapping divide.
		append([]byte{0x02, 0x40}, body...),                   // Nested control.
		append([]byte(nil), body[:len(body)-1]...),            // Incomplete packet.
	}
	dependent := append([]byte{0x20, 2, 0x20, 2, 0x7c, 0x21, 2}, body[8:]...) // sum += sum.
	cases = append(cases, dependent)
	oversize := make([]byte, integerReductionMaxBytes+1)
	for i := range oversize {
		oversize[i] = 0x20
	}
	cases = append(cases, oversize)
	for i, b := range cases {
		if inspectIntegerReductionLoop(wasm.ReaderFrom(b), types, &p) {
			t.Fatal("unsafe packet admitted", i)
		}
	}
	// The same fixture with a signed extension cannot authorize an unsigned
	// PMULUDQ product. General signed or full i64 multiplication stays scalar.
	signedProduct := []byte{0x20, 2, 0x20, 1, 0xac, 0x20, 1, 0xac, 0x7e, 0x7c, 0x21, 2}
	signedProduct = append(signedProduct, body[8:]...)
	if inspectIntegerReductionLoop(wasm.ReaderFrom(signedProduct), types, &p) {
		t.Fatal("unproved signed product admitted")
	}
	// More than four independent reductions is outside the register contract.
	var five []byte
	for local := byte(2); local < 7; local++ {
		five = append(five, 0x20, local, 0x20, 1, 0xad, 0x7c, 0x21, local)
	}
	five = append(five, body[8:]...)
	if inspectIntegerReductionLoop(wasm.ReaderFrom(five), []machineType{mtI32, mtI32, mtI64, mtI64, mtI64, mtI64, mtI64}, &p) {
		t.Fatal("five reductions admitted")
	}
}
