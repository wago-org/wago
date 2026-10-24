//go:build arm64

package arm64

import (
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

var simdStoreLoadAddressEnabled = true

type simdStoreLoadAddress struct {
	End, LoadPC int
	Extract     shared.SIMDLoadExtract
	Narrow      bool
}

// A successful vector store has already checked this same full 16-byte span.
// Linear memories cannot shrink, and these expressions contain no calls or
// growth, so the same address/base remains valid for the following real read.
func (f *fn) emitSIMDStoreLoadAddress(r *wasm.Reader, p simdStoreLoadAddress, base, ea Reg, disp int32) error {
	if err := r.JumpTo(p.End); err != nil {
		return err
	}
	addedPins := maskOf(base, ea) &^ f.pinned
	f.pinned = f.pinned.add(base).add(ea)
	defer func() { f.pinned &^= addedPins }()
	oldPC := f.wasmPC
	f.wasmPC = f.tracePCBase + uint32(p.LoadPC)
	defer func() { f.wasmPC = oldPC }()
	if profileEnabled && f.stats != nil && f.stats.RecordSources {
		previous := f.enterProfileInstruction()
		defer func() { f.switchProfileOrigin(previous) }()
	}
	if !p.Narrow {
		out := f.allocFReg(0)
		f.a.LdrQIdx(out, base, ea, disp)
		f.pushVReg(out)
	} else if p.Extract.Float {
		out := f.allocFReg(0)
		f.a.LdrFIdx(out, base, ea, disp+p.Extract.Offset, p.Extract.Size == 8)
		typ := mtF32
		if p.Extract.Size == 8 {
			typ = mtF64
		}
		f.pushFReg(out, typ)
	} else {
		out := f.allocReg(maskOf(base).add(ea))
		f.a.LoadIdx(out, base, ea, disp+p.Extract.Offset, p.Extract.Size, p.Extract.Signed, p.Extract.Size == 8)
		typ := mtI32
		if p.Extract.Size == 8 {
			typ = mtI64
		}
		f.pushReg(out, typ)
	}
	if p.Narrow {
		f.stats.peep("simd-load-extract")
	}
	f.stats.peep("simd-store-load-address")
	return nil
}
