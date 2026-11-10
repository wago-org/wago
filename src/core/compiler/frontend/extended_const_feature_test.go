package frontend

import (
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestExtendedConstFeatureGateCoversASTAndBytes(t *testing.T) {
	p := supportPass{feat: Features{}}
	for name, expr := range map[string]wasm.Expr{
		"AST":   {Instrs: []wasm.Instruction{{Kind: wasm.InstrI32Const}, {Kind: wasm.InstrI32Const}, {Kind: wasm.InstrI32Add}}},
		"bytes": {BodyBytes: []byte{0x41, 0x01, 0x41, 0x02, 0x6a, 0x0b}},
	} {
		t.Run(name, func(t *testing.T) {
			err := p.constExpr(expr, "global 0 initializer")
			if err == nil || !strings.Contains(err.Error(), "extended-constant-expressions disabled") {
				t.Fatalf("constExpr error = %v, want feature rejection", err)
			}
		})
	}
	all := supportPass{feat: AllFeatures()}
	if err := all.constExpr(wasm.Expr{BodyBytes: []byte{0x42, 0x02, 0x42, 0x03, 0x7e, 0x0b}}, "global 0 initializer"); err != nil {
		t.Fatalf("enabled extended const rejected: %v", err)
	}
}
