//go:build arm64 && !tinygo && !wago_profile

package arm64

import (
	"encoding/binary"
	"fmt"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"

	"os"
)

var sharedScalarEnabled = os.Getenv("WAGO_SHARED_SCALAR") != "0"
var scalarRegisterOrder = func() []uint8 {
	r := make([]uint8, len(gpAlloc))
	for i, v := range gpAlloc {
		r[i] = uint8(v)
	}
	return r
}()

func (f *fn) SpillOffset(slot int) int32 { return f.spillOff(slot) }
func (f *fn) Position() int              { return f.a.Len() }

func (f *fn) LocalOffset(local int) int32 { return f.localOff(local) }

func (f *fn) Load(r uint8, off int32, w bool) {
	f.scalarFrameUsed = true
	if w {
		f.ld64(Reg(r), SP, off)
	} else {
		f.ld32(Reg(r), SP, off)
	}
}
func (f *fn) Store(off int32, r uint8, w bool) {
	f.scalarFrameUsed = true
	if w {
		f.st64(SP, off, Reg(r))
	} else {
		f.st32(SP, off, Reg(r))
	}
}

func (f *fn) BranchZero(r uint8) int { return f.a.Cbz32(Reg(r)) }
func (f *fn) BranchCompare(op shared.IntOp, w bool, l uint8, r shared.ScalarOperand) int {
	f.scalarCmp(w, Reg(l), r)
	return f.a.Bcond(scalarCondition(op) ^ 1)
}
func (f *fn) Jump() int { return f.a.Branch() }
func (f *fn) Patch(site, pos int) error {
	var ok bool
	op := binary.LittleEndian.Uint32(f.a.B[site:])
	if op&0xfc000000 == 0x14000000 {
		ok = f.a.PatchBranch26(site, pos)
	} else {
		ok = f.a.PatchBranch19(site, pos)
	}
	if !ok {
		return fmt.Errorf("arm64: scalar branch displacement exceeds instruction range")
	}
	return nil
}

func (f *fn) ScaledAdd(wide bool, d, l, r, shift uint8) bool {
	// Shared width is is64; the encoder's final argument selects W registers.
	f.a.AddShifted(Reg(d), Reg(l), Reg(r), shift, !wide)
	return true
}
