//go:build amd64

package amd64

import (
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
	if profileEnabled && f.stats != nil && (f.stats.RecordSources || f.stats.RecordUnwind || f.stats.RecordAdapterUnwind) {
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

func (f *fn) LocalOffset(local int) int32 { return f.localAddr(local) }
func (f *fn) Constant(r uint8, v int64, wide bool) {
	if wide {
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
			f.a.MovRegReg32(Reg(d), Reg(s))
		}
	}
}
func (f *fn) Load(r uint8, off int32, w bool) {
	if w {
		f.a.Load64(Reg(r), RSP, off)
	} else {
		f.a.Load32(Reg(r), RSP, off)
	}
}
func (f *fn) Store(off int32, r uint8, w bool) {
	if w {
		f.a.Store64(RSP, off, Reg(r))
	} else {
		f.a.Store32(RSP, off, Reg(r))
	}
}
func (f *fn) Clobbers(op shared.IntOp) uint64 {
	if op == shared.IntShl || op == shared.IntShrS || op == shared.IntShrU {
		return 1 << RCX
	}
	return 0
}
func (f *fn) Operand(op shared.IntOp, w bool, o shared.ScalarOperand) bool {
	if o.Kind == shared.ScalarRegister {
		return true
	}
	if op == shared.IntShl || op == shared.IntShrS || op == shared.IntShrU {
		return o.Kind == shared.ScalarConstant
	}
	if o.Kind == shared.ScalarFrame {
		return true
	}
	return o.Kind == shared.ScalarConstant && (!w || o.Constant == int64(int32(o.Constant)))
}
func scalarCondition(op shared.IntOp) Cond {
	return []Cond{condE, condNE, condL, condB, condG, condA, condLE, condBE, condGE, condAE}[op-shared.IntEq]
}
func (f *fn) scalarCmp(op shared.IntOp, w bool, l Reg, r shared.ScalarOperand) {
	switch r.Kind {
	case shared.ScalarConstant:
		f.a.AluRI(7, l, int32(r.Constant), w)
	case shared.ScalarFrame:
		f.a.AluRM(0x3b, l, RSP, r.Offset, w)
	default:
		f.a.AluRR(0x39, l, Reg(r.Reg), w)
	}
}
func (f *fn) Binary(op shared.IntOp, w bool, d, l uint8, r shared.ScalarOperand) {
	dst, left := Reg(d), Reg(l)
	if op >= shared.IntEq && op <= shared.IntGeU {
		f.scalarCmp(op, w, left, r)
		f.a.SetccReg(scalarCondition(op), dst)
		return
	}
	f.Move(d, l, w)
	if op == shared.IntMul {
		switch r.Kind {
		case shared.ScalarConstant:
			f.a.ImulRI(dst, int32(r.Constant), w)
		case shared.ScalarFrame:
			f.a.ImulRM(dst, RSP, r.Offset, w)
		default:
			f.a.IMul(dst, Reg(r.Reg), w)
		}
		return
	}
	if op == shared.IntShl || op == shared.IntShrS || op == shared.IntShrU {
		digit := byte(4)
		if op == shared.IntShrS {
			digit = 7
		} else if op == shared.IntShrU {
			digit = 5
		}
		if r.Kind == shared.ScalarConstant {
			f.a.ShiftImm(digit, dst, byte(r.Constant), w)
		} else {
			f.Move(uint8(RCX), r.Reg, false)
			f.a.ShiftCL(digit, dst, w)
		}
		return
	}
	enc := aluTable[wOp(op)]
	switch r.Kind {
	case shared.ScalarConstant:
		f.a.AluRI(enc.digit, dst, int32(r.Constant), w)
	case shared.ScalarFrame:
		f.a.AluRM(enc.rm, dst, RSP, r.Offset, w)
	default:
		f.a.AluRR(enc.rr, dst, Reg(r.Reg), w)
	}
}
func (f *fn) BranchZero(r uint8) int { f.a.TestSelf(Reg(r), false); return f.a.JccPlaceholder(condE) }
func (f *fn) BranchCompare(op shared.IntOp, w bool, l uint8, r shared.ScalarOperand) int {
	f.scalarCmp(op, w, Reg(l), r)
	return f.a.JccPlaceholder(scalarCondition(op) ^ 1)
}
func (f *fn) Jump() int                 { return f.a.JmpPlaceholder() }
func (f *fn) Patch(site, pos int) error { f.a.PatchRel32(site, pos); return nil }
func (f *fn) Return(r uint8, w bool, slots int) {
	if f.singleRegResult {
		f.Move(uint8(RAX), r, w)
	} else {
		f.Store(f.spillOff(0), r, true)
		if f.maxSpill < 1 {
			f.maxSpill = 1
		}
	}
}

func (f *fn) ScaledAdd(w bool, d, l, r, shift uint8) bool {
	f.a.LeaScaledW(Reg(d), Reg(l), Reg(r), shift, 0, w)
	return true
}
