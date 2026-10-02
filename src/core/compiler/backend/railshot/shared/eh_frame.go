package shared

import (
	"fmt"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// MaxEHFrameRecords bounds the exception records and rooted exception values
// one function frame may reserve. It only keeps a hostile function from
// demanding an unbounded frame; real code stays far below it.
const MaxEHFrameRecords = 1024

// EHFrameShape is the exception-handling storage one function needs: one handler
// record per level of try_table nesting (sibling try_tables reuse a level) and
// one rooted exception slot per catch_ref / catch_all_ref clause. Code
// generation and the exception root maps must both size frames from it.
type EHFrameShape struct {
	TryRecords  int
	RootRecords int
}

// ScanEHFrameShape computes the EH frame shape of one validated function body.
func ScanEHFrameShape(classifier *wasm.ModuleInstructionClassifier, body []byte) (EHFrameShape, error) {
	var shape EHFrameShape
	// tryOpen[i] reports whether open control frame i is a try_table.
	tryOpen := make([]bool, 0, 16)
	tryDepth := 0
	r := wasm.NewReader(body)
	var imm wasm.InstructionImmediate
	for r.HasNext() {
		op, err := r.Byte()
		if err != nil {
			return shape, err
		}
		switch op {
		case 0x02, 0x03, 0x04: // block, loop, if
			if _, err := r.S33(); err != nil {
				return shape, err
			}
			tryOpen = append(tryOpen, false)
			continue
		case 0x05: // else
			continue
		case 0x0b: // end
			if n := len(tryOpen); n != 0 {
				if tryOpen[n-1] {
					tryDepth--
				}
				tryOpen = tryOpen[:n-1]
			}
			continue
		case 0x1f: // try_table blocktype vec(catch)
			if _, err := r.S33(); err != nil {
				return shape, err
			}
			n, err := r.U32()
			if err != nil {
				return shape, err
			}
			for i := uint32(0); i < n; i++ {
				kindByte, err := r.Byte()
				if err != nil {
					return shape, err
				}
				kind := wasm.CatchKind(kindByte)
				if kind == wasm.CatchTag || kind == wasm.CatchRef {
					if _, err := r.U32(); err != nil {
						return shape, err
					}
				}
				if _, err := r.U32(); err != nil {
					return shape, err
				}
				if kind == wasm.CatchRef || kind == wasm.CatchAllRef {
					shape.RootRecords++
				}
			}
			tryOpen = append(tryOpen, true)
			tryDepth++
			if tryDepth > shape.TryRecords {
				shape.TryRecords = tryDepth
			}
			continue
		}
		if err := classifier.ClassifyInto(r, op, &imm); err != nil {
			return shape, err
		}
	}
	if shape.TryRecords > MaxEHFrameRecords || shape.RootRecords > MaxEHFrameRecords {
		return shape, fmt.Errorf("exception handling needs %d try records and %d rooted values; limit %d", shape.TryRecords, shape.RootRecords, MaxEHFrameRecords)
	}
	return shape, nil
}
