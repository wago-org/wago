//go:build linux && (amd64 || arm64 || riscv64) && !tinygo && !wago_target_tinygo

package riscv64

import (
	"fmt"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
)

// compileSWARProductionModule stages the qualified private SWAR lowering through
// the shared production code-image contract. This prerequisite does not change
// public source, artifact or CPU admission. Decoded callers need separate wire
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
