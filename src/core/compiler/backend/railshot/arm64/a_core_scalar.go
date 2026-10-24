//go:build arm64 && !tinygo && !wago_profile

package arm64

import (
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"

	"time"
)

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
	summary := shared.AdmitScalarFunction(c, f.ft)
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

func (f *fn) ParameterRegister(local int) (uint8, bool) {
	// Compact headers are used only by register-ABI entries. Admission already
	// excludes calls, effects and noninteger parameters. No target local pin
	// owns these registers: CompileScalar becomes their sole allocator.
	if f.compactFrameHeader {
		return uint8(intArgRegs[local]), true
	}
	return 0, false
}

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

func (f *fn) NormalizeI32(r uint8) { f.a.MovReg32(Reg(r), Reg(r)) }

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
