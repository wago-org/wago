//go:build amd64

package amd64

import "github.com/wago-org/wago/src/core/compiler/wasm"

// Carry only an existing certificate through a completely inspected if. Neither
// arm can create or replace a certificate, change any local, collect, or branch.
// Sixteen attempts of at most 64 bytes / 32 operations bound compile work even
// for large, adversarial bodies. Inline readers and custom instruction semantics
// are deliberately excluded.
func (f *fn) prepareGCPureIf(reader *wasm.Reader, op byte, minCtrl int) {
	if op != 0x04 || !f.gcResolved.valid || f.unreachable || minCtrl != 0 ||
		f.moduleEH || len(f.customInstructions) != 0 || f.gcPureIfSites >= 16 {
		return
	}
	f.gcPureIfSites++
	r := *reader
	window, err := r.Bytes(min(64, r.BytesLeft()))
	if err != nil {
		return
	}
	r = wasm.ReaderFrom(window)
	typ, err := r.Byte()
	if err != nil || typ != 0x40 {
		return
	}
	// Only void ifs are admitted: integer results use a fixed merge register.
	seenElse := false
	for ops := 0; ops < 32 && r.Offset() < 64; ops++ {
		pc := uint64(f.tracePCBase) + uint64(reader.Offset()) + uint64(r.Offset())
		op, err := r.Byte()
		if err != nil {
			return
		}
		switch op {
		case 0x0b:
			if pc == 0 || pc > uint64(^uint32(0)) {
				return
			}
			f.gcPureIfEndPC = uint32(pc)
			f.stats.peep("gc-resolve-pure-if")
			return
		case 0x05:
			if seenElse {
				return
			}
			seenElse = true
		case 0x20:
			_, err = r.U32()
		case 0x41:
			_, err = r.I32()
		case 0x42:
			_, err = r.I64()
		case 0x1a,
			0x45, 0x46, 0x47, 0x48, 0x49, 0x4a, 0x4b, 0x4c, 0x4d, 0x4e, 0x4f,
			0x50, 0x51, 0x52, 0x53, 0x54, 0x55, 0x56, 0x57, 0x58, 0x59, 0x5a,
			0x6a, 0x6b, 0x6c, 0x71, 0x72, 0x73, 0x74, 0x75, 0x76, 0x77, 0x78,
			0x7c, 0x7d, 0x7e, 0x83, 0x84, 0x85, 0x86, 0x87, 0x88, 0x89, 0x8a:
		default:
			return
		}
		if err != nil {
			return
		}
	}
}
