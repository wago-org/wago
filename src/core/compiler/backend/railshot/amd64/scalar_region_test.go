//go:build (linux || darwin || windows) && amd64

package amd64

import (
	"bytes"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	encoder "github.com/wago-org/wago/src/core/encoder/amd64"
	"testing"
)

func scalarRegionFixture(typ machineType) *fn {
	f := &fn{a: &encoder.Asm{}, s: newStack(), sc: newScratch(), m: &wasm.Module{}, policy: currentCodegenPolicy(), scalarRegionNumeric: true, globalCellReg: regNone, memSizeReg: regNone, nParams: 2, nLocals: 2, nLocalSlots: 2, localType: []machineType{typ, typ}, localSlot: []uint32{0, 8}, locals: []localDef{{typ: typ, reg: R12, state: lsReg}, {typ: typ, reg: R8, state: lsReg}}, pinnedLocals: []int{0, 1}}
	f.pinnedLocalMask = maskOf(R12).union(maskOf(R8))
	return f
}
func scalarRegionEnable(t *testing.T) {
	old := scalarRegionsEnabled
	scalarRegionsEnabled = true
	t.Cleanup(func() { scalarRegionsEnabled = old })
}
func TestScalarRegionPressurePreflightPure(t *testing.T) {
	for _, wide := range []bool{false, true} {
		f := scalarRegionFixture(mtI64)
		s := scalarRegionState{n: 1, pending: scalarRegionPending{left: storage{kind: stLocalReg, typ: mtI64, reg: R12, idx: 0}, right: storage{kind: stConst, typ: mtI64, cval: 7}, op: opMul, typ: mtI64, index: 0, pc: 99, valid: true}}
		for _, r := range gpAlloc {
			f.reserved = f.reserved.add(r)
		}
		if wide {
			f.reserved = f.reserved.remove(RAX)
			s.pending.right.cval = 1 << 40
		}
		before := s
		code := append([]byte(nil), f.a.B...)
		reserved := f.reserved
		users := f.regUser
		if f.materializeScalarRegionPending(&s) {
			t.Fatalf("pressure case wide=%v unexpectedly admitted", wide)
		}
		if s != before || !bytes.Equal(code, f.a.B) || f.reserved != reserved || f.regUser != users || f.depth() != 0 {
			t.Fatal("failed materialization mutated state")
		}
	}
}
func TestScalarRegionExportOrderAndOwnership(t *testing.T) {
	f := scalarRegionFixture(mtI32)
	f.wasmPC = 1234
	s := scalarRegionState{n: 3, owned: maskOf(RAX), pending: scalarRegionPending{left: storage{kind: stLocalReg, typ: mtI32, reg: R12, idx: 0}, right: storage{kind: stConst, typ: mtI32, cval: 7}, op: opAdd, typ: mtI32, pc: 81, index: 1, valid: true}}
	s.values[0] = storage{kind: stReg, typ: mtI32, reg: RAX}
	s.values[2] = storage{kind: stConst, typ: mtI32, cval: 19}
	pins := f.pinned
	f.exportScalarRegion(&s)
	roots := f.rootsBottomToTop()
	if len(roots) != 3 || roots[0].st.kind != stReg || roots[0].st.reg != RAX || !roots[1].isDeferred() || roots[2].st.cval != 19 {
		t.Fatal("export reordered values")
	}
	if f.regUser[RAX] != roots[0] || f.pinned != pins || f.wasmPC != 1234 || s.n != 0 || s.owned != 0 || s.pending.valid {
		t.Fatal("export ownership/provenance not transferred")
	}
}
func scalarRegionRunBody(t *testing.T, body []byte, typ machineType, on bool) (uint64, int) {
	t.Helper()
	old := scalarRegionsEnabled
	scalarRegionsEnabled = on
	defer func() { scalarRegionsEnabled = old }()
	f := scalarRegionFixture(typ)
	f.a.Push(RCX)
	f.a.SubRsp(256)
	f.a.MovImm64(R12, 0xfffffffffffffff0)
	f.a.MovImm64(R8, 7)
	r := wasm.NewReader(body)
	hits := 0
	for r.HasNext() {
		if on {
			done, err := f.tryScalarRegion(r)
			if err != nil {
				t.Fatal(err)
			}
			if done {
				hits++
				continue
			}
		}
		f.wasmPC = uint32(r.Offset())
		op, err := r.Byte()
		if err != nil {
			t.Fatal(err)
		}
		if err = f.emitPlain(r, op); err != nil {
			t.Fatal(err)
		}
	}
	if f.depth() != 1 {
		t.Fatalf("result depth=%d", f.depth())
	}
	reg := f.materialize(f.s.back())
	f.a.MovReg64(RAX, reg)
	f.a.AddRsp(256)
	f.a.Pop(RCX)
	f.a.Store64(RCX, 0, RAX)
	f.a.Ret()
	return runCompiledAmd64u(t, &encoder.CompiledModule{Code: f.a.B, Entry: []int{0}}), hits
}
func scalarRegionI64(v int64) []byte {
	b := []byte{0x42}
	for {
		x := byte(v & 0x7f)
		v >>= 7
		done := (v == 0 && x&0x40 == 0) || (v == -1 && x&0x40 != 0)
		if !done {
			x |= 0x80
		}
		b = append(b, x)
		if done {
			return b
		}
	}
}
func TestScalarRegionRuntimeMatchesLegacy(t *testing.T) {
	tests := []struct {
		name string
		typ  machineType
		body []byte
		want uint64
	}{
		{"nested-i32-width", mtI32, []byte{0x20, 0, 0x20, 1, 0x6a, 0x41, 3, 0x6c, 0x41, 1, 0x6a}, uint64(uint32(0xffffffe6))},
		{"borrow-before-overwrite", mtI32, []byte{0x20, 0, 0x41, 3, 0x21, 0, 0x20, 0, 0x6b}, uint64(uint32(0xffffffed))},
		{"pending-under-other-value", mtI32, []byte{0x20, 0, 0x41, 3, 0x6a, 0x20, 1, 0x41, 2, 0x6c, 0x6b}, uint64(uint32(0xffffffe5))},
		{"drop-chain", mtI32, []byte{0x20, 0, 0x41, 3, 0x6a, 0x41, 2, 0x6c, 0x1a, 0x41, 9}, 9},
		{"tee-then-get", mtI32, []byte{0x20, 1, 0x41, 3, 0x6a, 0x22, 0, 0x20, 0, 0x6a}, 20},
	}
	wide := append([]byte{0x20, 1}, scalarRegionI64(1<<40)...)
	wide = append(wide, 0x7e, 0x42, 3, 0x7c)
	tests = append(tests, struct {
		name string
		typ  machineType
		body []byte
		want uint64
	}{"wide-immediate", mtI64, wide, 7*(1<<40) + 3})
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			base, _ := scalarRegionRunBody(t, tc.body, tc.typ, false)
			got, hits := scalarRegionRunBody(t, tc.body, tc.typ, true)
			if base != tc.want || got != tc.want {
				t.Fatalf("legacy=%x region=%x want=%x", base, got, tc.want)
			}
			if !profileEnabled && !regallocCheckEnabled && hits == 0 {
				t.Fatal("no region activation")
			}
			if (profileEnabled || regallocCheckEnabled) && hits != 0 {
				t.Fatal("diagnostic mode unexpectedly admitted region")
			}
		})
	}
}
func TestScalarRegionPressureLeavesOpcodeForReplay(t *testing.T) {
	scalarRegionEnable(t)
	f := scalarRegionFixture(mtI32)
	for _, r := range gpAlloc {
		f.reserved = f.reserved.add(r)
	}
	r := wasm.NewReader([]byte{0x20, 0, 0x41, 3, 0x6a, 0x41, 4, 0x6c})
	done, err := f.tryScalarRegion(r)
	if profileEnabled || regallocCheckEnabled {
		if done || err != nil || r.Offset() != 0 {
			t.Fatal("checked fallback changed reader")
		}
		return
	}
	if err != nil || !done || r.Offset() != 7 {
		t.Fatalf("done=%v err=%v offset=%d", done, err, r.Offset())
	}
	next, _ := r.Peek()
	if next != 0x6c || f.depth() != 2 {
		t.Fatal("failed pending evaluation consumed next ALU")
	}
	if f.scratchState().region.owned != 0 || f.scratchState().region.pending.valid {
		t.Fatal("pressure export leaked state")
	}
}
