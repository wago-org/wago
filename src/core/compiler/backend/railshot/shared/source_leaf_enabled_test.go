//go:build wago_regalloccheck

package shared

import (
	"github.com/wago-org/wago/internal/regalloccheck"
	"github.com/wago-org/wago/src/core/compiler/codegen"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	x86 "github.com/wago-org/wago/src/core/encoder/amd64"
	a64 "github.com/wago-org/wago/src/core/encoder/arm64"
	"testing"
)

func sourceLeafFixture(t *testing.T, wide bool) (*ScalarState, *wasm.Module, codegen.Options) {
	t.Helper()
	typ, op := wasm.I32, byte(0x6a)
	if wide {
		typ, op = wasm.I64, 0x7c
	}
	m := &wasm.Module{Types: []wasm.RecType{{SubTypes: []wasm.SubType{{Comp: wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{typ, typ}, Results: []wasm.ValType{typ}}}}}}, FuncTypes: []wasm.TypeIdx{{}}, Code: []wasm.Func{{BodyBytes: []byte{0x20, 0, 0x20, 1, op, 0x0b}}}}
	a := new(wasm.ValidatedModuleAnalysis)
	if err := wasm.ValidateModuleWithAnalysis(m, wasm.ValidationFeatures{}, 1, wasm.ValidationLimits{}, a); err != nil {
		t.Fatal(err)
	}
	opts := codegen.SourceOptions(codegen.Options{}, m, a, wasm.ValidationFeatures{})
	codegen.SetSourceReporter(opts, m, func(r codegen.SourceReport) { t.Logf("source result %+v", r) })
	s := new(ScalarState)
	SetSourceContext(s, codegen.SourceContextFor(opts, m))
	t.Cleanup(func() { s.FinishWorker(); codegen.CloseSourceContext(opts, m) })
	return s, m, opts
}

// Faulty output is compiled and inspected only. The control uses the same
// encoders and source contracts as each negative, without fault execution.
func TestSourceLeafEncoderProofControls(t *testing.T) {
	for _, arm := range []bool{false, true} {
		for _, wide := range []bool{false, true} {
			for _, test := range []struct {
				name    string
				verdict regalloccheck.Verdict
			}{
				{"positive", regalloccheck.Verified}, {"commuted", regalloccheck.Verified},
				{"wrong input", regalloccheck.Rejected}, {"late clobber", regalloccheck.Rejected},
				{"wrong return", regalloccheck.Rejected}, {"wrong width", regalloccheck.Rejected},
				{"wrong operator", regalloccheck.Rejected}, {"missing observer", regalloccheck.Inconclusive},
				{"raw gap", regalloccheck.Inconclusive}, {"changed length", regalloccheck.Inconclusive},
				{"neutral frame rewrite", regalloccheck.Verified}, {"bad frame rewrite", regalloccheck.Inconclusive},
			} {
				t.Run(test.name+map[bool]string{true: "/arm", false: "/amd"}[arm]+map[bool]string{true: "/i64", false: "/i32"}[wide], func(t *testing.T) {
					s, m, _ := sourceLeafFixture(t, wide)
					leaf := BeginSourceLeaf(s, m, 0, true)
					if leaf == nil {
						t.Fatal("source leaf not admitted")
					}
					defer leaf.Close()
					var code []byte
					if arm {
						var a a64.Asm
						a.ObserveRegalloc(leaf.ObserveEffect)
						a.ObserveGPWrites(leaf.ObserveGPWrites)
						if test.name == "missing observer" {
							a.ObserveGPWrites(nil)
						}
						a.Movz64(a64.X16, 0, 0)
						a.Movk64(a64.X16, 0, 1)
						a.SubSPReg(a64.X16)
						if !wide {
							a.MovReg32(a64.X0, a64.X0)
							a.MovReg32(a64.X1, a64.X1)
						}
						if test.name == "wrong input" {
							a.MovReg64(a64.X1, a64.X0)
						}
						dst, left, right := a64.X0, a64.X0, a64.X1
						if test.name == "wrong return" {
							dst = a64.X2
						}
						if test.name == "commuted" {
							left, right = right, left
						}
						w := wide
						if test.name == "wrong width" {
							w = !w
						}
						if test.name == "wrong operator" {
							if w {
								a.Eor64(dst, left, right)
							} else {
								a.Eor32(dst, left, right)
							}
						} else {
							if w {
								a.Add64(dst, left, right)
							} else {
								a.Add32(dst, left, right)
							}
						}
						if test.name == "late clobber" {
							a.MovReg64(a64.X0, a64.X1)
						}
						if test.name == "raw gap" {
							a.B = append(a.B, 0, 0, 0, 0)
						}
						a.Movz64(a64.X16, 0, 0)
						a.Movk64(a64.X16, 0, 1)
						a.AddSPReg(a64.X16)
						a.Ret()
						code = a.B
					} else {
						var a x86.Asm
						a.ObserveRegalloc(leaf.ObserveEffect)
						a.ObserveGPWrites(leaf.ObserveGPWrites)
						if test.name == "missing observer" {
							a.ObserveGPWrites(nil)
						}
						a.SubRsp(0)
						move := func(d, r x86.Reg) {
							if wide {
								a.MovReg64(d, r)
							} else {
								a.MovRegReg32(d, r)
							}
						}
						move(x86.R9, x86.RAX)
						move(x86.R10, x86.RCX)
						if test.name == "wrong input" {
							move(x86.R10, x86.R9)
						}
						dst, left, right := x86.RAX, x86.R9, x86.R10
						if test.name == "wrong return" {
							dst = x86.R8
						}
						if test.name == "commuted" {
							left, right = right, left
						}
						move(dst, left)
						w := wide
						if test.name == "wrong width" {
							w = !w
						}
						op := byte(1)
						if test.name == "wrong operator" {
							op = 0x31
						}
						a.AluRR(op, dst, right, w)
						if test.name == "late clobber" {
							move(x86.RAX, x86.RCX)
						}
						if test.name == "raw gap" {
							a.EmitBytes([]byte{0x90})
						}
						a.AddRsp(0)
						a.Ret()
						code = a.B
					}
					leaf.EndEmission(len(code))
					if test.name == "neutral frame rewrite" || test.name == "bad frame rewrite" {
						if arm {
							for _, pc := range []int{0, 4, 8, len(code) - 16, len(code) - 12, len(code) - 8} {
								copy(code[pc:], []byte{0x1f, 0x20, 0x03, 0xd5})
							}
							if test.name == "bad frame rewrite" {
								code[4] = 0
							}
						} else if test.name == "bad frame rewrite" {
							code[3] = 8
						}
					}
					if test.name == "changed length" {
						code = code[:len(code)-1]
					}
					result := leaf.Verify(code, arm)
					if result.Verdict != test.verdict {
						t.Fatalf("result=%+v code=%x", result, code)
					}
					if again := leaf.Verify(code, arm); again != result {
						t.Fatalf("changed final report: %+v %+v", result, again)
					}
					journal := leaf.journal
					ledger := leaf.attempt.ledger
					work, storage := s.sourceWork, s.sourceStorage
					leaf.Close()
					leaf.Close()
					if s.sourceAttempt != nil || ledger.ValueCount() != 0 || journal.Result().State != regalloccheck.JournalClosed {
						t.Fatal("retained attempt storage")
					}
					if s.sourceWork != work || s.sourceStorage != storage {
						t.Fatal("cleanup refunded history")
					}
				})
			}
		}
	}
}

func TestSourceLeafAggregateBudgetExhaustion(t *testing.T) {
	s, m, _ := sourceLeafFixture(t, false)
	s.sourceWork = 0
	if BeginSourceLeaf(s, m, 0, true) != nil || s.sourceAttempt != nil {
		t.Fatal("zero budget selected defaults")
	}
	s.sourceWork = 1024
	s.sourceStorage = 2047
	if BeginSourceLeaf(s, m, 0, true) != nil || s.sourceAttempt != nil || s.sourceWork != 992 {
		t.Fatal("storage exhaustion allocated attempt")
	}
	s.sourceStorage = 2048
	l := BeginSourceLeaf(s, m, 0, true)
	if l == nil {
		t.Fatal("bounded attempt unavailable")
	}
	l.Close()
	if BeginSourceLeaf(s, m, 0, true) != nil || s.sourceStorage != 0 {
		t.Fatal("retry replenished historical storage")
	}
}
