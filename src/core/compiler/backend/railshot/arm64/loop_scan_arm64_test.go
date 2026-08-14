//go:build arm64

package arm64

import (
	"encoding/binary"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestLoopScanUsesBoundedScratch(t *testing.T) {
	body := []byte{
		0x21, 0x03, // local.set 3
		0x22, 0x03, // local.tee 3 (deduplicated)
		0x03, 0x40, // nested loop (empty block type)
		0x21, 0x07, // local.set 7
		0x0b,       // nested end
		0x40, 0x00, // memory.grow 0
		0x0b, // outer end
	}
	r := wasm.NewReader(body)
	f := fn{sc: &scratch{}}
	scan := f.scanLoopBody(r, false)
	if r.Offset() != 0 {
		t.Fatalf("reader offset = %d, want restored to 0", r.Offset())
	}
	if !scan.exact || !scan.hasGrow || !scan.hasNested || scan.hasCall || scan.hasTable {
		t.Fatalf("scan flags = %+v", scan)
	}
	if scan.count != 2 || f.loopSetN != 2 || !f.loopSetsLocal(&ctrlFrame{loopSetStart: scan.start, loopSetCount: scan.count}, 3) ||
		!f.loopSetsLocal(&ctrlFrame{loopSetStart: scan.start, loopSetCount: scan.count}, 7) {
		t.Fatalf("scan locals = %v, used = %d", f.sc.loopSetLocals[:scan.count], f.loopSetN)
	}
}

func TestLoopScanFallsBackAtCaps(t *testing.T) {
	t.Run("operation fuel", func(t *testing.T) {
		body := make([]byte, maxLoopScanOps+1)
		for i := range maxLoopScanOps {
			body[i] = 0x01 // nop
		}
		body[len(body)-1] = 0x0b
		r := wasm.NewReader(body)
		f := fn{sc: &scratch{}}
		assertConservativeLoopScan(t, &f, r)
	})

	t.Run("local arena", func(t *testing.T) {
		body := make([]byte, 0, maxLoopSetLocals*3)
		var buf [binary.MaxVarintLen32]byte
		for i := uint32(0); i <= maxLoopSetLocals; i++ {
			body = append(body, 0x21) // local.set
			n := binary.PutUvarint(buf[:], uint64(i))
			body = append(body, buf[:n]...)
		}
		body = append(body, 0x0b)
		r := wasm.NewReader(body)
		f := fn{sc: &scratch{}}
		assertConservativeLoopScan(t, &f, r)
	})
}

func assertConservativeLoopScan(t *testing.T, f *fn, r *wasm.Reader) {
	t.Helper()
	scan := f.scanLoopBody(r, false)
	if scan.exact || scan.count != 0 || !scan.hasGrow || !scan.hasCall || !scan.hasNested || !scan.hasTable {
		t.Fatalf("fallback scan = %+v", scan)
	}
	if f.loopSetN != 0 || r.Offset() != 0 {
		t.Fatalf("fallback retained scratch/reader state: used=%d offset=%d", f.loopSetN, r.Offset())
	}
}
