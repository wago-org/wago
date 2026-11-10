//go:build amd64

package amd64

import "testing"

func TestVectorAliasTypeFilter(t *testing.T) {
	saved := vectorAliasTypeFilterEnabled
	defer func() { vectorAliasTypeFilterEnabled = saved }()
	for _, enabled := range []bool{false, true} {
		vectorAliasTypeFilterEnabled = enabled
		f := fn{s: newStack(), localType: []machineType{mtI32, mtI64, mtF32, mtF64, mtV128, mtV128}}
		a := f.s.pushValue(storage{kind: stReg, typ: mtV128, reg: 1, cval: 5})
		b := f.s.pushValue(storage{kind: stReg, typ: mtV128, reg: 2, cval: 6})
		for x := 0; x < 4; x++ {
			f.clearV128LocalAliases(x)
			if a.st.cval != 5 || b.st.cval != 6 {
				t.Fatal("scalar assignment changed vector aliases")
			}
		}
		f.clearV128LocalAliases(4)
		if a.st.cval != 0 || b.st.cval != 6 || a.st.reg != 1 {
			t.Fatal("vector assignment did not invalidate exactly its own alias")
		}
		f.clearV128LocalAliases(5)
		if b.st.cval != 0 || b.st.reg != 2 {
			t.Fatal("alias invalidation changed register ownership")
		}
	}
	vectorAliasTypeFilterEnabled = true
	// A scalar assignment must return before inspecting the operand stack.
	for _, typ := range []machineType{mtI32, mtI64, mtF32, mtF64} {
		f := fn{localType: []machineType{typ}}
		f.clearV128LocalAliases(0)
	}
}
