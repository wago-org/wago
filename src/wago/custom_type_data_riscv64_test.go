//go:build linux && riscv64 && !tinygo

package wago

import (
	"context"
	"fmt"
	rv "github.com/wago-org/wago/codegen/riscv64"
	"os"
	"path/filepath"
	"testing"
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

func TestRV64CustomWideDataSourceArtifact(t *testing.T) {
	mode, dir := os.Getenv("WAGO_RV64_CUSTOM_ARTIFACT_MODE"), os.Getenv("WAGO_RV64_CUSTOM_ARTIFACT_DIR")
	carriers := []struct {
		typ  WasmType
		wire byte
	}{{WasmI32, 0x7f}, {WasmI64, 0x7e}, {WasmF32, 0x7d}, {WasmF64, 0x7c}, {WasmV128, 0x7b}, {WasmFuncRef, 0x70}, {WasmExternRef, 0x6f}}
	for _, size := range []int32{32, 64, 128, 256, 512} {
		for _, carrier := range carriers {
			t.Run(fmt.Sprintf("size%d-carrier%x", size, carrier.wire), func(t *testing.T) {
				rt := NewRuntime()
				defer rt.Close()
				if err := rt.Use(rv64WideDataExtension{size: size, carrier: carrier.typ}); err != nil {
					t.Fatal(err)
				}
				bodies := []struct {
					name string
					body []byte
					seed int
				}{
					{"direct", []byte{0x10, 0, 0x10, 2, 0x0b}, 1},
					{"managed-identity", []byte{0x10, 0, 0x10, 3, 0x10, 2, 0x0b}, 1},
					{"block-params", []byte{0x10, 0, 0x02, 3, 0x0b, 0x10, 2, 0x0b}, 1},
					{"loop-params", []byte{0x10, 0, 0x03, 3, 0x0b, 0x10, 2, 0x0b}, 1},
					{"branch", []byte{0x02, carrier.wire, 0x10, 0, 0x10, 1, 0x0c, 0, 0x0b, 0x10, 2, 0x0b}, 2},
					{"branch-table", []byte{0x02, carrier.wire, 0x10, 0, 0x10, 1, 0x41, 0, 0x0e, 1, 0, 0, 0x0b, 0x10, 2, 0x0b}, 2},
					{"join-then", []byte{0x41, 1, 0x04, carrier.wire, 0x10, 0, 0x05, 0x10, 1, 0x0b, 0x10, 2, 0x0b}, 1},
					{"join-else", []byte{0x41, 0, 0x04, carrier.wire, 0x10, 0, 0x05, 0x10, 1, 0x0b, 0x10, 2, 0x0b}, 2},
					{"select-first", []byte{0x10, 0, 0x10, 1, 0x41, 1, 0x1c, 1, carrier.wire, 0x10, 2, 0x0b}, 1},
					{"select-second", []byte{0x10, 0, 0x10, 1, 0x41, 0, 0x1c, 1, carrier.wire, 0x10, 2, 0x0b}, 2},
					{"spills", []byte{0x10, 0, 0x10, 1, 0x10, 1, 0x10, 1, 0x10, 2, 0x1a, 0x10, 2, 0x1a, 0x10, 2, 0x1a, 0x10, 2, 0x0b}, 1},
				}
				mixed := []byte{0x10, 0}
				for range 255 {
					mixed = append(mixed, 0x41, 1)
				}
				for range 255 {
					mixed = append(mixed, 0x1a)
				}
				mixed = append(mixed, 0x10, 2, 0x0b)
				bodies = append(bodies, struct {
					name string
					body []byte
					seed int
				}{"mixed-scalars", mixed, 1})
				for _, test := range bodies {
					t.Run(test.name, func(t *testing.T) {
						var blob []byte
						var err error
						entries := make([]struct {
							name string
							mod  *Module
						}, 0, 2)
						artifactPath := filepath.Join(dir, fmt.Sprintf("size%d-carrier%x-%s.wago", size, carrier.wire, test.name))
						if mode == "load" {
							blob, err = os.ReadFile(artifactPath)
						} else {
							var mod *Module
							mod, blob = rv64PrepareWideData(t, rt, carrier.wire, test.body)
							defer mod.Close()
							entries = append(entries, struct {
								name string
								mod  *Module
							}{"source", mod})
							if mode == "prepare" {
								err = os.WriteFile(artifactPath, blob, 0600)
							}
						}
						if err != nil {
							t.Fatal(err)
						}
						loaded, err := LoadTrustedArtifact(blob)
						if err != nil {
							t.Fatal(err)
						}
						loadedMod, err := rt.AdoptModule(loaded)
						if err != nil {
							t.Fatal(err)
						}
						defer loadedMod.Close()
						entries = append(entries, struct {
							name string
							mod  *Module
						}{"artifact", loadedMod})
						for _, entry := range entries {
							t.Run(entry.name, func(t *testing.T) {
								in, err := rt.Instantiate(context.Background(), entry.mod)
								if err != nil {
									t.Fatal(err)
								}
								defer in.Close()
								got, err := in.Invoke("run")
								if err != nil || len(got) != 1 || got[0] != rv64WideSum(size, test.seed) {
									t.Fatalf("sum=%x error=%v want=%x", got, err, rv64WideSum(size, test.seed))
								}
							})
						}
					})
				}
			})
		}
	}
}
