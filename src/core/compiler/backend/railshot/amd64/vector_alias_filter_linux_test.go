//go:build linux && amd64

package amd64

import (
	"bytes"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestVectorAliasFilterMixedAssignmentsPreserveCode(t *testing.T) {
	saved := vectorAliasTypeFilterEnabled
	defer func() { vectorAliasTypeFilterEnabled = saved }()
	a := [16]byte{0, 0x80, 0xff, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15}
	// Keep a vector tee live across assignments in all four scalar banks.
	// After consuming that alias, overwrite its local and read the new value.
	body := []byte{5, 1, 0x7f, 1, 0x7e, 1, 0x7d, 1, 0x7c, 2, 0x7b}
	body = append(body, v128ConstBytes(a)...)
	body = append(body, 0x22, 4, 0x41, 1, 0x21, 0, 0x42, 2, 0x21, 1,
		0x43, 0, 0, 0x80, 0x3f, 0x21, 2, 0x44, 0, 0, 0, 0, 0, 0, 0xf0, 0x3f, 0x21, 3,
		0x20, 4)
	body = append(body, simdOp(81)...)
	body = append(body, 0x21, 5)
	body = append(body, v128ConstBytes(a)...)
	body = append(body, simdOp(77)...)
	body = append(body, 0x21, 4, 0x20, 4, 0x0b)
	m := mod1(t, nil, []wasm.ValType{wasm.V128}, body)
	var want [16]byte
	for i := range a {
		want[i] = ^a[i]
	}
	for _, features := range []shared.AMD64Features{0, shared.AMD64ModernBaseline} {
		for _, compact := range []bool{false, true} {
			opts := CompileOptions{AMD64Features: features, AMD64FeaturesSet: true, CompactNative: compact,
				Optimizations: map[string]bool{"v128-pins": false}}
			var original []byte
			for _, enabled := range []bool{false, true} {
				vectorAliasTypeFilterEnabled = enabled
				if got := runAmd64V128WithOptions(t, m, nil, opts); got != want {
					t.Fatalf("filter=%v: got=%x want=%x", enabled, got, want)
				}
				cm, err := CompileModuleWith(m, opts)
				if err != nil {
					t.Fatal(err)
				}
				if !enabled {
					original = append([]byte(nil), cm.Code...)
				} else if !bytes.Equal(original, cm.Code) {
					t.Fatal("type filter changed generated code")
				}
				if cm.CodeImage != nil {
					_ = cm.CodeImage.Close()
				}
			}
		}
	}
}
