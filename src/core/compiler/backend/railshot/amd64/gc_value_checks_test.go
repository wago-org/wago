//go:build amd64

package amd64

import (
	"bytes"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	encamd64 "github.com/wago-org/wago/src/core/encoder/amd64"
	"os"
	"testing"
)

func BenchmarkGCValueChecksSparseBarriers(b *testing.B) {
	s := gcValueChecks{locals: make([]uint32, 1<<20), localRefs: []uint64{1}}
	f := fn{gcValues: &s}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if f.gcLocalValueID(0) == 0 {
			b.Fatal("missing local identity")
		}
		s.clear()
	}
}

func BenchmarkGCValueCheckPlanSparseBarriers(b *testing.B) {
	data, err := os.ReadFile("../../../../../../tests/fixtures/wasm/gc-value-checks.wasm")
	if err != nil {
		b.Fatal(err)
	}
	m, err := wasm.DecodeModule(data)
	if err != nil {
		b.Fatal(err)
	}
	f := fn{m: m, nLocals: 1 << 16, classifier: wasm.NewModuleInstructionClassifier(m, true)}
	body := bytes.Repeat([]byte{0x20, 0, 0x1a, 0x02, 0x40, 0x0b}, 1024)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if p := f.planGCValueChecks(body, 0); p != nil {
			b.Fatal("unexpected producers", p)
		}
	}
}

func TestGCValueCheckHomesPreserveLocalPacking(t *testing.T) {
	refs := encamd64.LocalRefRecorder{Sites: make([]encamd64.LocalRefSite, 0, 8)}
	if !refs.Reset(2, 8) {
		t.Fatal("cannot record local references")
	}
	f := fn{
		a: &encamd64.Asm{LocalRefs: &refs}, nLocals: 2,
		localType: []machineType{mtI64, mtI64, mtI64, mtI64},
		localSlot: []uint32{0, 128, 256, 264},
		gcValues:  &gcValueChecks{home: 2}, compactFrameHeader: true,
		stats: &CodegenStats{},
	}
	f.a.Store64(RSP, f.localAddr(1), RAX)
	for _, limit := range []bool{false, true} {
		f.a.Store64(RSP, f.gcValueHome(0, limit), RAX)
	}
	if refs.Overflow || refs.Pending {
		t.Fatalf("private cache homes damaged local recorder: %+v", refs)
	}
	if got := f.packLocalSlots(8); got != 1 {
		t.Fatalf("local swaps=%d, want 1 with private homes", got)
	}
	if f.localOff(1) != 0 || f.localOff(2) != 256 || f.localOff(3) != 264 {
		t.Fatalf("source/private homes moved incorrectly: %v", f.localSlot)
	}
}

func TestGCValueChecksFinalIntervalTransfer(t *testing.T) {
	s := gcValueChecks{locals: []uint32{7}, localRefs: []uint64{1}, serial: 7}
	f := fn{
		a: &encamd64.Asm{}, s: newStack(), gcValues: &s,
		localType: []machineType{mtI64}, gcLocalTypes: []uint64{1},
		locals:      []localDef{{reg: R12, typ: mtI64, state: lsReg}},
		intervalReg: []Reg{R12}, intervalLast: []uint32{1}, intervalActive: 1,
		pinnedLocalMask: maskOf(R12),
	}
	if err := f.emitPlain(wasm.NewReader([]byte{0}), 0x20); err != nil {
		t.Fatal(err)
	}
	if got := f.gcValueID(f.s.back()); got != 7 {
		t.Fatalf("final local transfer value ID=%d, want 7", got)
	}
}

func TestGCValueChecksUseValueVersions(t *testing.T) {
	s := gcValueChecks{locals: make([]uint32, 3), localRefs: []uint64{7}}
	f := fn{gcValues: &s}
	a := &elem{st: storage{kind: stLocalRef, typ: mtI64, idx: 0}}
	a.st.setGCRoot(true)
	first := f.gcValueID(a)
	if first == 0 || f.gcValueID(a) != first {
		t.Fatal("local identity is not stable")
	}
	s.setLocalValue(1, first)
	b := &elem{st: storage{kind: stLocalRef, typ: mtI64, idx: 1}}
	b.st.setGCRoot(true)
	if f.gcValueID(b) != first {
		t.Fatal("copied value lost identity")
	}
	s.setLocalValue(0, s.nextID())
	if f.gcValueID(a) == first || f.gcValueID(b) != first {
		t.Fatal("assignment changed the wrong value")
	}
	snapshot := &elem{st: storage{kind: stReg, typ: mtI64, cval: -int64(first)}}
	snapshot.st.setGCRoot(true)
	if f.gcValueID(snapshot) != first {
		t.Fatal("owned snapshot uses the new local value")
	}
	child := s.fieldValue(first, 7, 0)
	if child == first || child == 0 || child != s.fieldValue(first, 7, 0) {
		t.Fatal("field identity invalid")
	}
	if s.find(child, 7) != nil {
		t.Fatal("checked parent must not validate child")
	}
	s.clear()
	if s.locals[0] != 0 || s.locals[1] != 0 || len(s.touched) != 0 {
		t.Fatalf("local identities survived an effect: %v", s.locals)
	}
	if s.fieldValue(first, 7, 0) == child {
		t.Fatal("field identity survived effect")
	}
}

func TestGCValueCheckPlan(t *testing.T) {
	b, e := os.ReadFile("../../../../../../tests/fixtures/wasm/gc-value-checks.wasm")
	if e != nil {
		t.Fatal(e)
	}
	m, e := wasm.DecodeModule(b)
	if e != nil {
		t.Fatal(e)
	}
	if e = wasm.ValidateModule(m); e != nil {
		t.Fatal(e)
	}
	found := false
	for _, exp := range m.Exports {
		if exp.Name != "mix" {
			continue
		}
		found = true
		i := int(exp.Index.Index) - m.ImportedFuncCount()
		f := fn{m: m, nLocals: 3, classifier: wasm.NewModuleInstructionClassifier(m, true)}
		p := f.planGCValueChecks(m.Code[i].BodyBytes, m.Code[i].LocalDeclBytes)
		t.Logf("plan=%v", p)
		if len(p) < 2 {
			t.Fatal("interleaved sources have no reusable producers")
		}
	}
	if !found {
		t.Fatal("missing mix export")
	}
}
