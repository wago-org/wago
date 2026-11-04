//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"slices"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	encoder "github.com/wago-org/wago/src/core/encoder/riscv64"
)

func rv64TailSymbolicFixture(t *testing.T, kind uint32, vector bool) *Compiled {
	t.Helper()
	values := []wasm.ValType{wasm.I32, wasm.F32, wasm.I64, wasm.F64}
	if vector {
		values = append(values, wasm.V128)
	}
	base := rv64NativeTailCompile(t, "c2p-symbolic", rv64PublicTailModule(values, false), false)
	c := cloneCompiledMetadata(base)
	c.validateMemo, c.codeCache = &validateMemo{}, nil
	d := DefinedTypeDescriptor{Kind: CompositeTypeFunction, Final: true}
	for _, v := range c.importFuncSigs[0].Params {
		switch v {
		case ValI32:
			d.Params = append(d.Params, ValueTypeDescriptor{Kind: ValueTypeI32})
		case ValI64:
			d.Params = append(d.Params, ValueTypeDescriptor{Kind: ValueTypeI64})
		case ValF32:
			d.Params = append(d.Params, ValueTypeDescriptor{Kind: ValueTypeF32})
		case ValF64:
			d.Params = append(d.Params, ValueTypeDescriptor{Kind: ValueTypeF64})
		case ValV128:
			d.Params = append(d.Params, ValueTypeDescriptor{Kind: ValueTypeV128})
		default:
			t.Fatal("unexpected fixture signature")
		}
	}
	d.Results = slices.Clone(d.Params)
	c.Types = append(slices.Clone(c.Types), DefinedTypeDescriptor{Kind: CompositeTypeFunction, Final: true}, d)
	p := rv64NativeTailModule(base).Plans[0]
	p.Kind, p.TypeIndex = kind, uint32(len(c.Types)-1)
	p.ImportIndex = 0
	if vector {
		p.SourceStride = 16
	}
	p.ReferenceBase = p.SourceBase + len(d.Params)*p.SourceStride
	p.FrameBytes = (p.ReferenceBase + 8 + 15) &^ 15
	c.NeedsFuncRefDescs = true
	if kind == encoder.NativeTailIndirect {
		c.HasTable, c.TableType, c.TableSize = true, ValFuncRef, 1
	}
	if kind == encoder.NativeTailReference {
		c.requiredFeatures |= CoreFeatureTypedFunctionReferences
	}
	rv64StoreNativeTailModule(c, &encoder.NativeTailModule{Plans: []encoder.NativeTailPlan{p}})
	if err := rv64ValidateNativeTail(c); err != nil {
		t.Fatal("valid symbolic fixture", err)
	}
	return c
}

func rv64TailCodecView(c *Compiled) *Compiled {
	v := *c
	v.validateMemo, v.codeCache = &validateMemo{}, nil
	return &v
}
