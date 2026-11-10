//go:build arm64

package arm64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestStoreAddressAhead(t *testing.T) {
	cases := []struct {
		name string
		code []byte
		want bool
	}{
		{"constant", []byte{0x41, 42, 0x36, 2, 0}, true},
		{"expression", []byte{0x20, 5, 0x41, 7, 0x71, 0x22, 0, 0x41, 4, 0x6a, 0x36, 2, 0}, true},
		{"two values", []byte{0x41, 1, 0x41, 2, 0x36, 2, 0}, false},
		{"underflow", []byte{0x41, 1, 0x6a, 0x36, 2, 0}, false},
		{"control barrier", []byte{0x41, 1, 0x0d, 0, 0x36, 2, 0}, false},
		{"truncated", []byte{0x41, 0x80}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := wasm.ReaderFrom(tc.code)
			if got := storeAddressAhead(r); got != tc.want {
				t.Fatalf("got %t, want %t", got, tc.want)
			}
			if r.Offset() != 0 {
				t.Fatal("probe advanced caller")
			}
		})
	}
}
