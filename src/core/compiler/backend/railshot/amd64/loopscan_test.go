//go:build amd64

package amd64

import (
	"slices"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestLoopScanDecodesNestedTryTable(t *testing.T) {
	body := []byte{
		0x21, 0x01, // local.set 1
		0x1f, 0x40, 0x01, 0x00, 0x00, 0x00, // try_table void, catch tag 0 -> label 0
		0x21, 0x02, // local.set 2 in try body
		0x0b,       // end try_table
		0x21, 0x03, // local.set 3 after try_table
		0x0b, // loop end
	}
	r := wasm.NewReader(body)
	set, _, valid := scanLoopBody(r, nil)
	if !valid || !slices.Equal(set, []uint16{1, 2, 3}) {
		t.Fatalf("try_table scan = %v valid=%v", set, valid)
	}
	if r.Offset() != 0 {
		t.Fatalf("reader offset = %d, want 0", r.Offset())
	}
}

func TestLoopScanRejectsPartialFindings(t *testing.T) {
	malformed := []byte{
		0x21, 0x01, // partial finding must be discarded
		0x1f, 0x40, 0x01, 0x00, 0x00, // truncated catch label
	}
	if set, _, valid := scanLoopBody(wasm.NewReader(malformed), nil); valid || set != nil {
		t.Fatalf("malformed scan retained partial findings: %v valid=%v", set, valid)
	}
}

func TestLoopScanDecodesMemory64Immediate(t *testing.T) {
	m := &wasm.Module{Memories: []wasm.MemType{{Limits: wasm.Limits{Min: 1}}, {Limits: wasm.Limits{Min: 1, Addr64: true}}}}
	body := []byte{
		0x20, 0x00,
		0xfd, 0x00, 0x44, 0x01, 0x80, 0x80, 0x80, 0x80, 0x10, // v128.load memory 1, offset 2^32
		0x1a,
		0x21, 0x02,
		0x0b,
	}
	set, grow, valid := scanLoopBody(wasm.NewReader(body), m)
	if !valid || grow || !slices.Equal(set, []uint16{2}) {
		t.Fatalf("mixed-width scan = set=%v grow=%v valid=%v", set, grow, valid)
	}
}
