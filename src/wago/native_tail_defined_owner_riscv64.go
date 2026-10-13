//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"fmt"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// Private cache qualification helper, called under nativeExecutionMu and the
// exact physical root. Production generations use the uncached constructor so
// refunding a reservation cannot leave uncharged snapshots on an instance.
func rv64NativeDefinedTailOwner(in *Instance) (*railshot.NativeTailDefinedOwner, error) {
	n, values, err := rv64NativeDefinedTailOwnerCounts(in)
	if err != nil {
		return nil, err
	}
	state := in.ensurePluginState()
	if owner := state.railshotInvocation.definedTailOwner; owner != nil {
		return owner, owner.Validate()
	}
	owner, err := rv64NewNativeDefinedTailOwner(in, n, values)
	if err == nil {
		state.railshotInvocation.definedTailOwner = owner
	}
	return owner, err
}

// Generations own these snapshots directly. They must not leave a persistent
// instance cache after refunding their metadata reservation at retirement.
func rv64NewNativeDefinedTailOwner(in *Instance, n, values int) (*railshot.NativeTailDefinedOwner, error) {
	if err := railshot.NativeTailDefinedOwnerBudget(n, values); err != nil {
		return nil, err
	}
	if in.c.nativeReferenceFunctionABI {
		nonCallable := 0
		for i := 0; i < n; i++ {
			if railshotNonCallableFunction(in.c, i) {
				nonCallable++
			}
		}
		if _, err := railshot.NativeTailSparseDefinedOwnerBytes(n, nonCallable, values); err != nil {
			return nil, err
		}
	}
	image, err := railshot.NewNativeTailCodeImage(rv64TailCodeOwner{in})
	if err != nil {
		return nil, err
	}
	signatures := make([]wasm.CompType, n)
	for i, sig := range in.c.Funcs[:n] {
		if railshotNonCallableFunction(in.c, i) {
			continue
		}
		params, err := rv64TailTypes(sig.Params)
		if err != nil {
			return nil, err
		}
		results, err := rv64TailTypes(sig.Results)
		if err != nil {
			return nil, err
		}
		signatures[i] = wasm.CompType{Kind: wasm.CompFunc, Params: params, Results: results}
	}
	if in.c.nativeReferenceFunctionABI {
		callable := make([]*wasm.CompType, n)
		for i := range signatures {
			if !railshotNonCallableFunction(in.c, i) {
				callable[i] = &signatures[i]
			}
		}
		return railshot.NewNativeTailSparseDefinedOwner(image, in.jm, rv64NativeInvocationContext(in), in.funcRefDescs, in.ensurePluginState().railshotInvocation.tailTargets, in.c.Entry, callable)
	}
	owner, err := railshot.NewNativeTailDefinedOwner(image, in.jm, rv64NativeInvocationContext(in), in.funcRefDescs, in.ensurePluginState().railshotInvocation.tailTargets, in.c.Entry, signatures)
	if err != nil {
		return nil, err
	}
	return owner, nil
}

func rv64NativeDefinedTailOwnerCounts(in *Instance) (int, int, error) {
	if in == nil || in.c == nil || !in.c.nativeContextTrapABI || !in.c.nativeTableContextABI || !in.c.registerABIDisabled || in.hostEvents != nil || in.c.NumImports != 0 {
		return 0, 0, fmt.Errorf("%w: RV64 defined tail snapshot requires a native definition-only owner", railshot.ErrUnsupported)
	}
	n := len(in.c.FuncTypeID)
	if n == 0 || n > len(in.c.Funcs) || n > len(in.c.Entry) || n > railshot.MaxFunctions || !in.c.needsFuncRefDescs() {
		return 0, 0, fmt.Errorf("%w: RV64 defined tail snapshot function arena", railshot.ErrUnsupported)
	}
	values := 0
	publicReferenceCarriers := rv64PublicReferenceCallBoundary(in.c) && rv64PublicReferenceCarrierTarget(in.c, 0)
	mutableVariant := rv64MutableFuncrefGlobalLocalCallCodeVariant(in.c)
	mutableReferenceCarriers := mutableVariant == 19 || mutableVariant == 20
	for i, sig := range in.c.Funcs[:n] {
		if railshotNonCallableFunction(in.c, i) {
			continue
		}
		if len(sig.Params) > 256 || len(sig.Results) > 256 {
			return 0, 0, fmt.Errorf("%w: RV64 defined tail signature count", railshot.ErrUnsupported)
		}
		for _, list := range [][]ValType{sig.Params, sig.Results} {
			cells := 0
			for _, typ := range list {
				switch typ {
				case ValI32, ValI64, ValF32, ValF64:
					cells++
				case ValV128:
					cells += 2
				case ValFuncRef, ValExternRef:
					if !(publicReferenceCarriers && i == 0) && !(mutableReferenceCarriers && i <= 1) {
						return 0, 0, fmt.Errorf("%w: RV64 defined tail signature type", railshot.ErrUnsupported)
					}
					cells++
				default:
					return 0, 0, fmt.Errorf("%w: RV64 defined tail signature type", railshot.ErrUnsupported)
				}
			}
			if cells > 256 {
				return 0, 0, fmt.Errorf("%w: RV64 defined tail physical signature bound", railshot.ErrUnsupported)
			}
			values += len(list)
		}
	}
	return n, values, nil
}
