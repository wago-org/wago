# Compiler instruction plugins

Wago compiler plugins can recognize ordinary Wasm function imports and replace
their calls during native code generation. Any source language that can emit an
`i32` or `externref` import can use the mechanism without custom Wasm types,
custom sections, or compiler-specific metadata.

The guest ABI is conventional:

- every logical input is one physical `i32`;
- zero logical outputs produce no Wasm result;
- one or more logical outputs produce one `i32`;
- values wider than 32 bits and multi-value results use opaque handles managed
  by `wago:abi`.

`Input` and `Output` contain logical bit widths. This registers `(i4, i4) -> i4`
over a physical `(i32, i32) -> i32` import:

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

`Handler` is the executable fallback. `Lower` is an optional constrained scalar
recipe. Neither facility defines a SIMD vocabulary.

## Raw target lowerings

Target-specific implementations are independent plugin callbacks. Wago does not
define vector widths, lane types, SIMD opcodes, instruction selection,
multi-register chunking, or equivalence between targets. A plugin that supports
both AMD64 and ARM64 supplies both implementations and owns their compatibility.

```go
reg.Compiler().Instruction(wago.InstructionSpec{
	Module:  "example",
	Name:    "bytes.xor32",
	Input:   []int32{32, 32, 32}, // destination, left, right pointers
	Handler: portableFallback,

	AMD64: &wago.AMD64InstructionLowering{
		Compatibility: wago.AMD64CompatibilityFullAccess,
		Features:      wago.AMD64FeatureAVX2,
		Emit: func(ctx wago.AMD64LoweringContext) error {
			dstBase, dst, dstDisp, err := ctx.CheckedMemory(0, 0, 32)
			if err != nil {
				return err
			}
			leftBase, left, leftDisp, err := ctx.CheckedMemory(1, 0, 32)
			if err != nil {
				return err
			}
			rightBase, right, rightDisp, err := ctx.CheckedMemory(2, 0, 32)
			if err != nil {
				return err
			}
			x := ctx.AllocYMM()
			y := ctx.AllocYMM(x)
			a := ctx.Encoder()
			a.YMovdquLoadIdx(x, leftBase, left, leftDisp)
			a.YMovdquLoadIdx(y, rightBase, right, rightDisp)
			a.YPxor(x, x, y)
			a.YMovdquStoreIdx(dstBase, dst, x, dstDisp)
			return nil
		},
	},

	ARM64: &wago.ARM64InstructionLowering{
		Compatibility: wago.ARM64CompatibilityFullAccess,
		Emit: func(ctx wago.ARM64LoweringContext) error {
			// The plugin emits its own AArch64/NEON sequence here.
			// Wago does not derive it from the AMD64 implementation.
			return emitNEONXor32(ctx)
		},
	},
})
```

The import name is the plugin's semantic contract. The two callbacks contain
raw target instructions. A plugin may select AVX2, AVX-512, NEON, SVE, scalar
code, or any other implementation supported by the exposed encoder. Feature
selection and fallback between those implementations are plugin policy.

## Compatibility modes

`Managed` receives canonical inputs, checked memory helpers, and engine-owned
register lifetimes without direct encoder access. It is intentionally small and
contains no semantic instruction helpers.

`FullAccess` additionally exposes the target encoder and physical-register
allocation/reservation. Encoder byte buffers are public, so trusted plugins can
emit instructions not yet covered by a typed encoder method. Wago cannot verify
arbitrary machine code and treats a full-access plugin like backend code.

Both modes run at compile time. AMD64 feature declarations are recorded in the
compiled artifact. A target callback that is absent is not intercepted on that
target; the ordinary imported function remains available as the fallback.

## Ownership boundary

`src/core/plugins` owns registration, logical bit widths, validation, and the
canonical references to target callbacks.

`src/core/compiler/machinecode` owns only raw lowering contexts and trust modes.
It must not grow plugin-specific instruction semantics.

Each Railshot backend adapts its stack, register allocator, checked linear
memory, and raw encoder to the corresponding context. It invokes the callback
but does not interpret the plugin's instructions.

Instruction module names, operation names, versioning, feature dispatch,
cross-platform behavior, and machine-code sequences all belong to the plugin.

## Erased `externref` values

Pointer imports are suitable at the boundary, but a sequence of pointer-based
vector operations reloads and stores the same values repeatedly. A plugin can
instead mark selected `externref` parameters and a single `externref` result as
a compiler-erased virtual type:

```go
v256 := wago.VirtualType{Name: "example.v256", Size: 32}

reg.Compiler().Instruction(wago.InstructionSpec{
	Module: "example",
	Name:   "v256.xor",
	Input:  []int32{256, 256},
	Output: []int32{256},
	Virtual: &wago.VirtualSignature{
		Inputs: []wago.VirtualType{v256, v256},
		Output: &v256,
	},
	AMD64: amd64Xor,
	ARM64: arm64Xor,
})
```

The physical Wasm signature is
`(externref, externref) -> externref`. `InputVirtual` transfers each input's
native register bundle to the lowering, and `OutputVirtual` assigns the output
bundle. The plugin owns the type name, byte size, register chunking, semantics,
and machine code.

The `externref` is only a validated carrier. Wago does not allocate a host
object, enter the runtime reference store, or materialize the value in linear
memory. A source-language transform can therefore emit a chain such as:

```wat
(call $v256.store
  (call $v256.xor
    (call $v256.load (i32.const 32))
    (call $v256.load (i32.const 64)))
  (i32.const 0))
```

and the native backend keeps the intermediate value in registers. Load and
store remain ordinary plugin-defined instructions; Wago does not attach memory
or SIMD meaning to them.

Virtual values are intentionally native-only and currently have expression
lifetime. They may flow directly between plugin calls, be dropped, or be
consumed by a plugin call, but cannot be stored in Wasm locals, passed to an
ordinary function, returned from the guest, or carried across control flow.
Compilation rejects such escapes. A virtual instruction must provide at least
one target lowering and must not provide a portable `Handler`; a target without
that lowering retains a trapping import.

Because general-purpose and vector register numbers overlap on both supported
architectures, raw lowerings should call `ReleaseGP` or `ReleaseVector`.
`Release` remains as a compatibility convenience when the register class is
unambiguous.
