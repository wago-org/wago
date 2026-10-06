package shared

import (
	"bytes"
	"testing"
)

func TestBoundedHostSegments(t *testing.T) {
	for _, tc := range []struct {
		name string
		body []byte
		want bool
	}{
		{"leaf", []byte{0x20, 0, 0x41, 1, 0x6a, 0x0b}, true},
		{"host-loop", []byte{0x03, 0x40, 0x10, 0, 0x0c, 0, 0x0b, 0x0b}, true},
		{"loop-with-exit", []byte{0x02, 0x40, 0x03, 0x40, 0x20, 0, 0x0d, 1, 0x10, 0, 0x0c, 0, 0x0b, 0x0b, 0x0b}, true},
		{"no-host-loop", []byte{0x03, 0x40, 0x0c, 0, 0x0b, 0x0b}, false},
		{"host-before-unbounded-loop", []byte{0x10, 0, 0x03, 0x40, 0x0c, 0, 0x0b, 0x0b}, false},
		{"conditional-host-can-be-skipped", []byte{0x03, 0x40, 0x20, 0, 0x04, 0x40, 0x10, 0, 0x0b, 0x0c, 0, 0x0b, 0x0b}, false},
		{"both-arms-host", []byte{0x03, 0x40, 0x20, 0, 0x04, 0x40, 0x10, 0, 0x05, 0x10, 0, 0x0b, 0x0c, 0, 0x0b, 0x0b}, true},
		{"br-if-skips-host", []byte{0x03, 0x40, 0x20, 0, 0x0d, 0, 0x10, 0, 0x0c, 0, 0x0b, 0x0b}, false},
		{"br-table-skips-host", []byte{0x03, 0x40, 0x20, 0, 0x0e, 0, 0, 0x10, 0, 0x0b, 0x0b}, false},
		{"nested-loop-skips-host", []byte{0x03, 0x40, 0x10, 0, 0x03, 0x40, 0x0c, 0, 0x0b, 0x0c, 0, 0x0b, 0x0b}, false},
		{"defined-call", []byte{0x10, 1, 0x0b}, false},
		{"indirect-call", []byte{0x11, 0, 0, 0x0b}, false},
		{"tail-call", []byte{0x12, 0, 0x0b}, false},
		{"memory-fill", []byte{0xfc, 11, 0, 0x0b}, false},
		{"missing-end", []byte{0x10, 0}, false},
		{"invalid-label", []byte{0x0c, 1, 0x0b}, false},
		{"oversize", append(bytes.Repeat([]byte{0x01}, 384), 0x0b), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := BoundedHostSegments(tc.body, 1); got != tc.want {
				t.Fatalf("bounded=%v; want %v", got, tc.want)
			}
		})
	}
}

func TestBoundedNativeHostBody(t *testing.T) {
	for _, tc := range []struct {
		name string
		body []byte
		want bool
	}{
		{"single-host", []byte{0x20, 0, 0x10, 0, 0x0b}, true},
		{"conditional-host", []byte{0x20, 0, 0x04, 0x40, 0x10, 0, 0x0b, 0x0b}, true},
		{"two-hosts", []byte{0x10, 0, 0x10, 0, 0x0b}, true},
		{"host-loop", []byte{0x03, 0x40, 0x10, 0, 0x0c, 0, 0x0b, 0x0b}, false},
		{"host-before-loop", []byte{0x10, 0, 0x03, 0x40, 0x0c, 0, 0x0b, 0x0b}, false},
		{"defined-call", []byte{0x10, 1, 0x0b}, false},
		{"indirect-call", []byte{0x11, 0, 0, 0x0b}, false},
		{"memory-load", []byte{0x20, 0, 0x28, 2, 0, 0x0b}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := BoundedNativeHostBody(tc.body, 1); got != tc.want {
				t.Fatalf("bounded=%t; want %t", got, tc.want)
			}
		})
	}
}
