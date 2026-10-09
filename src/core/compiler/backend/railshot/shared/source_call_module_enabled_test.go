//go:build wago_regalloccheck

package shared

import (
	"encoding/binary"
	"github.com/wago-org/wago/internal/regalloccheck"
	"github.com/wago-org/wago/src/core/compiler/codegen"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	x86 "github.com/wago-org/wago/src/core/encoder/amd64"
	"testing"
)

func sourceCallModuleFixture(t *testing.T) (codegen.Options, *wasm.Module) {
	t.Helper()
	caller := []byte{0x20, 0, 0x20, 1, 0x10, 2, 0x0b}
	m := &wasm.Module{Types: []wasm.RecType{{SubTypes: []wasm.SubType{{Comp: wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.I32, wasm.I32}, Results: []wasm.ValType{wasm.I32}}}}}}, FuncTypes: []wasm.TypeIdx{{}, {}, {}}, Code: []wasm.Func{{BodyBytes: caller}, {BodyBytes: caller}, {BodyBytes: []byte{0x20, 0, 0x0b}}}}
	var v wasm.ValidatedModuleAnalysis
	if err := wasm.ValidateModuleWithAnalysis(m, wasm.ValidationFeatures{}, 1, wasm.ValidationLimits{}, &v); err != nil {
		t.Fatal(err)
	}
	opts := codegen.SourceOptions(codegen.Options{}, m, &v, wasm.ValidationFeatures{})
	t.Cleanup(func() { codegen.CloseSourceContext(opts, m) })
	return opts, m
}

// A typed independent encoder fixture, with no observation callbacks. These
// images and all mutated controls are decoded only; they are never executed.
func sourceCallModuleImage(mode string) ([]byte, []int, []int) {
	var a x86.Asm
	a.MovReg64(x86.RBX, x86.RSI)
	a.Push(x86.RCX)
	a.Load64(x86.RAX, x86.RDI, 0)
	a.Load64(x86.RCX, x86.RDI, 8)
	adapter := a.CallRel32()
	a.Pop(x86.RCX)
	a.Store64(x86.RCX, 0, x86.RAX)
	a.Ret()
	entry, internal := make([]int, 3), make([]int, 3)
	var calls [2]int
	for i := 0; i < 2; i++ {
		internal[i] = a.Len()
		if i == 1 {
			entry[i] = a.Len()
		}
		a.SubRsp(24)
		a.Load64(x86.RSI, x86.RBX, -72)
		a.Cmp64(x86.RSP, x86.RSI)
		fence := a.JccPlaceholder(x86.CondB)
		input := x86.RAX
		if mode == "wrong argument" && i == 1 {
			input = x86.RCX
		}
		a.MovRegReg32(x86.R12, input)
		a.MovRegReg32(x86.R13, x86.RCX)
		a.MovRegReg32(x86.RDI, x86.R12)
		a.MovRegReg32(x86.RSI, x86.R13)
		off := int32(0)
		if mode == "frame bounds" && i == 1 {
			off = 24
		}
		a.Store32(x86.RSP, off, x86.R12)
		a.Store32(x86.RSP, 4, x86.R13)
		a.MovReg64(x86.RAX, x86.RDI)
		a.MovReg64(x86.RCX, x86.RSI)
		calls[i] = a.CallRel32()
		a.MovReg64(x86.RDI, x86.RAX)
		a.MovRegReg32(x86.RAX, x86.RDI)
		restore := int32(24)
		if mode == "SP balance" && i == 1 {
			restore = 32
		}
		a.AddRsp(restore)
		a.Ret()
		trap := a.Len()
		a.MovImm32(x86.RAX, 0)
		jump := a.JmpPlaceholder()
		a.PatchRel32(jump, a.Len())
		a.Load64(x86.RSI, x86.RBX, -104)
		ordinal := int32(i + 1)
		if mode == "wrong trap ordinal" && i == 1 {
			ordinal = 1
		}
		a.StoreImm32Mem(x86.RSI, 16, ordinal)
		a.Store32(x86.RSI, 20, x86.RAX)
		a.StoreImm32Mem(x86.RSI, 0, 13)
		a.Load64(x86.RSP, x86.RBX, -24)
		a.Ret()
		if mode == "wrong fence target" && i == 1 {
			trap++
		}
		a.PatchRel32(fence, trap)
	}
	entry[2], internal[2] = a.Len(), a.Len()
	a.SubRsp(0)
	a.MovRegReg32(x86.R9, x86.RAX)
	a.MovRegReg32(x86.R10, x86.RCX)
	result := x86.R9
	if mode == "wrong callee result" {
		result = x86.R10
	}
	a.MovRegReg32(x86.RAX, result)
	a.AddRsp(0)
	a.Ret()
	a.PatchRel32(adapter, internal[0])
	for i, call := range calls {
		target := internal[2]
		if mode == "wrong call target" && i == 1 {
			target = internal[0]
		}
		if mode == "mid-callee target" && i == 1 {
			target++
		}
		a.PatchRel32(call, target)
	}
	code := a.B
	switch mode {
	case "wrong adapter target":
		a.PatchRel32(adapter, internal[1])
	case "wrong map":
		internal[2]++
	case "aliased map":
		internal[1] = internal[0]
	case "entry map":
		entry[1]++
	case "wrong trap PC":
		binary.LittleEndian.PutUint32(code[internal[1]+66:], 1)
	case "wrong predicate":
		code[internal[1]+15] = 0x83
	case "changed fence offset":
		code[internal[1]+10] = 0xb0
	case "changed trap SP":
		code[internal[1]+98] = 0xe0
	case "wrong return control":
		code[len(code)-1] = 0x90
	case "extra island":
		code = append(code, 0x90)
	case "truncated":
		code = code[:len(code)-1]
	case "changed callee frame":
		binary.LittleEndian.PutUint32(code[internal[2]+3:], 8)
	}
	return code, entry, internal
}

func TestSourceCallFinalModuleControls(t *testing.T) {
	for _, tc := range []struct {
		name string
		want regalloccheck.Verdict
	}{
		{"positive without observers", regalloccheck.Verified}, {"wrong argument", regalloccheck.Rejected}, {"wrong callee result", regalloccheck.Rejected}, {"wrong call target", regalloccheck.Rejected}, {"mid-callee target", regalloccheck.Rejected}, {"wrong adapter target", regalloccheck.Rejected}, {"wrong map", regalloccheck.Rejected}, {"aliased map", regalloccheck.Rejected}, {"entry map", regalloccheck.Rejected}, {"frame bounds", regalloccheck.Rejected}, {"SP balance", regalloccheck.Rejected}, {"wrong fence target", regalloccheck.Rejected}, {"wrong trap ordinal", regalloccheck.Rejected},
		{"wrong trap PC", regalloccheck.Rejected}, {"wrong predicate", regalloccheck.Inconclusive}, {"changed fence offset", regalloccheck.Inconclusive}, {"changed trap SP", regalloccheck.Inconclusive}, {"wrong return control", regalloccheck.Inconclusive}, {"extra island", regalloccheck.Inconclusive}, {"truncated", regalloccheck.Inconclusive}, {"changed callee frame", regalloccheck.Inconclusive},
	} {
		t.Run(tc.name, func(t *testing.T) {
			opts, m := sourceCallModuleFixture(t)
			ctx := codegen.SourceContextFor(opts, m)
			code, entry, internal := sourceCallModuleImage(tc.name)
			r := VerifySourceCallModuleAMD64(ctx, m, code, entry, internal, true)
			if r.Verdict != tc.want {
				t.Fatalf("result=%+v code=%x", r, code)
			}
			again := VerifySourceCallModuleAMD64(ctx, m, code, entry, internal, true)
			if again.Reason != regalloccheck.ResourceLimit {
				t.Fatalf("retry=%+v", again)
			}
		})
	}
}

func TestSourceCallFinalMissingContext(t *testing.T) {
	_, m := sourceCallModuleFixture(t)
	code, entry, internal := sourceCallModuleImage("")
	if r := VerifySourceCallModuleAMD64(nil, m, code, entry, internal, true); r.Verdict != regalloccheck.Inconclusive {
		t.Fatal(r)
	}
}
