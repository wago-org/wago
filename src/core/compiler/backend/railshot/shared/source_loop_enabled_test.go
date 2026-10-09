//go:build wago_regalloccheck

package shared

import (
	"github.com/wago-org/wago/internal/regalloccheck"
	"github.com/wago-org/wago/src/core/compiler/codegen"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	x86 "github.com/wago-org/wago/src/core/encoder/amd64"
	"testing"
)

func sourceLoopFixture(t *testing.T) (*ScalarState, *wasm.Module) {
	t.Helper()
	m := &wasm.Module{Types: []wasm.RecType{{SubTypes: []wasm.SubType{{Comp: wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.I32, wasm.I32, wasm.I32}, Results: []wasm.ValType{wasm.I32}}}}}}, FuncTypes: []wasm.TypeIdx{{}}, Code: []wasm.Func{{BodyBytes: []byte{3, 0x40, 0x20, 1, 0x20, 0, 0x21, 1, 0x21, 0, 0x20, 2, 0x0d, 0, 0x0b, 0x20, 0, 0x0b}}}}
	a := new(wasm.ValidatedModuleAnalysis)
	if err := wasm.ValidateModuleWithAnalysis(m, wasm.ValidationFeatures{}, 1, wasm.ValidationLimits{}, a); err != nil {
		t.Fatal(err)
	}
	opts := codegen.SourceOptions(codegen.Options{}, m, a, wasm.ValidationFeatures{})
	s := new(ScalarState)
	SetSourceContext(s, codegen.SourceContextFor(opts, m))
	t.Cleanup(func() { s.FinishWorker(); codegen.CloseSourceContext(opts, m) })
	return s, m
}

// Images, including negative controls, are decoded only. A nonzero third
// parameter would keep this source loop running indefinitely.
func TestSourceLoopEncoderProofControls(t *testing.T) {
	for _, tc := range []struct {
		name string
		want regalloccheck.Verdict
	}{
		{"positive", regalloccheck.Verified}, {"padding", regalloccheck.Verified}, {"wide spill", regalloccheck.Verified}, {"dead scratch kill", regalloccheck.Verified},
		{"wrong entry", regalloccheck.Rejected}, {"duplicate pins", regalloccheck.Rejected}, {"duplicate swap source", regalloccheck.Rejected}, {"wrong swap order", regalloccheck.Rejected},
		{"backedge-only live kill", regalloccheck.Rejected}, {"wrong condition", regalloccheck.Rejected}, {"wrong exit", regalloccheck.Rejected},
		{"frame bounds", regalloccheck.Rejected}, {"SP balance", regalloccheck.Rejected},
		{"wrong predicate", regalloccheck.Inconclusive}, {"wrong header target", regalloccheck.Inconclusive}, {"missing observer", regalloccheck.Inconclusive}, {"unknown padding", regalloccheck.Inconclusive}, {"changed length", regalloccheck.Inconclusive},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, m := sourceLoopFixture(t)
			b := BeginSourceLoop(s, m, 0, true)
			if b == nil {
				t.Fatal("not admitted")
			}
			defer b.Close()
			var a x86.Asm
			a.ObserveRegalloc(b.ObserveEffect)
			a.ObserveGPWrites(b.ObserveGPWrites)
			if tc.name == "missing observer" {
				a.ObserveGPWrites(nil)
			}
			a.SubRsp(40)
			entry := x86.RAX
			if tc.name == "wrong entry" {
				entry = x86.RCX
			}
			a.MovRegReg32(x86.R9, entry)
			pin := x86.R10
			if tc.name == "duplicate pins" {
				pin = x86.R9
			}
			a.MovRegReg32(pin, x86.RCX)
			a.MovRegReg32(x86.R11, x86.RDX)
			if tc.name == "padding" {
				a.EmitBytes([]byte{0x66, 0x0f, 0x1f, 0x84, 0, 0, 0, 0, 0, 0x0f, 0x1f, 0x80, 0, 0, 0, 0})
			}
			if tc.name == "unknown padding" {
				a.EmitBytes([]byte{0x66, 0x91})
			}
			header := a.Len()
			swapSource := x86.R10
			if tc.name == "duplicate swap source" {
				swapSource = x86.R9
			}
			a.MovRegReg32(x86.RDI, swapSource)
			if tc.name == "wrong swap order" {
				a.MovRegReg32(x86.R9, x86.RDI)
				a.MovRegReg32(x86.R10, x86.R9)
			} else {
				a.MovRegReg32(x86.R10, x86.R9)
				a.MovRegReg32(x86.R9, x86.RDI)
			}
			if tc.name == "dead scratch kill" {
				a.XorSelf32(x86.RDI)
			}
			if tc.name == "backedge-only live kill" {
				a.XorSelf32(x86.R10)
			}
			off := int32(16)
			if tc.name == "frame bounds" {
				off = 40
			}
			condition := x86.R11
			if tc.name == "wrong condition" {
				condition = x86.R9
			}
			if tc.name == "wide spill" {
				a.Store64(x86.RSP, off, condition)
			} else {
				a.Store32(x86.RSP, off, condition)
			}
			a.Load32(x86.RDI, x86.RSP, off)
			a.TestSelf(x86.RDI, false)
			predicate := x86.CondNE
			if tc.name == "wrong predicate" {
				predicate = x86.CondE
			}
			backedge := a.JccPlaceholder(predicate)
			if tc.name == "padding" {
				a.EmitBytes([]byte{0x0f, 0x1f, 0x44, 0, 0})
			}
			result := x86.R9
			if tc.name == "wrong exit" {
				result = x86.R10
			}
			a.MovRegReg32(x86.RAX, result)
			restore := int32(40)
			if tc.name == "SP balance" {
				restore = 48
			}
			a.AddRsp(restore)
			a.Ret()
			if tc.name == "wrong header target" {
				header++
			}
			a.PatchRel32(backedge, header)
			b.EndEmission(a.Len())
			code := a.B
			if tc.name == "changed length" {
				code = code[:len(code)-1]
			}
			r := b.Verify(code, false)
			if r.Verdict != tc.want {
				t.Fatalf("result=%+v code=%x", r, code)
			}
			if b.Verify(code, false) != r {
				t.Fatal("cached verification changed")
			}
			if tc.name == "backedge-only live kill" {
				t.Logf("backedge result=%+v", r)
				// A straight-line single-iteration model still has the correct
				// returned entry value. Only carrying the killed local into a
				// later iteration makes it relevant to the returned value.
				decoded, ok := decodeSourceLoopAMD64(code)
				if !ok {
					t.Fatal("control stopped decoding")
				}
				flat := regalloccheck.Graph{Widths: []uint8{4, 4, 4}, Blocks: make([]regalloccheck.Block, 1), Inputs: []regalloccheck.Binding{{Location: leafReg(0), Value: 1}, {Location: leafReg(1), Value: 2}, {Location: leafReg(2), Value: 3}}}
				for _, in := range decoded.instructions {
					if in.copy {
						e := in.effect
						if e.Dst.Bank == regalloccheck.GP && e.Size == 4 {
							e.ClearTo = 8
						}
						flat.Blocks[0].Operations = append(flat.Blocks[0].Operations, regalloccheck.Operation{Kind: regalloccheck.Machine, Effect: e})
					} else if in.writes != 0 {
						for reg := uint8(0); reg < 32; reg++ {
							if in.writes&(1<<reg) != 0 {
								flat.Blocks[0].Operations = append(flat.Blocks[0].Operations, regalloccheck.Operation{Kind: regalloccheck.Machine, Effect: regalloccheck.Effect{Kind: regalloccheck.Kill, Dst: leafReg(reg), Size: 8}})
							}
						}
					}
				}
				flat.Blocks[0].Operations = append(flat.Blocks[0].Operations, regalloccheck.Operation{Kind: regalloccheck.Use, Location: leafReg(0), Value: 2, Where: "one iteration return"})
				if once := flat.Verify(regalloccheck.Limits{Blocks: 1, Values: 3, Operations: 128, Facts: 4096, Work: 65536}); once.Verdict != regalloccheck.Verified {
					t.Fatalf("single iteration=%+v", once)
				}
			}
			journal, ledger, token := b.journal, b.ledger, b.attempt
			work, storage := s.sourceWork, s.sourceStorage
			b.Close()
			b.Close()
			if b.owner != nil || journal.Finalize(0, 0, nil).State == regalloccheck.JournalReady || ledger.EventCount() != 0 || token.owner != nil || s.sourceAttempt != nil || s.sourceWork != work || s.sourceStorage != storage {
				t.Fatal("retained proof or refunded history")
			}
		})
	}
}

func TestSourceLoopQuotaAndAbandonment(t *testing.T) {
	for _, kind := range []string{"work", "storage", "abandon", "retry"} {
		t.Run(kind, func(t *testing.T) {
			s, m := sourceLoopFixture(t)
			if kind == "work" {
				s.sourceWork = 0
			}
			if kind == "storage" {
				s.sourceStorage = 0
			}
			b := BeginSourceLoop(s, m, 0, true)
			if kind == "work" || kind == "storage" {
				if b != nil || s.sourceAttempt != nil {
					t.Fatal("exhaustion admitted proof")
				}
				return
			}
			if b == nil {
				t.Fatal("not admitted")
			}
			storage := s.sourceStorage
			b.Close()
			if s.sourceAttempt != nil || s.sourceStorage != storage {
				t.Fatal("retained/refunded abandoned attempt")
			}
			if kind == "retry" {
				next := BeginSourceLoop(s, m, 0, true)
				if next == nil || s.sourceStorage != storage-8192 {
					t.Fatal("retry refunded quota")
				}
				next.Close()
			}
		})
	}
}
