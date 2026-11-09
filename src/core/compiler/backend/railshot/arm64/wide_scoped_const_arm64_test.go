//go:build arm64

package arm64

import (
	"bytes"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	a64 "github.com/wago-org/wago/src/core/encoder/arm64"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func wideScopedConstFixture(t testing.TB, table bool) *wasm.Module {
	b := []byte{2, 1, 0x7e, 70, 0x7f, 0x41, 5, 0x21, 2, 0x20, 0, 0x21, 3, 0x02, 0x40, 0x20, 0, 0x45, 0x0d, 0, 0x03, 0x40, 0x20, 1, 0x20, 2, 0xad, 0x42}
	b = append(b, wasmtest.SLEB64(-4417276706812531889)...)
	b = append(b, 0x7e, 0x7c, 0x42)
	b = append(b, wasmtest.SLEB64(-7046029288634856825)...)
	b = append(b, 0x85, 0x21, 1, 0x20, 2, 0x41, 3, 0x6a, 0x21, 2, 0x20, 3, 0x41, 1, 0x6b, 0x22, 3, 0x0d, 0, 0x0b, 0x0b, 0x20, 1, 0x10, 1, 0x0b)
	m := modFuncs(t, funcDef{params: []wasm.ValType{wasm.I32}, results: []wasm.ValType{wasm.I64}, body: b}, funcDef{params: []wasm.ValType{wasm.I64}, results: []wasm.ValType{wasm.I64}, body: []byte{0, 0x20, 0, 0x0b}})
	if table {
		m.Tables = []wasm.Table{{Type: wasm.TableType{Ref: wasm.AbsRef(wasm.HeapFunc)}}}
	}
	if e := wasm.ValidateModule(m); e != nil {
		t.Fatal(e)
	}
	return m
}
func TestWideScopedConstExactExecution(t *testing.T) {
	old := wideScopedLoopConstsEnabled
	defer func() { wideScopedLoopConstsEnabled = old }()
	for _, table := range []bool{false, true} {
		m := wideScopedConstFixture(t, table)
		for _, on := range []bool{false, true} {
			wideScopedLoopConstsEnabled = on
			for _, compact := range []bool{false, true} {
				for _, n := range []uint64{0, 1, 2, 3, 7, 16, 127, 4097} {
					var want uint64
					induction := uint32(5)
					for i := uint64(0); i < n; i++ {
						want = (want + uint64(induction)*0xc2b2ae3d27d4eb4f) ^ 0x9e3779b185ebca87
						induction += 3
					}
					opts := CompileOptions{CompactNative: compact, Optimizations: map[string]bool{"inline": false, "loop-int-const": true, "scoped-loop-int-const": true}}
					got, e := runArm64WrapperWithOptions(t, m, opts, n)
					if e != nil || got != want {
						t.Fatalf("table%v on%v compact%v n%d got%x want%x err%v", table, on, compact, n, got, want, e)
					}

				}
			}
		}
	}
}
func TestWideScopedConstProbeLimitsAndTableBarrier(t *testing.T) {
	old := wideScopedLoopConstsEnabled
	wideScopedLoopConstsEnabled = true
	defer func() { wideScopedLoopConstsEnabled = old }()
	m := wideScopedConstFixture(t, true)
	classifier := wasm.NewModuleInstructionClassifier(m, true)
	hints := scopedLoopConstHints{loopIntConstCount: 1, loopIntConstTypes: 2, loopIntConst: [4]int64{-4417276706812531889}}
	literal := append([]byte{0x42}, wasmtest.SLEB64(hints.loopIntConst[0])...)
	literal = append(literal, 0x7e, 0x1a)
	for _, helper := range [][]byte{{0x25, 0}, {0x26, 0}, {0xfc, 16, 0}, {0xfc, 15, 0}, {0xfb, 0}, {0xfe, 0}} {
		code := append(append([]byte{}, literal...), helper...)
		code = append(code, 0x0b)
		work := uint16(0)
		uses := scopedLoopConstUses{hints: &hints, work: &work}
		r := wasm.ReaderFrom(code)
		scanLoopSetLocals(&r, classifier, nil, &uses)
		if !uses.blocked || uses.mask != 0 || r.Offset() != 0 {
			t.Fatalf("helper%x blocked%v mask%d offset%d", helper, uses.blocked, uses.mask, r.Offset())
		}
	}
	for _, initial := range []uint16{0, scopedLoopConstWorkLimit - 1, scopedLoopConstWorkLimit} {
		code := make([]byte, 514)
		for i := range code {
			code[i] = 1
		}
		code[len(code)-1] = 0x0b
		work := initial
		uses := scopedLoopConstUses{hints: &hints, work: &work}
		r := wasm.ReaderFrom(code)
		if set, call := scanLoopSetLocals(&r, classifier, nil, &uses); set != nil || !call || r.Offset() != 0 || work > scopedLoopConstWorkLimit {
			t.Fatalf("budget%d work%d offset%d call%v", initial, work, r.Offset(), call)
		}
	}
}

func TestWideScopedConstAdmission(t *testing.T) {
	requireCompilerDiagnostics(t)
	old := wideScopedLoopConstsEnabled
	defer func() { wideScopedLoopConstsEnabled = old }()
	for _, on := range []bool{false, true} {
		wideScopedLoopConstsEnabled = on
		stats := &ModuleStats{}
		cm, e := CompileModuleWith(wideScopedConstFixture(t, true), CompileOptions{Stats: stats, Optimizations: map[string]bool{"inline": false, "loop-int-const": true, "scoped-loop-int-const": true}})
		if e != nil {
			t.Fatal(e)
		}
		cm.CodeImage.Close()
		if hit := stats.Funcs[0].Peephole["scoped-loop-int-const"]; (hit != 0) != on {
			t.Fatalf("on%v leases%d", on, hit)
		}
	}
}

func TestWideScopedConstPrefixBudgetAndReader(t *testing.T) {
	old := wideScopedLoopConstsEnabled
	wideScopedLoopConstsEnabled = true
	defer func() { wideScopedLoopConstsEnabled = old }()
	m := wideScopedConstFixture(t, true)
	body := m.Code[0].BodyBytes
	at := bytes.Index(body, []byte{0x03, 0x40})
	if at < 0 {
		t.Fatal("missing loop")
	}
	code := body[at+2:]
	f := fn{m: m, nLocals: 73, usesCalls: true, policy: currentCodegenPolicy(), classifier: wasm.NewModuleInstructionClassifier(m, true), scopedConstHints: scopedLoopConstHints{loopIntConstCount: 1, loopIntConstTypes: 2, loopIntConst: [4]int64{-4417276706812531889}}}
	f.integerResidualWork = integerResidualWorkLimit
	var admitted bool
	allocs := testing.AllocsPerRun(100, func() {
		f.scopedConstWork = 0
		r := wasm.ReaderFrom(code)
		admitted = f.hasWideScopedConstPrefix(&r)
		if r.Offset() != 0 {
			panic("prefix advanced reader")
		}
	})
	if !admitted || allocs != 0 {
		t.Fatalf("admitted%v allocations%v", admitted, allocs)
	}
	f.scopedConstWork = scopedLoopConstWorkLimit
	r := wasm.ReaderFrom(code)
	if f.hasWideScopedConstPrefix(&r) || r.Offset() != 0 || f.scopedConstWork != scopedLoopConstWorkLimit {
		t.Fatal("exhausted prefix budget admitted")
	}
	f.scopedConstWork = 0
	nops := bytes.Repeat([]byte{1}, 17)
	r = wasm.ReaderFrom(append(nops, code...))
	if f.hasWideScopedConstPrefix(&r) || r.Offset() != 0 || f.scopedConstWork != 16 {
		t.Fatal("prefix depth limit failed")
	}
}

func TestWideScopedConstPreservesLiveGP(t *testing.T) {
	old := wideScopedLoopConstsEnabled
	wideScopedLoopConstsEnabled = true
	defer func() { wideScopedLoopConstsEnabled = old }()
	f := fn{a: &a64.Asm{}, policy: currentCodegenPolicy()}
	f.pinnedLocalMask = maskOf(X25).union(maskOf(X24)).union(maskOf(X23)).union(maskOf(X27))
	owner := &elem{}
	f.regUser[X14] = owner
	f.regUser[X13] = owner
	h := funcHintView{loopIntConstCount: 1, loopIntConstTypes: 2, loopIntConst: [4]int64{-4417276706812531889}}
	f.reserveLoopIntConsts(&h)
	if f.iconstN != 0 || f.regUser[X14] != owner || f.regUser[X13] != owner {
		t.Fatal("leased an occupied register")
	}
}
