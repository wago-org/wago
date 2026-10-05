//go:build linux && (amd64 || arm64 || riscv64) && !tinygo && !wago_target_tinygo

package riscv64

import (
	"errors"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

// ProductionOptions controls staging for the production ABI checkpoint.
// ForceFallback is a differential diagnostic; it still emits genuine RV64.
type ProductionOptions struct {
	DeferCodeMapping bool
	ForceFallback    bool
	Interruptible    bool
}

// CompileProduction admits the whole module before producing any native image.
// It supports integer/control/direct calls, one defined nonshared memory32,
// integer globals and data/bulk operations through production descriptors.
// Public Wago backend binding and broader feature lowering remain separate.
func CompileProduction(data []byte, opts ProductionOptions) (*enc.CompiledModule, error) {
	c, err := compileOptions(data, scalarRegisters[:], CompileOptions{ForceFallback: opts.ForceFallback, Interruptible: opts.Interruptible, production: true})
	if err != nil {
		return nil, err
	}
	var image *coreruntime.CodeBuffer
	if opts.DeferCodeMapping {
		image, err = coreruntime.NewHeapCodeBuffer(len(c.code))
	} else {
		image, err = coreruntime.NewCodeBuffer(len(c.code))
	}
	if err != nil {
		return nil, err
	}
	if err := image.Append(c.code); err != nil {
		if cleanup := image.Close(); cleanup != nil {
			return &enc.CompiledModule{CodeImage: image}, errors.Join(err, cleanup)
		}
		return nil, err
	}
	entries, internal := shared.ModuleEntries(len(c.functions))
	for i, f := range c.functions {
		// No direct/prepared proof is advertised. Internal aliases enter the
		// same fully framed wrapper ABI until a genuine register ABI is added.
		entries[i], internal[i] = f.Entry, f.Entry
	}
	return &enc.CompiledModule{Code: image.Bytes(), CodeImage: image, Entry: entries, InternalEntry: internal}, nil
}
