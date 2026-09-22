//go:build linux && amd64

package amd64

import (
	"encoding/hex"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"testing"
)

func TestReviewNestedShiftWrongResult(t *testing.T) {
	for _, tc := range []struct {
		name, body  string
		input, want uint32
	}{
		{"rotate", "004100200073410041002000747520004101756c782000780b", 0x21624472, 0x911c8858},
		{"shift", "0041014100200071410041d4d8ebc67e20007578200020006c7176740b", 0x7f859dd6, 1},
		{"mixed", "0041014101200041016a410020007677787520007841016b2000710b", 0xf7aa7331, 0x7331},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body, err := hex.DecodeString(tc.body)
			if err != nil {
				t.Fatal(err)
			}
			m := mod1(t, []wasm.ValType{i32}, []wasm.ValType{i32}, body)
			got := uint32(runAmd64(t, m, int32(tc.input)))
			if got != tc.want {
				t.Fatalf("got %#x, want %#x", got, tc.want)
			}
		})
	}
}
func TestReviewNestedShiftCompile(t *testing.T) {
	body, err := hex.DecodeString("00410020004100200077200074200078200041006b7478770b")
	if err != nil {
		t.Fatal(err)
	}
	m := mod1(t, []wasm.ValType{i32}, []wasm.ValType{i32}, body)
	cm, err := CompileModule(m)
	if err != nil {
		t.Fatalf("valid i32 expression rejected: %v", err)
	}
	if cm.CodeImage != nil {
		defer cm.CodeImage.Close()
	}
}
