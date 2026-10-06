//go:build linux && (amd64 || arm64 || riscv64) && !tinygo && !wago_target_tinygo

package riscv64

import (
	"fmt"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
)

// compileSWARProductionModule stages the qualified SWAR lowering through
// the shared production code-image contract. Public integration additionally enforces
// source, artifact and CPU admission. Decoded callers need separate wire
// provenance admission and instance state setup before making entries callable.
func compileSWARProductionModule(m *wasm.Module, opts CompileOptions) (*enc.CompiledModule, error) {
	if m == nil {
		return nil, fmt.Errorf("rv64: nil module")
	}
	if err := admitSWARDecodedBudget(m); err != nil {
		return nil, err
	}
	if err := admitProductionOptions(m, &opts, true); err != nil {
		return nil, err
	}
	policy := swarModulePlain
	if len(m.Imports) != 0 {
		policy = swarModuleHosts
	} else if len(m.Tables) != 0 || len(m.Elements) != 0 {
		policy = swarModuleTables
	}
	image, err := compileSWARDecodedModule(m, emissionMode{production: true, interruptible: opts.Interruptible}, policy)
	if err != nil {
		return nil, err
	}
	return stageProductionModule(&Compiled{code: image.code, functions: image.functions}, opts)
}

// moduleUsesSWAR walks encoded instructions, skipping their actual immediates;
// an immediate byte equal to the SIMD prefix must not change compiler selection.
// Existing production count/byte budgets precede this walk, and per-type/run
// limits prevent hand-built decoded callers from hiding unbounded metadata.
func moduleUsesSWAR(m *wasm.Module) (bool, error) {
	for _, group := range m.Types {
		if len(group.SubTypes) != 1 {
			return false, unsupported("decoded type group")
		}
		sig := &group.SubTypes[0].Comp
		if len(sig.Params) > 256 || len(sig.Results) > 256 {
			return false, unsupported("decoded signature bound")
		}
		for _, list := range [][]wasm.ValType{sig.Params, sig.Results} {
			for _, typ := range list {
				if wasm.EqualValType(typ, wasm.V128) {
					return true, nil
				}
			}
		}
	}
	for _, global := range m.Globals {
		if wasm.EqualValType(global.Type.Type, wasm.V128) {
			return true, nil
		}
	}
	for _, fn := range m.Code {
		if len(fn.Locals.Runs) > 256 {
			return false, unsupported("decoded local declaration bound")
		}
		for _, run := range fn.Locals.Runs {
			if wasm.EqualValType(run.Type, wasm.V128) {
				return true, nil
			}
		}
		reader := wasm.ReaderFrom(fn.BodyBytes)
		for reader.HasNext() {
			op, err := reader.Byte()
			if err != nil {
				return false, err
			}
			if op == 0xfd {
				return true, nil
			}
			if op == 0x02 || op == 0x03 || op == 0x04 {
				// Inline block types are single bytes; indexed signatures
				// were already examined above. Peek without disturbing the
				// reader used by the ordinary immediate classifier below.
				look := reader
				if typ, err := look.Byte(); err == nil && typ == 0x7b {
					return true, nil
				}
			}
			if op == 0x1c {
				// Typed select has exactly one valtype. Check its count before
				// any general classifier could expand unvalidated metadata.
				look := reader
				count, err := look.U32()
				if err != nil || count != 1 {
					return false, unsupported("typed select valtype count")
				}
				if typ, err := look.Byte(); err == nil && typ == 0x7b {
					return true, nil
				}
			}
			if err := wasm.SkipInstructionImmediate(&reader, op); err != nil {
				return false, err
			}
		}
	}
	return false, nil
}
