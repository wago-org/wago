//go:build arm64

package arm64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestSelectLocalTeeSinkExecArm64(t *testing.T) {
	body := []byte{
		0x00,
		0x20, 0x00, // local.get 0
		0x20, 0x01, // local.get 1
		0x20, 0x02, // local.get 2
		0x1b,       // select
		0x22, 0x00, // local.tee 0
		0x20, 0x00, // local.get 0
		0x6a, // i32.add
		0x0b,
	}
	m := mod1(t, []wasm.ValType{wasm.I32, wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, body)
	for _, test := range []struct {
		a, b, cond uint64
		want       uint32
	}{
		{a: 7, b: 11, cond: 1, want: 14},
		{a: 7, b: 11, cond: 0, want: 22},
		{a: 0xffffffff, b: 2, cond: 1, want: 0xfffffffe},
	} {
		got, err := runArm64WrapperWithOptions(t, m, CompileOptions{}, test.a, test.b, test.cond)
		if err != nil {
			t.Fatal(err)
		}
		if uint32(got) != test.want {
			t.Fatalf("select(%#x, %#x, %#x) doubled = %#x, want %#x", test.a, test.b, test.cond, uint32(got), test.want)
		}
	}
}
