//go:build linux && amd64

package amd64

import (
	"encoding/binary"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"testing"
)

func TestVectorRegionWritesBorrowsAndBoundaries(t *testing.T) {
	requireCompilerDiagnostics(t)
	saved := vectorRegionsEnabled
	defer func() { vectorRegionsEnabled = saved }()
	for _, tee := range []bool{false, true} {
		for _, boundary := range []bool{false, true} {
			body := []byte{2, 16, 0x7b, 1, 0x7f}
			for round := 0; round < 2; round++ {
				for i := 0; i < 16; i++ {
					body = append(body, 0xfd, 0x0c)
					for lane := 0; lane < 4; lane++ {
						var bits [4]byte
						binary.LittleEndian.PutUint32(bits[:], uint32(round*16+i+lane*100))
						body = append(body, bits[:]...)
					}
					set := byte(0x21)
					if tee {
						set = 0x22
					}
					body = append(body, set, byte(i))
					if tee {
						body = append(body, 0x1a)
					}
					body = append(body, 0x20, byte(i), 0xfd, 0x1b, 3, 0x20, 16, 0x6a, 0x21, 16)
					if boundary && i%4 == 3 {
						body = append(body, 0x41, 1, 0x04, 0x40, 0x01, 0x0b)
					}
				}
			}
			body = append(body, 0x20, 16, 0x0b)
			m := modFuncs(t, funcDef{results: []wasm.ValType{wasm.I32}, body: body})
			for _, on := range []bool{false, true} {
				vectorRegionsEnabled = on
				stats := compileWithStats(t, m, false).Funcs[0]
				if on && stats.Peephole["vector-region-admit"] == 0 {
					t.Fatalf("cache not exercised: %v", stats.Peephole)
				}
				if got := runAmd64(t, m); got != 10096 {
					t.Fatalf("tee=%v boundary=%v on=%v got %d want 10096", tee, boundary, on, got)
				}
			}
		}
	}
}

func TestVectorRegionCallWriteback(t *testing.T) {
	requireCompilerDiagnostics(t)
	saved := vectorRegionsEnabled
	defer func() { vectorRegionsEnabled = saved }()
	body := []byte{2, 16, 0x7b, 1, 0x7f}
	for i := 0; i < 16; i++ {
		body = append(body, 0xfd, 0x0c)
		for lane := 0; lane < 4; lane++ {
			var bits [4]byte
			binary.LittleEndian.PutUint32(bits[:], uint32(i+lane*100))
			body = append(body, bits[:]...)
		}
		body = append(body, 0x21, byte(i), 0x20, byte(i), 0xfd, 0x1b, 3, 0x20, 16, 0x6a, 0x21, 16)
	}
	// Call barriers must write back all cache homes before private ABI clobbers.
	body = append(body, 0x10, 1, 0x1a, 0x20, 15, 0xfd, 0x1b, 3, 0x20, 16, 0x6a, 0x0b)
	helper := []byte{0}
	for i := 0; i < 512; i++ {
		helper = append(helper, 0x01)
	}
	helper = append(helper, 0x41, 7, 0x0b)
	m := modFuncs(t, funcDef{results: []wasm.ValType{wasm.I32}, body: body}, funcDef{results: []wasm.ValType{wasm.I32}, body: helper})
	for _, on := range []bool{false, true} {
		vectorRegionsEnabled = on
		stats := compileWithStats(t, m, false).Funcs[0]
		if on && stats.Peephole["vector-region-admit"] == 0 {
			t.Fatalf("call cache not exercised: %v", stats.Peephole)
		}
		if got := runAmd64(t, m); got != 5235 {
			t.Fatalf("on=%v result %d want 5235", on, got)
		}
	}
}

func TestVectorRegionLiveValuesAndOverwrite(t *testing.T) {
	requireCompilerDiagnostics(t)
	saved := vectorRegionsEnabled
	defer func() { vectorRegionsEnabled = saved }()
	body := []byte{2, 16, 0x7b, 1, 0x7f}
	var vectors [16][16]byte
	for i := range vectors {
		for j := range vectors[i] {
			vectors[i][j] = byte(i*19 + j*7)
		}
		body = append(body, v128ConstBytes(vectors[i])...)
		body = append(body, 0x21, byte(i), 0x20, byte(i), 0x1a)
	}
	// A cached old read survives a rewrite of its local before it is consumed.
	body = append(body, 0x20, 15)
	body = append(body, v128ConstBytes(vectors[0])...)
	body = append(body, 0x21, 15)
	mismatch := func(v [16]byte) {
		body = append(body, v128ConstBytes(v)...)
		body = append(body, simdOp(81)...)
		body = append(body, simdOp(83)...)
		body = append(body, 0x20, 16, 0x72, 0x21, 16)
	}
	mismatch(vectors[15])
	vectors[15] = vectors[0]
	salt := [16]byte{0xd3, 0x76, 9, 0xff, 0x81, 0x4a, 0x22, 7, 6, 5, 4, 3, 2, 1, 0x80, 0x17}
	for i := range vectors {
		body = append(body, 0x20, byte(i))
		body = append(body, v128ConstBytes(salt)...)
		body = append(body, simdOp(81)...)
	}
	for i := 15; i >= 0; i-- {
		mismatch(applyV128BooleanBytes(81, vectors[i], salt))
	}
	for i := range vectors {
		body = append(body, 0x20, byte(i))
		mismatch(vectors[i])
	}
	body = append(body, 0x20, 16, 0x0b)
	m := modFuncs(t, funcDef{results: []wasm.ValType{wasm.I32}, body: body})
	for _, on := range []bool{false, true} {
		vectorRegionsEnabled = on
		for _, features := range []shared.AMD64Features{0, shared.AMD64ModernBaseline} {
			for _, compact := range []bool{false, true} {
				// The helper maps a normal result buffer; only the declared i32 result is read.
				out := runAmd64V128WithOptions(t, m, nil, CompileOptions{AMD64Features: features, AMD64FeaturesSet: true, CompactNative: compact})
				if got := binary.LittleEndian.Uint32(out[:4]); got != 0 {
					t.Fatalf("on=%v features=%x compact=%v mismatches=%x", on, features, compact, got)
				}
			}
		}
	}
}
