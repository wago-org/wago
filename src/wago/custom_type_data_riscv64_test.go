//go:build linux && riscv64 && !tinygo

package wago

import (
	"fmt"
	rv "github.com/wago-org/wago/codegen/riscv64"
)

type rv64WideDataExtension struct {
	size    int32
	carrier WasmType
}

func (e rv64WideDataExtension) Info() ExtensionInfo {
	return ExtensionInfo{ID: "test.rv64-wide-data", Version: "1.0.0", Stability: Experimental}
}
func rv64WideCell(seed, cell int) uint64 {
	return uint64(seed*131+cell*17+1)<<32 | uint64(seed*37+cell*13+7)
}
func rv64WideSum(size int32, seed int) uint64 {
	var sum uint64
	for i := 0; i < int(size/8); i++ {
		sum ^= rv64WideCell(seed, i)
	}
	return uint64(uint32(sum ^ (sum >> 32)))
}
func (e rv64WideDataExtension) Register(reg *Registry) error {
	reg.Capability(CapCompilerCodegen)
	compiler := reg.Compiler()
	typ, err := compiler.Type(CustomTypeSpec{Name: "test.rv64-wide-data", Size: e.size, Carrier: e.carrier})
	if err != nil {
		return err
	}
	for seed := 1; seed <= 2; seed++ {
		seed := seed
		if err := compiler.Instruction(InstructionSpec{Module: "wide", Name: fmt.Sprintf("new%d", seed), Output: []int32{e.size * 8}, Custom: &CustomSignature{Output: &typ}, Codegen: &rv.Lowering{Compatibility: rv.CompatibilityFullAccess, Emit: func(ctx rv.Context) error {
			if e.size > 128 {
				if e.size > 256 {
					address, err := ctx.OutputCustomAddress()
					if err != nil {
						return err
					}
					value := ctx.AllocGP()
					for i := 0; i < int(e.size/8); i++ {
						ctx.Encoder().Constant64(value, int64(rv64WideCell(seed, i)))
						ctx.Encoder().Sd(value, address, 0)
						ctx.Encoder().Addi(address, address, 8)
					}
					ctx.ReleaseGP(value)
					ctx.ReleaseGP(address)
					return nil
				}
				for i := 0; i < int(e.size/8); i++ {
					value := ctx.AllocGP()
					ctx.Encoder().Constant64(value, int64(rv64WideCell(seed, i)))
					if err := ctx.OutputCustomCell(i, value); err != nil {
						return err
					}
					ctx.ReleaseGP(value)
				}
				return nil
			}
			cells := make([]rv.Reg, e.size/8)
			for i := range cells {
				cells[i] = ctx.AllocGP()
				ctx.Encoder().Constant64(cells[i], int64(rv64WideCell(seed, i)))
			}
			return ctx.OutputCustom(cells...)
		}}}); err != nil {
			return err
		}
	}
	if err := compiler.Instruction(InstructionSpec{Module: "wide", Name: "sum", Input: []int32{e.size * 8}, Output: []int32{32}, Custom: &CustomSignature{Inputs: []CustomType{typ}}, Codegen: &rv.Lowering{Compatibility: rv.CompatibilityFullAccess, Emit: func(ctx rv.Context) error {
		if e.size > 128 {
			acc := ctx.AllocGP()
			ctx.Encoder().Addi(acc, rv.Zero, 0)
			if e.size > 256 {
				address, err := ctx.InputCustomAddress(0)
				if err != nil {
					return err
				}
				value := ctx.AllocGP()
				for i := 0; i < int(e.size/8); i++ {
					ctx.Encoder().Ld(value, address, 0)
					ctx.Encoder().Xor(acc, acc, value)
					ctx.Encoder().Addi(address, address, 8)
				}
				ctx.ReleaseGP(value)
				ctx.ReleaseGP(address)
			} else {
				for i := 0; i < int(e.size/8); i++ {
					value, err := ctx.InputCustomCell(0, i)
					if err != nil {
						return err
					}
					ctx.Encoder().Xor(acc, acc, value)
					ctx.ReleaseGP(value)
				}
			}
			hi := ctx.AllocGP()
			ctx.Encoder().Srli(hi, acc, 32)
			ctx.Encoder().Xor(acc, acc, hi)
			ctx.ReleaseGP(hi)
			return ctx.OutputI32(acc)
		}
		cells, err := ctx.InputCustom(0)
		if err != nil {
			return err
		}
		acc := cells[0]
		for _, cell := range cells[1:] {
			ctx.Encoder().Xor(acc, acc, cell)
			ctx.ReleaseGP(cell)
		}
		hi := ctx.AllocGP()
		ctx.Encoder().Srli(hi, acc, 32)
		ctx.Encoder().Xor(acc, acc, hi)
		ctx.ReleaseGP(hi)
		return ctx.OutputI32(acc)
	}}}); err != nil {
		return err
	}
	return compiler.Instruction(InstructionSpec{Module: "wide", Name: "id", Input: []int32{e.size * 8}, Output: []int32{e.size * 8}, Custom: &CustomSignature{Inputs: []CustomType{typ}, Output: &typ}, Codegen: &rv.Lowering{Compatibility: rv.CompatibilityManaged, Managed: func(ctx rv.ManagedContext) error {
		for i := 0; i < int(e.size/8); i++ {
			value, err := ctx.InputCustomCell(0, i)
			if err != nil {
				return err
			}
			if err := ctx.OutputCustomCell(i, value); err != nil {
				return err
			}
			ctx.ReleaseGP(value)
		}
		return nil
	}}})
}
