//go:build amd64

package amd64

import "github.com/wago-org/wago/src/core/compiler/wasm"

// maxCallNextUseOps caps the post-call scan. It is deliberately small and uses
// only copied Reader state plus two register masks: compile memory stays O(1),
// and a long or structurally complicated region falls back to storing locals.
const maxCallNextUseOps = 64

// planCallDeadLocals finds dirty pinned locals whose next access after a call is
// an overwrite rather than a read. A callee cannot observe its caller's locals,
// so those values need not be copied to their canonical frame slots before the
// call. Control boundaries and malformed/unsupported immediates stop the scan
// conservatively.
func (f *fn) planCallDeadLocals(r *wasm.Reader) {
	f.callDeadGP, f.callDeadFP = 0, 0
	if !f.opt(optCallNextUse) || !f.usesCalls {
		return
	}
	var candGP, candFP regMask
	for x := 0; x < f.nLocals; x++ {
		reg, isFloat, ok := f.pinReg(x)
		if !ok || f.locals[x].state != lsReg {
			continue
		}
		if isFloat {
			candFP = candFP.add(reg)
		} else {
			candGP = candGP.add(reg)
		}
	}
	if candGP == 0 && candFP == 0 {
		return
	}

	peek := *r
	for fuel := 0; fuel < maxCallNextUseOps; fuel++ {
		op, err := peek.Byte()
		if err != nil {
			return
		}
		switch op {
		case 0x0f: // return: operand-producing local.gets were already scanned.
			f.callDeadGP |= uint16(candGP)
			f.callDeadFP |= uint16(candFP)
			return
		case 0x00, 0x02, 0x03, 0x04, 0x05, 0x0b, 0x0c, 0x0d, 0x0e:
			return // trap or structured-control boundary: keep canonical stores
		case 0x20, 0x21, 0x22: // local.get / local.set / local.tee
			x32, err := peek.U32()
			if err != nil {
				return
			}
			x := int(x32) + f.localBase
			reg, isFloat, ok := f.pinReg(x)
			if !ok {
				continue
			}
			cand := &candGP
			dead := &f.callDeadGP
			if isFloat {
				cand, dead = &candFP, &f.callDeadFP
			}
			if !cand.has(reg) {
				continue
			}
			if op != 0x20 {
				*dead |= uint16(1) << uint(reg) // overwritten before any read
			}
			*cand = cand.remove(reg)
			if candGP == 0 && candFP == 0 {
				return
			}
		default:
			if err := skipImmediates(&peek, op); err != nil {
				return
			}
		}
	}
}
