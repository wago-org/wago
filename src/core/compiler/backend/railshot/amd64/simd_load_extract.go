//go:build amd64

package amd64

import (
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

var simdLoadExtractEnabled = true

func (f *fn) trySIMDLoadExtract(r *wasm.Reader, memoryIndex uint32, off uint64) (bool, error) {
	if !f.opt(optSIMDLoadExtract) {
		return false, nil
	}
	p, ok := shared.InspectSIMDLoadExtract(r)
	if !ok {
		return false, nil
	}
	if err := r.JumpTo(p.End); err != nil {
		return false, err
	}
	// Hardware guard faults on the narrowed lane cannot stand in for the original
	// vector span. Force the full access check before issuing the scalar read.
	guard := f.guardMode
	f.guardMode = false
	base, ea, disp, baseOwned, eaOwned := f.simdMemAddr(memoryIndex, off, 16)
	f.guardMode = guard
	if p.Float {
		out := f.allocFReg(0)
		f.a.FLoadIdx(out, base, ea, disp+p.Offset, p.Size == 8)
		f.releaseSIMDMemAddr(base, ea, baseOwned, eaOwned)
		typ := mtF32
		if p.Size == 8 {
			typ = mtF64
		}
		f.pushFReg(out, typ)
	} else {
		out := f.allocReg(maskOf(base).add(ea))
		f.a.LoadIdx(out, base, ea, disp+p.Offset, p.Size, p.Signed, p.Size == 8)
		f.releaseSIMDMemAddr(base, ea, baseOwned, eaOwned)
		typ := mtI32
		if p.Size == 8 {
			typ = mtI64
		}
		f.pushReg(out, typ)
	}
	f.stats.peep("simd-load-extract")
	return true, nil
}
