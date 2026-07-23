# Compiler instruction plugins

Wago compiler plugins can recognize ordinary Wasm function imports and replace
their calls during native code generation. This gives every source language
that can emit an `i32` import the same extension mechanism without custom Wasm
types, custom sections, binary rewriting, or compiler-specific metadata.

The guest ABI is deliberately conventional:

- every logical input is one physical `i32`;
- zero logical outputs produce no Wasm result;
- one or more logical outputs produce one `i32`;
- values wider than 32 bits and multi-value results use opaque handles managed
  by `wago:abi`.

The bit widths in `Input` and `Output` describe logical values, not Wasm value
types. For example, this registers `(i4, i4) -> i4` over a physical
`(i32, i32) -> i32` import:

```go
reg.Capability(wago.CapCompilerCodegen)
return reg.Compiler().Instruction(wago.InstructionSpec{
	Module: "wago:instr/example.int",
	Name:   "i4.add",
	Input:  []int32{4, 4},
	Output: []int32{4},
	Handler: func(_ wago.InstructionContext, args []wago.Bits) ([]wago.Bits, error) {
		sum, err := wago.BitsFromUint32(4, args[0].Uint32()+args[1].Uint32())
		return []wago.Bits{sum}, err
	},
	Lower: func(ctx wago.LoweringContext) error {
		ctx.Output(0, ctx.Add(ctx.Input(0), ctx.Input(1)))
		return nil
	},
})
```

`Handler` is the portable correctness path. `Lower` builds a constrained,
target-independent fixed-width expression DAG. Wago currently lowers scalar
recipes up to 32 bits and otherwise leaves the ordinary host call intact.

## Native amd64 lowering

An instruction may additionally provide one amd64 implementation. The
compatibility mode is mandatory.

`AMD64CompatibilityManaged` is the preferred mode. It exposes canonical `i32`
inputs, checked linear-memory access, engine-owned YMM operations, register
allocation, and output placement. It does not expose the encoder.

```go
AMD64: &wago.AMD64InstructionLowering{
	Compatibility: wago.AMD64CompatibilityManaged,
	Features:      wago.AMD64FeatureAVX2,
	Managed: func(ctx wago.AMD64ManagedLoweringContext) error {
		a, err := ctx.LoadYMM(1, 0)
		if err != nil {
			return err
		}
		b, err := ctx.LoadYMM(2, 0)
		if err != nil {
			return err
		}
		out, err := ctx.SIMD256YMM(81, nil, a, b) // v128.xor semantics at 256 bits
		if err != nil {
			return err
		}
		return ctx.StoreYMM(0, 0, out)
	},
}
```

`AMD64CompatibilityFullAccess` is an explicitly unsafe mode for trusted
plugins. It adds the real encoder, managed GP/YMM allocation, physical-register
reservation, the linear-memory base, and checked address construction. A plugin
may append arbitrary bytes through `ctx.Encoder().B`; Wago cannot verify those
bytes and treats the plugin like backend code.

Both modes run at compile time. Generated code records its AVX2 requirement, so
loading fails on unsupported CPUs. The plugin is not needed to load an already
compiled artifact.

## Validation and fallback

Wago verifies the imported physical signature against the registered logical
contract before code generation. A mismatched import is a compile error.

If an instruction has no supported native recipe, its `Handler` remains a
synchronous host import. Modules therefore retain executable semantics on Wago
targets without the selected native lowering. Other runtimes may provide the
same ordinary imports independently.

Instruction modules and names are plugin-owned strings. Versioning and
compatibility policy belong to the plugin; Wago does not impose a hash or
version-locking scheme.
