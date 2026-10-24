package shared

import (
	"bytes"
	"testing"
)

func TestBoundedHostSegmentsScratch(t *testing.T) {
	// Exercise the complete admitted byte budget, nesting depth, and branch tape.
	straight := append(bytes.Repeat([]byte{0x01}, 383), 0x0b)
	nested := bytes.Repeat([]byte{0x02, 0x40}, 127)
	nested = append(nested, bytes.Repeat([]byte{0x0b}, 128)...)
	table := []byte{0x02, 0x40, 0x41, 0, 0x0e, 0x7f}
	table = append(table, make([]byte, 128)...)
	table = append(table, 0x0b, 0x0b)
	for _, body := range [][]byte{straight, nested, table, {0x20, 0, 0x41, 1, 0x6a, 0x0b}} {
		if !BoundedHostSegments(body, 0) || !BoundedNativeHostBody(body, 0) {
			t.Fatal("bounded body rejected")
		}
		if got := testing.AllocsPerRun(1, func() { BoundedHostSegments(body, 0); BoundedNativeHostBody(body, 0) }); got != 0 {
			t.Fatalf("scheduler proof allocated %g objects", got)
		}
	}
}
