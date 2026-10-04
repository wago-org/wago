//go:build arm64

package arm64

import (
	"encoding/binary"
	"fmt"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"os"
	"time"
)

var sharedScalarEnabled = os.Getenv("WAGO_SHARED_SCALAR") != "0"
var scalarRegisterOrder = func() []uint8 {
	r := make([]uint8, len(gpAlloc))
	for i, v := range gpAlloc {
		r[i] = uint8(v)
	}
	return r
}()

func (f *fn) admitScalar(c *wasm.Func) shared.ScalarSummary {
	if !sharedScalarEnabled || f.nLocals > 256 || f.moduleEH || f.gcFrameRoots != nil || len(f.gcTypeLayouts) != 0 || len(f.customInstructions) != 0 {
		return shared.ScalarSummary{}
	}
	if profileEnabled && f.stats != nil && f.stats.RecordSources {
		return shared.ScalarSummary{}
	}
	begin := time.Time{}
	if diagnosticsEnabled && f.stats != nil {
		begin = time.Now()
	}
	var types [256]wasm.ValType
	i := copy(types[:], f.ft.Params)
	for _, run := range c.Locals.Runs {
		for k := uint32(0); k < run.Count; k++ {
			types[i] = run.Type
			i++
		}
	}
	summary := shared.AdmitScalar(c.BodyBytes, f.ft, types[:i])
	if diagnosticsEnabled && f.stats != nil {
		f.stats.ScalarAdmissionNanos = uint64(time.Since(begin))
		f.stats.ScalarBodyBytes = len(c.BodyBytes)
		f.stats.SharedScalar = summary.Eligible
	}
	return summary
}
func (f *fn) scalarBody(c *wasm.Func) error {
	var widths [256]bool
	for i, t := range f.localType {
		widths[i] = t == mtI64
	}
	slots, err := f.sc.scalar.CompileScalar(c.BodyBytes, f.scalarSummary, widths[:f.nLocals], f.nParams, f)
	if slots > f.maxSpill {
		f.maxSpill = slots
	}
	if diagnosticsEnabled && f.stats != nil {
		f.stats.ScalarSpills = f.sc.scalar.Spills
		f.stats.ScalarReloads = f.sc.scalar.Reloads
	}
	return err
}
func (f *fn) Registers() ([]uint8, uint64) {
	return scalarRegisterOrder, uint64(f.reserved.union(f.pinned))
}
func (f *fn) SpillOffset(slot int) int32 { return f.spillOff(slot) }
func (f *fn) Position() int              { return f.a.Len() }

func (f *fn) LocalOffset(local int) int32 { return f.localOff(local) }
func (f *fn) Constant(r uint8, v int64, w bool) {
	if w {
		f.a.MovImm64(Reg(r), uint64(v))
	} else {
		f.a.MovImm32(Reg(r), int32(v))
	}
}
func (f *fn) Move(d, s uint8, w bool) {
	if d != s {
		if w {
			f.a.MovReg64(Reg(d), Reg(s))
		} else {
			f.a.MovReg32(Reg(d), Reg(s))
		}
	}
}
func (f *fn) Load(r uint8, off int32, w bool) {
	if w {
		f.ld64(Reg(r), SP, off)
	} else {
		f.ld32(Reg(r), SP, off)
	}
}
func (f *fn) Store(off int32, r uint8, w bool) {
	if w {
		f.st64(SP, off, Reg(r))
	} else {
		f.st32(SP, off, Reg(r))
	}
}
func (f *fn) Clobbers(op shared.IntOp) uint64 { return 0 }
func (f *fn) Operand(op shared.IntOp, w bool, o shared.ScalarOperand) bool {
	return o.Kind == shared.ScalarRegister || o.Kind == shared.ScalarConstant && (op == shared.IntAdd || op == shared.IntSub) && o.Constant >= 0 && o.Constant < 4096
}
func scalarCondition(op shared.IntOp) Cond {
	return []Cond{condE, condNE, condL, condB, condG, condA, condLE, condBE, condGE, condAE}[op-shared.IntEq]
}
func (f *fn) scalarCmp(w bool, l Reg, r shared.ScalarOperand) {
	if w {
		f.a.CmpReg64(l, Reg(r.Reg))
	} else {
		f.a.CmpReg32(l, Reg(r.Reg))
	}
}
func (f *fn) Binary(op shared.IntOp, w bool, d, l uint8, r shared.ScalarOperand) {
	dst, left, right := Reg(d), Reg(l), Reg(r.Reg)
	if op >= shared.IntEq && op <= shared.IntGeU {
		f.scalarCmp(w, left, r)
		f.a.Cset32(dst, scalarCondition(op))
		return
	}
	if r.Kind == shared.ScalarConstant {
		if op == shared.IntAdd {
			if w {
				f.a.AddImm64(dst, left, uint32(r.Constant))
			} else {
				f.a.AddImm32(dst, left, uint32(r.Constant))
			}
		} else {
			if w {
				f.a.SubImm64(dst, left, uint32(r.Constant))
			} else {
				f.a.SubImm32(dst, left, uint32(r.Constant))
			}
		}
		return
	}
	if w {
		switch op {
		case shared.IntAdd:
			f.a.Add64(dst, left, right)
		case shared.IntSub:
			f.a.Sub64(dst, left, right)
		case shared.IntMul:
			f.a.Mul64(dst, left, right)
		case shared.IntAnd:
			f.a.And64(dst, left, right)
		case shared.IntOr:
			f.a.Orr64(dst, left, right)
		case shared.IntXor:
			f.a.Eor64(dst, left, right)
		case shared.IntShl:
			f.a.Lslv64(dst, left, right)
		case shared.IntShrS:
			f.a.Asrv64(dst, left, right)
		case shared.IntShrU:
			f.a.Lsrv64(dst, left, right)
		}
	} else {
		switch op {
		case shared.IntAdd:
			f.a.Add32(dst, left, right)
		case shared.IntSub:
			f.a.Sub32(dst, left, right)
		case shared.IntMul:
			f.a.Mul32(dst, left, right)
		case shared.IntAnd:
			f.a.And32(dst, left, right)
		case shared.IntOr:
			f.a.Orr32(dst, left, right)
		case shared.IntXor:
			f.a.Eor32(dst, left, right)
		case shared.IntShl:
			f.a.Lslv32(dst, left, right)
		case shared.IntShrS:
			f.a.Asrv32(dst, left, right)
		case shared.IntShrU:
			f.a.Lsrv32(dst, left, right)
		}
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
func (f *fn) Return(r uint8, w bool, slots int) {
	if f.singleRegResult {
		f.Move(uint8(X0), r, w)
	} else {
		f.Store(f.spillOff(0), r, true)
		if f.maxSpill < 1 {
			f.maxSpill = 1
		}
	}
}

func (f *fn) ScaledAdd(wide bool, d, l, r, shift uint8) bool {
	// Shared width is is64; the encoder's final argument selects W registers.
	f.a.AddShifted(Reg(d), Reg(l), Reg(r), shift, !wide)
	return true
}
