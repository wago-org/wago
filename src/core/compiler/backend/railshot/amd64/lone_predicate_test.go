//go:build amd64

package amd64

import (
	encoder "github.com/wago-org/wago/src/core/encoder/amd64"
	"testing"
)

func TestLonePredicateAdmissionAndFallback(t *testing.T) {
	saved := lonePredicateEnabled
	defer func() { lonePredicateEnabled = saved }()
	for _, enabled := range []bool{false, true} {
		for _, prefix := range []bool{false, true} {
			for _, pin := range []bool{false, true} {
				lonePredicateEnabled = enabled
				stats := new(CodegenStats)
				f := &fn{a: &encoder.Asm{}, s: newStackWithCap(8), stats: stats}
				if prefix {
					f.pushReg(RDX, mtI64)
				}
				reg := RAX
				if pin {
					reg = R12
					f.pinnedLocalMask = maskOf(R12)
				}
				pred := f.pushReg(reg, mtI32)
				f.flushBranchPredicate(pred)
				want := enabled && !prefix && !pin
				if (pred.st.kind == stReg) != want {
					t.Fatalf("enabled=%v prefix=%v pin=%v storage=%v", enabled, prefix, pin, pred.st.kind)
				}
				if want && f.a.Len() != 0 {
					t.Fatalf("lone predicate emitted traffic: %x", f.a.B)
				}
				if !want && f.a.Len() == 0 {
					t.Fatal("required staging missing")
				}
				if diagnosticsEnabled && (stats.Peephole["lone-predicate"] != 0) != want {
					t.Fatal("admission counter", stats.Peephole)
				}
			}
		}
	}
}
