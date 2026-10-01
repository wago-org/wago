//go:build amd64

package amd64

import "os"

// The exact linear sum has no observable effects before a memory trap. Its
// existing proof covers the whole range and preserves modular memory32 wrap.
// Signal mode reads the current descriptor without reserving another register.
var linearSumSignalsEnabled = os.Getenv("WAGO_AMD64_LINEAR_SUM_SIGNALS") == "1"

func (f *fn) compareLinearSumSize(end Reg) {
	if f.memSizeReg != regNone {
		f.a.Cmp64(end, f.memSizeReg)
	} else {
		f.a.AluRM(cmpRMcode, end, RBX, -bdCurBytes, true)
	}
}
