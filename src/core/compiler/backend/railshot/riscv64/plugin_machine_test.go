//go:build linux && !tinygo

package riscv64

import (
	"encoding/binary"
	rv "github.com/wago-org/wago/codegen/riscv64"
	railcore "github.com/wago-org/wago/src/core/compiler/backend/railshot"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
	"testing"
)

func TestPluginRV64NativeEmissionAndContextLifetime(t *testing.T) {
	sig := &wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.I32}, Results: []wasm.ValType{wasm.I32}}
	var retained rv.Context
	instruction := railcore.CustomInstruction{InputWidths: []int32{5}, ResultWidth: 3, Codegen: &rv.Lowering{Compatibility: rv.CompatibilityFullAccess, Emit: func(ctx rv.Context) error {
		retained = ctx
		r, err := ctx.InputI32(0)
		if err != nil {
			return err
		}
		ctx.Encoder().Addi(r, r, 5)
		return ctx.OutputI32(r)
	}}}
	f := &integerEmitter{target: target{header: 48, mode: emissionMode{customInstructions: pluginInstructions(map[uint32]railcore.CustomInstruction{0: instruction})}}}
	if !f.emitPluginCall(0, sig, 0, f.SpillOffset) || f.err != nil || f.a.Err() != nil || len(f.a.B) == 0 {
		t.Fatal("native emission", f.err, f.a.Err())
	}
	if _, err := retained.InputI32(0); err == nil {
		t.Fatal("retained plugin context used after callback")
	}
	if binary.LittleEndian.Uint32(f.a.B[len(f.a.B)-4:])&0x7f != 0x23 {
		t.Fatal("result not committed to canonical home")
	}
}
func TestPluginRV64RejectsReservedAndUnownedRegisters(t *testing.T) {
	f := &integerEmitter{}
	ctx := &pluginRV64Context{f: f, active: true}
	for _, r := range []enc.Reg{enc.Zero, enc.SP, enc.RA, enc.X24, enc.X25, enc.X26, enc.X27, enc.X28, enc.X29, enc.X30, enc.X31} {
		if ctx.ReserveGP(r) == nil {
			t.Fatal("runtime register admitted", r)
		}
	}
	if ctx.OutputI32(enc.X6) == nil {
		t.Fatal("unowned output admitted")
	}
	r := ctx.AllocGP()
	if ctx.OutputI32(r) != nil || ctx.OutputI32(r) == nil {
		t.Fatal("output assignment contract")
	}
	ctx.ReleaseGP(r)
	if ctx.owned&(1<<r) == 0 {
		t.Fatal("output prematurely released")
	}
}
