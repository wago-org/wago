//go:build linux && amd64

package amd64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestAffineLeaTreeCover(t *testing.T) {
	i32x2 := []wasm.ValType{wasm.I32, wasm.I32}
	cases := []struct {
		name string
		body []byte
		want int32
	}{
		{
			name: "deferred-index",
			// a + ((b + 7) << 2)
			body: []byte{0x00, 0x20, 0x00, 0x20, 0x01, 0x41, 0x07, 0x6a, 0x41, 0x02, 0x74, 0x6a, 0x0b},
			want: 77 + ((5 + 7) << 2),
		},
		{
			name: "commuted-outer-add",
			// ((a + 7) << 2) + b
			body: []byte{0x00, 0x20, 0x00, 0x41, 0x07, 0x6a, 0x41, 0x02, 0x74, 0x20, 0x01, 0x6a, 0x0b},
			want: ((77 + 7) << 2) + 5,
		},
		{
			name: "commuted-index-add",
			// a + ((7 + b) << 2)
			body: []byte{0x00, 0x20, 0x00, 0x41, 0x07, 0x20, 0x01, 0x6a, 0x41, 0x02, 0x74, 0x6a, 0x0b},
			want: 77 + ((7 + 5) << 2),
		},
		{
			name: "negative-index-offset",
			// a + ((b - 7) << 2)
			body: []byte{0x00, 0x20, 0x00, 0x20, 0x01, 0x41, 0x07, 0x6b, 0x41, 0x02, 0x74, 0x6a, 0x0b},
			want: 77 + ((5 - 7) << 2),
		},
	}

	saved := affineLeaEnabled
	defer func() { affineLeaEnabled = saved }()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := mod1(t, i32x2, []wasm.ValType{wasm.I32}, tc.body)

			affineLeaEnabled = true
			on := compileWithStats(t, m, false).Funcs[0]
			if got := runAmd64(t, m, 77, 5); got != tc.want {
				t.Fatalf("enabled result = %d, want %d", got, tc.want)
			}
			if hits := on.Peephole["affine-lea-cover"]; hits != 1 {
				t.Fatalf("affine-lea-cover = %d, want 1 (all: %v)", hits, on.Peephole)
			}

			affineLeaEnabled = false
			off := compileWithStats(t, m, false).Funcs[0]
			if got := runAmd64(t, m, 77, 5); got != tc.want {
				t.Fatalf("disabled result = %d, want %d", got, tc.want)
			}
			if on.CodeBytes >= off.CodeBytes {
				t.Fatalf("covered code = %d bytes, want smaller than disabled %d bytes",
					on.CodeBytes, off.CodeBytes)
			}
		})
	}
}
