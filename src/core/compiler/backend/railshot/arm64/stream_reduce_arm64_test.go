//go:build arm64

package arm64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func streamingReductionModule(t testing.TB, mutate bool) *wasm.Module {
	body := []byte{0}
	for i := byte(0); i < 6; i++ {
		body = append(body, 0x41, 0, 0x41, i+1, 0x36, 2, i*4)
	}
	for i := byte(0); i < 6; i++ {
		body = append(body, 0x20, 0)
		if mutate {
			body = append(body, 0x41, 1, 0x6a, 0x22, 0)
		}
		body = append(body, 0x41, 0, 0x28, 2, i*4, 0x6c)
	}
	for i := 0; i < 5; i++ {
		body = append(body, 0x6a)
	}
	if mutate {
		body = append(body, 0x20, 0, 0x6a)
	}
	body = append(body, 0x0b)
	return modMem(t, 1, []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, body)
}

func TestStreamingReductionOverflowAndLocalWrites(t *testing.T) {

	for _, mutate := range []bool{false, true} {
		m := streamingReductionModule(t, mutate)
		{
			const enabled = false

			for _, guard := range []bool{false, true} {
				for _, x := range []uint64{0, 1, 17, 0x7fffffff, 0x80000000, 0xffffffff} {
					want := x * 21
					if mutate {
						want = x*22 + 97
					}
					got, err := runArm64WrapperWithOptions(t, m, CompileOptions{ElideBoundsChecks: guard}, x)
					if err != nil || uint32(got) != uint32(want) {
						t.Fatalf("mutate=%v enabled=%v guard=%v x=%x got=%x want=%x err=%v", mutate, enabled, guard, x, got, want, err)
					}
				}
			}
		}
	}
}

func TestStreamingReductionManyLiveLocals(t *testing.T) {

	for _, terms := range []byte{16, 32, 64} {
		body := []byte{1, terms, 0x7f}
		for i := byte(1); i <= terms; i++ {
			body = append(body, 0x41, 0, 0x41)
			body = append(body, wasmtest.SLEB32(int32(i))...)
			body = append(body, 0x36, 2)
			body = append(body, wasmtest.ULEB(uint32(i-1)*4)...)
		}
		for i := byte(1); i <= terms; i++ {
			body = append(body, 0x41, 0, 0x28, 2)
			body = append(body, wasmtest.ULEB(uint32(i-1)*4)...)
			body = append(body, 0x21, i)
		}
		body = append(body, 0x03, 0x7f)
		for i := byte(1); i <= terms; i++ {
			body = append(body, 0x20, i, 0x20, 0, 0x6c)
		}
		for i := byte(1); i < terms; i++ {
			body = append(body, 0x6a)
		}
		body = append(body, 0x0b, 0x0b)
		m := modMem(t, 1, []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, body)
		{
			const enabled = false

			for _, guard := range []bool{false, true} {
				for _, x := range []uint64{0, 17, 0xffffffff} {
					got, err := runArm64WrapperWithOptions(t, m, CompileOptions{ElideBoundsChecks: guard}, x)
					want := x * uint64(terms) * uint64(terms+1) / 2
					if err != nil || uint32(got) != uint32(want) {
						t.Fatalf("terms=%d enabled=%v guard=%v x=%x got=%x want=%x err=%v", terms, enabled, guard, x, got, want, err)
					}
				}
			}
		}
	}
}
