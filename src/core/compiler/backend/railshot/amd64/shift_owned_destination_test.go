//go:build (linux || darwin || windows) && amd64

package amd64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"math/bits"
	"testing"
)

func TestShiftOwnedDestination(t *testing.T) {
	saved := shiftOwnedDestinationEnabled
	defer func() { shiftOwnedDestinationEnabled = saved }()
	for op := byte(0x74); op <= 0x78; op++ {
		for _, owned := range []bool{false, true} {
			body := []byte{0, 0x20, 0}
			if owned {
				body = append(body, 0x20, 1, 0x73)
			}
			body = append(body, 0x41, 7, op, 0x20, 0, 0x73, 0x0b)
			m := mod1(t, []wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, body)
			for _, enabled := range []bool{false, true} {
				shiftOwnedDestinationEnabled = enabled
				var stats ModuleStats
				cm, err := CompileModuleWith(m, CompileOptions{AMD64FeaturesSet: true, Stats: optionalTestStats(&stats)})
				if err != nil {
					t.Fatal(err)
				}
				if cm.CodeImage != nil {
					defer cm.CodeImage.Close()
				}
				if diagnosticsEnabled && enabled && owned && stats.Funcs[0].Peephole["shift-owned-destination"] == 0 {
					t.Fatal("owned shift was not reused", stats.Funcs[0].Peephole)
				}
				for _, a := range []uint32{0, 1, 0x80000000, 0xffffffff, 0x12345678} {
					b := uint32(0xabcd4321)
					x := a
					if owned {
						x ^= b
					}
					var want uint32
					switch op {
					case 0x74:
						want = x << 7
					case 0x75:
						want = uint32(int32(x) >> 7)
					case 0x76:
						want = x >> 7
					case 0x77:
						want = bits.RotateLeft32(x, 7)
					case 0x78:
						want = bits.RotateLeft32(x, -7)
					}
					want ^= a
					if got := uint32(runCompiledAmd64u(t, cm, uint64(a), uint64(b))); got != want {
						t.Fatalf("op=%x owned=%v enabled=%v a=%x got=%x want=%x", op, owned, enabled, a, got, want)
					}
				}
			}
		}
	}
}

func TestShiftOwnedDestinationI64(t *testing.T) {
	saved := shiftOwnedDestinationEnabled
	defer func() { shiftOwnedDestinationEnabled = saved }()
	for op := byte(0x86); op <= 0x8a; op++ {
		for _, owned := range []bool{false, true} {
			body := []byte{0, 0x20, 0}
			if owned {
				body = append(body, 0x20, 1, 0x85)
			}
			body = append(body, 0x42, 7, op, 0x20, 0, 0x85, 0x0b)
			m := mod1(t, []wasm.ValType{wasm.I64, wasm.I64}, []wasm.ValType{wasm.I64}, body)
			for _, enabled := range []bool{false, true} {
				shiftOwnedDestinationEnabled = enabled
				cm, err := CompileModuleWith(m, CompileOptions{AMD64FeaturesSet: true})
				if err != nil {
					t.Fatal(err)
				}
				if cm.CodeImage != nil {
					defer cm.CodeImage.Close()
				}
				a := uint64(0x8000000000000000)
				for i := 0; i < 128; i++ {
					b := a*6364136223846793005 + 1
					x := a
					if owned {
						x ^= b
					}
					var want uint64
					switch op {
					case 0x86:
						want = x << 7
					case 0x87:
						want = uint64(int64(x) >> 7)
					case 0x88:
						want = x >> 7
					case 0x89:
						want = bits.RotateLeft64(x, 7)
					case 0x8a:
						want = bits.RotateLeft64(x, -7)
					}
					want ^= a
					if got := runCompiledAmd64u(t, cm, a, b); got != want {
						t.Fatalf("op=%x owned=%v enabled=%v a=%x got=%x want=%x", op, owned, enabled, a, got, want)
					}
					a = b
				}
			}
		}
	}
}
