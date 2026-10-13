//go:build arm64

package arm64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"os"
)

var streamReduceEnabled = os.Getenv("WAGO_ARM64_NO_STREAM_REDUCE") != "1"

type streamReduction struct {
	ends         [64]int
	n, adds, end int
}

// Prove independent i32 product terms followed by their wrapping sum.
// Local writes may occur only inside a term, before it completes. Memory
// reads retain their original order; calls, stores and control are excluded.
func (f *fn) inspectStreamReduction(r *wasm.Reader) (p streamReduction, ok bool) {
	scan := *r
	start, depth := scan.Offset(), 0
	for scan.Offset()-start < 4096 {
		pc := scan.Offset()
		op, err := scan.Byte()
		if err != nil {
			return p, false
		}
		switch op {
		case 0x20, 0x22:
			x, err := scan.U32()
			if err != nil || int(x) >= len(f.localType) || f.localType[x] != mtI32 {
				return p, false
			}
			if op == 0x20 {
				depth++
			} else if depth <= p.n {
				return p, false
			}
		case 0x41:
			if _, err := scan.I32(); err != nil {
				return p, false
			}
			depth++
		case 0x28, 0x2c, 0x2d, 0x2e, 0x2f:
			if depth <= p.n {
				return p, false
			}
			align, err := scan.U32()
			if err != nil || align > 4 {
				return p, false
			}
			if _, err := scan.U32(); err != nil {
				return p, false
			}
		case 0x6a, 0x6b, 0x6c, 0x71, 0x72, 0x73, 0x74, 0x75, 0x76:
			if op == 0x6a && depth == p.n && p.n >= 4 {
				p.adds = pc
				for i := 1; i < p.n-1; i++ {
					next, err := scan.Byte()
					if err != nil || next != 0x6a {
						return p, false
					}
				}
				p.end = scan.Offset()
				return p, true
			}
			if depth < p.n+2 {
				return p, false
			}
			depth--
			if op == 0x6c && depth == p.n+1 {
				if p.n == len(p.ends) {
					return p, false
				}
				p.ends[p.n] = scan.Offset()
				p.n++
			}
		default:
			return p, false
		}
	}
	return p, false
}

func (f *fn) tryStreamReduction(r *wasm.Reader) (bool, error) {
	if !f.streamingReduction || f.unreachable || f.s.head.prev != sentinelNodeID || f.localBase != 0 ||
		f.selectGroupEnd != 0 || len(f.customInstructions) != 0 || f.memoryAddr64(0) || f.threadedMemory0 {
		return false, nil
	}
	op, exists := r.Peek()
	if !exists || (op != 0x20 && op != 0x41) {
		return false, nil
	}
	p, ok := f.inspectStreamReduction(r)
	if !ok {
		return false, nil
	}
	emit := func(op byte) error {
		f.branchHintUnlikely = false
		var previous profileOrigin
		if profileEnabled && f.stats != nil && f.stats.RecordSources {
			previous = f.enterProfileInstruction()
		}
		f.prepareStoreForward(op)
		err := f.emitPlain(r, op)
		if profileEnabled && f.stats != nil && f.stats.RecordSources {
			f.switchProfileOrigin(previous)
		}
		return err
	}
	for term := 0; term < p.n; term++ {
		for r.Offset() < p.ends[term] {
			f.wasmPC = f.tracePCBase + uint32(r.Offset())
			op, err := r.Byte()
			if err != nil {
				return true, err
			}
			if err := emit(op); err != nil {
				return true, err
			}
		}
		if term > 0 {
			f.wasmPC = f.tracePCBase + uint32(p.adds+term-1)
			if err := emit(0x6a); err != nil {
				return true, err
			}
			f.materialize(f.s.back())
		}
	}
	if err := r.JumpTo(p.end); err != nil {
		return true, err
	}
	f.stats.peep("streaming-integer-reduction")
	return true, nil
}
