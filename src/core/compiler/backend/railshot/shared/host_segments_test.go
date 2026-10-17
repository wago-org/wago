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

func TestBoundedFixedSharedHostSegments(t *testing.T) {
	for _, tc := range []struct {
		name           string
		body           []byte
		callees        uint64
		bounded, yield bool
	}{
		{"shared-host-loop", []byte{0x03, 0x40, 0x41, 0, 0x28, 2, 0, 0x10, 0, 0x1a, 0x0c, 0, 0x0b, 0x0b}, 0, true, true},
		{"callee-loop", []byte{0x03, 0x40, 0x10, 1, 0x0c, 0, 0x0b, 0x0b}, 1, true, true},
		{"unknown-callee", []byte{0x10, 1, 0x0b}, 0, false, false},
		{"wrong-callee", []byte{0x10, 2, 0x0b}, 1, false, false},
		{"callee-overflow", []byte{0x10, 65, 0x0b}, 1, false, false},
		{"conditional-yield", []byte{0x20, 0, 0x04, 0x40, 0x10, 0, 0x0b, 0x0b}, 0, true, false},
		{"early-return", []byte{0x0f, 0x10, 0, 0x0b}, 0, true, false},
		{"memory-size", []byte{0x3f, 0, 0x1a, 0x10, 0, 0x0b}, 0, true, true},
		{"grow", []byte{0x40, 0, 0x10, 0, 0x0b}, 0, false, false},
		{"bulk", []byte{0xfc, 11, 0, 0x10, 0, 0x0b}, 0, false, false},
		{"atomic", []byte{0xfe, 0x10, 2, 0, 0x10, 0, 0x0b}, 0, false, false},
		{"unaligned-excess", []byte{0x28, 3, 0, 0x10, 0, 0x0b}, 0, false, false},
		{"other-memory", []byte{0x28, 0x42, 1, 0, 0x10, 0, 0x0b}, 0, false, false},
		{"post-host-cycle", []byte{0x10, 0, 0x03, 0x40, 0x0c, 0, 0x0b, 0x0b}, 0, false, false},
		{"host-bypass-cycle", []byte{0x03, 0x40, 0x20, 0, 0x0d, 0, 0x10, 0, 0x0c, 0, 0x0b, 0x0b}, 0, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bounded, yield := BoundedFixedSharedHostSegments(tc.body, 1, tc.callees)
			if bounded != tc.bounded || yield != tc.yield {
				t.Fatalf("got %t/%t want %t/%t", bounded, yield, tc.bounded, tc.yield)
			}
			if (tc.name == "shared-host-loop" || tc.name == "callee-loop") && BoundedHostSegments(tc.body, 1) {
				t.Fatal("resource-free certificate widened")
			}
		})
	}
}
