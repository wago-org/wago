//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"unsafe"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

// The entry graph retains all physical roots. Only indexed arenas contribute
// canonical descriptor snapshots; a bare producer keeps its existing anchor.
// Counts are checked without allocating any converted signatures or images.
func rv64NativeTailOwnerCounts(in *Instance) (counts railshot.NativeTailOwnerCounts, metadata int, err error) {
	if _, err = (rv64TailCodeOwner{in}).NativeTailCodeView(); err != nil {
		return counts, 0, err
	}
	c := in.c
	if !c.nativeContextTrapABI || !c.nativeTableContextABI || !c.registerABIDisabled || in.hostEvents != nil || c.NumImports < 0 || c.NumImports > railshot.MaxHostFunctionImports || len(c.importFuncSigs) != c.NumImports {
		return counts, 0, rv64TailGenerationError("native owner ABI")
	}
	if !c.needsFuncRefDescs() {
		return counts, 0, nil
	}
	n := len(c.FuncTypeID)
	if n <= c.NumImports || n > railshot.MaxFunctions || n-c.NumImports > len(c.Funcs) || n-c.NumImports > len(c.Entry) {
		return counts, 0, rv64TailGenerationError("indexed owner function arena")
	}
	counts.Owners, counts.Functions = 1, n
	for i := 0; i < n; i++ {
		var sig FuncSig
		if i < c.NumImports {
			sig = c.importFuncSigs[i]
		} else {
			sig = c.Funcs[i-c.NumImports]
		}
		values, e := rv64NativeTailSignatureValues(sig)
		if e != nil {
			return counts, 0, e
		}
		counts.Values += values
		if i < c.NumImports {
			target, native := rv64NativeFunctionAttachment(in, i)
			if !native || target == nil || target.localIdx < 0 {
				return counts, 0, rv64TailGenerationError("dynamic owner requires genuine native imports")
			}
			if _, e := (rv64TailCodeOwner{target.inst}).NativeTailCodeView(); e != nil {
				return counts, 0, e
			}
			if _, _, _, valid := rv64QualifiedNativeTailTarget(target); !valid {
				return counts, 0, rv64TailGenerationError("dynamic owner import target frame")
			}
			bytes, e := railshot.NativeTailFunctionBytes(values)
			if e != nil {
				return counts, 0, e
			}
			metadata += bytes + int(unsafe.Sizeof(railshot.NativeTailCodeImage{})) + int(unsafe.Sizeof(rv64TailCodeOwner{}))
		}
	}
	if c.NumImports == 0 {
		metadata, err = railshot.NativeTailDefinedOwnerBytes(n, counts.Values)
	} else {
		if _, err = (rv64TailThunkCodeOwner{in}).NativeTailCodeView(); err != nil {
			return counts, 0, err
		}
		counts.Targets, counts.TargetValues, counts.ProxySlots, counts.Proxies = n, counts.Values, n, c.NumImports
		var bytes int
		bytes, err = railshot.NativeTailImportedOwnerBytes(n, c.NumImports, counts.Values)
		metadata += bytes + int(unsafe.Sizeof(rv64TailThunkCodeOwner{}))
	}
	metadata += int(unsafe.Sizeof(rv64TailCodeOwner{}))
	return counts, metadata, err
}

func rv64NativeTailSignatureValues(sig FuncSig) (int, error) {
	for _, list := range [][]ValType{sig.Params, sig.Results} {
		if len(list) > 256 {
			return 0, rv64TailGenerationError("native owner signature count")
		}
		cells := 0
		for _, typ := range list {
			switch typ {
			case ValI32, ValI64, ValF32, ValF64:
				cells++
			case ValV128:
				cells += 2
			default:
				return 0, rv64TailGenerationError("native owner signature type")
			}
		}
		if cells > 256 {
			return 0, rv64TailGenerationError("native owner physical signature bound")
		}
	}
	return len(sig.Params) + len(sig.Results), nil
}

// Called only after the generation reserves the complete graph's code and
// metadata. It owns all snapshots directly, with no persistent instance cache.
func rv64NewNativeTailOwner(in *Instance, counts railshot.NativeTailOwnerCounts) (*railshot.NativeTailOwner, error) {
	if counts.Owners == 0 {
		return nil, nil
	}
	if in.c.NumImports == 0 {
		return rv64NewNativeDefinedTailOwner(in, counts.Functions, counts.Values)
	}
	image, err := railshot.NewNativeTailCodeImage(rv64TailCodeOwner{in})
	if err != nil {
		return nil, err
	}
	wrappers, err := railshot.NewNativeTailCodeImage(rv64TailThunkCodeOwner{in})
	if err != nil {
		return nil, err
	}
	state := &in.ensurePluginState().railshotInvocation
	if state.nativeImports == nil || len(state.nativeImports.records) != in.c.NumImports {
		return nil, rv64TailGenerationError("missing certified import construction")
	}
	signatures := make([]wasm.CompType, counts.Functions)
	imports := make([]railshot.NativeTailImport, in.c.NumImports)
	for i := range signatures {
		var sig FuncSig
		if i < in.c.NumImports {
			sig = in.c.importFuncSigs[i]
		} else {
			sig = in.c.Funcs[i-in.c.NumImports]
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
		if i >= len(imports) {
			continue
		}
		r := &state.nativeImports.records[i]
		if err := r.validate(in, i, wrappers); err != nil {
			return nil, err
		}
		producer := r.producer
		actual, err := railshot.NewNativeTailCodeImage(rv64TailCodeOwner{producer})
		if err != nil {
			return nil, err
		}
		entry, end := producer.c.Entry[r.local], len(actual.Bytes())
		if r.local+1 < len(producer.c.Entry) {
			end = producer.c.Entry[r.local+1]
		}
		target, err := railshot.NewNativeTailFunction(actual, producer.jm, rv64NativeInvocationContext(producer), producer.funcRefDescs, entry, end, uint32(producer.c.NumImports+r.local), signatures[i])
		if err != nil {
			return nil, err
		}
		imports[i] = railshot.NativeTailImport{Image: wrappers, Offset: r.offset, CodeBytes: r.codeBytes, Target: target, Arguments: r.target.Arguments, Results: r.target.Results, Certificate: r.certificate}
	}
	trampoline, err := coreruntime.HostCallTrampolinePointer()
	if err != nil {
		return nil, err
	}
	return railshot.NewNativeTailImportedOwner(image, in.jm, rv64NativeInvocationContext(in), in.funcRefDescs, state.tailTargets, in.c.Entry, signatures, imports, trampoline)
}
