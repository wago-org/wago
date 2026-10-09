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
// It supports scalar integer/FP, control/direct calls, one defined or imported nonshared
// memory32, scalar globals, data/bulk and private-table indirect calls through
// production descriptors.
// The opt-in public binding retains the same bounded whole-module policy.
func CompileProduction(data []byte, opts ProductionOptions) (*enc.CompiledModule, error) {
	c, err := compileOptions(data, scalarRegisters[:], CompileOptions{ForceFallback: opts.ForceFallback, Interruptible: opts.Interruptible, production: true})
	if err != nil {
		return nil, err
	}
	return stageProduction(c, opts.DeferCodeMapping)
}

func admitProductionFunctionABI(c *Compiled) error {
	if c.state.referenceFunctions {
		return unsupported("private reference function ABI cannot be staged")
	}
	for _, info := range c.functions {
		if info.mode.referenceFunctions {
			return unsupported("private reference function ABI cannot be staged")
		}
	}
	return nil
}

func stageProduction(c *Compiled, deferMapping bool) (*enc.CompiledModule, error) {
	if err := admitProductionFunctionABI(c); err != nil {
		return nil, err
	}
	return stageProductionImage(c, deferMapping)
}

// Callers must admit the complete function ABI before allocating an image.
func stageProductionImage(c *Compiled, deferMapping bool) (*enc.CompiledModule, error) {
	if len(c.code) == 0 && len(c.functions) == 0 && c.state.importedFunctions == 0 {
		// State-only modules have nothing to publish. In particular, do not
		// create the page-rounded empty code mapping used by imported bridges.
		return &enc.CompiledModule{}, nil
	}
	var err error
	var image *coreruntime.CodeBuffer
	if deferMapping {
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
