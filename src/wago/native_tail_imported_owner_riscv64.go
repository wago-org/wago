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
	if _, err = (rv64TailCodeOwner{in}).nativeOwnerCodeView(true); err != nil {
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
	if n == 0 || n < c.NumImports || n > railshot.MaxFunctions || n-c.NumImports > len(c.Funcs) || n-c.NumImports > len(c.Entry) || n == c.NumImports && (len(c.Funcs) != 0 || len(c.Entry) != 0) {
		return counts, 0, rv64TailGenerationError("indexed owner function arena")
	}
	counts.Owners, counts.Functions = 1, n
	if c.nativeReferenceFunctionABI {
		counts.MaskSlots = n
	}
	publicReferenceCarriers := rv64PublicReferenceCallBoundary(c) && rv64PublicReferenceCarrierTarget(c, 0)
	publicDynamicHostReferenceCarriers := rv64PublicDynamicHostReferenceBoundary(c)
	mutableVariant := rv64MutableFuncrefGlobalLocalCallCodeVariant(c)
	mutableReferenceCarriers := mutableVariant >= 19 && mutableVariant <= 21
	for i := 0; i < n; i++ {
		if railshotNonCallableFunction(c, i) {
			counts.NonCallable++
			continue
		}
		var sig FuncSig
		if i < c.NumImports {
			sig = c.importFuncSigs[i]
		} else {
			sig = c.Funcs[i-c.NumImports]
		}
		values, e := rv64NativeTailSignatureValues(sig, publicReferenceCarriers && i == 0 || publicDynamicHostReferenceCarriers && i <= 1 || mutableReferenceCarriers && i <= 1)
		if e != nil {
			return counts, 0, e
		}
		counts.Values += values
		if i < c.NumImports {
			target, native := rv64NativeFunctionAttachment(in, i)
			if !native {
				if _, e := rv64HostTailAttached(in, i); e != nil {
					return counts, 0, e
				}
				bytes, e := railshot.NativeTailHostFunctionBytes(values)
				if e != nil {
					return counts, 0, e
				}
				ownerBytes, e := rv64HostTailCodeOwnerBytes(values)
				if e != nil {
					return counts, 0, e
				}
				metadata += bytes + ownerBytes + int(unsafe.Sizeof(railshot.NativeTailCodeImage{}))
				counts.HostSlots = n
				continue
			}
			if target == nil || target.localIdx < 0 {
				return counts, 0, rv64TailGenerationError("dynamic owner native import has no definition")
			}
			counts.Proxies++
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
		if c.nativeReferenceFunctionABI {
			metadata, err = railshot.NativeTailSparseDefinedOwnerBytes(n, counts.NonCallable, counts.Values)
		} else {
			metadata, err = railshot.NativeTailDefinedOwnerBytes(n, counts.Values)
		}
	} else {
		if counts.Proxies != 0 {
			if _, err = (rv64TailThunkCodeOwner{in}).NativeTailCodeView(); err != nil {
				return counts, 0, err
			}
			metadata += int(unsafe.Sizeof(rv64TailThunkCodeOwner{}))
		}
		counts.Targets, counts.TargetValues, counts.ProxySlots = n, counts.Values, n
		counts.TargetNonCallable = counts.NonCallable
		var bytes int
		if c.nativeReferenceFunctionABI && counts.HostSlots != 0 {
			bytes, err = railshot.NativeTailSparseHostOwnerBytes(n, c.NumImports, counts.NonCallable, counts.Values)
		} else if c.nativeReferenceFunctionABI {
			bytes, err = railshot.NativeTailSparseImportedOwnerBytes(n, c.NumImports, counts.NonCallable, counts.Values)
		} else if counts.HostSlots != 0 {
			bytes, err = railshot.NativeTailHostOwnerBytes(n, c.NumImports, counts.Values)
		} else {
			bytes, err = railshot.NativeTailImportedOwnerBytes(n, c.NumImports, counts.Values)
		}
		// The base budget conservatively includes an import-wrapper image and
		// proxy records even for host-only owners; host images are charged above.
		metadata += bytes
	}
	metadata += int(unsafe.Sizeof(rv64TailCodeOwner{}))
	return counts, metadata, err
}

func rv64NativeTailSignatureValues(sig FuncSig, referenceCarriers bool) (int, error) {
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
			case ValFuncRef, ValExternRef:
				if !referenceCarriers {
					return 0, rv64TailGenerationError("native owner signature type")
				}
				cells++
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
	var image *railshot.NativeTailCodeImage
	var err error
	if counts.Functions != in.c.NumImports {
		image, err = railshot.NewNativeTailCodeImage(rv64TailCodeOwner{in})
		if err != nil {
			return nil, err
		}
	}
	state := &in.ensurePluginState().railshotInvocation
	var wrappers *railshot.NativeTailCodeImage
	if counts.Proxies != 0 {
		wrappers, err = railshot.NewNativeTailCodeImage(rv64TailThunkCodeOwner{in})
		if err != nil {
			return nil, err
		}
		if state.nativeImports == nil || len(state.nativeImports.records) != in.c.NumImports {
			return nil, rv64TailGenerationError("missing certified import construction")
		}
	}
	signatures := make([]wasm.CompType, counts.Functions)
	imports := make([]railshot.NativeTailImport, in.c.NumImports)
	for i := range signatures {
		if railshotNonCallableFunction(in.c, i) {
			continue
		}
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
		if _, native := rv64NativeFunctionAttachment(in, i); !native {
			owner, err := rv64NewHostTailCodeOwner(in, i)
			if err != nil {
				return nil, err
			}
			actual, err := railshot.NewNativeTailCodeImage(owner)
			if err != nil {
				return nil, err
			}
			r := owner.record
			var host *railshot.NativeTailHostFunction
			if rv64PublicDynamicHostReferenceBoundary(in.c) && i == 0 {
				host, err = railshot.NewNativeTailHostReferenceFunction(actual, r.certificate, in.jm, rv64NativeInvocationContext(in), in.funcRefDescs, r.offset, r.dispatchIndex, r.owned, signatures[i])
			} else {
				host, err = railshot.NewNativeTailHostFunction(actual, r.certificate, in.jm, rv64NativeInvocationContext(in), in.funcRefDescs, r.offset, r.dispatchIndex, r.owned, signatures[i])
			}
			if err != nil {
				return nil, err
			}
			imports[i] = railshot.NativeTailImport{Image: actual, Offset: r.offset, CodeBytes: r.certificate.CodeBytes(), Host: host}
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
	if in.c.nativeReferenceFunctionABI {
		callable := make([]*wasm.CompType, len(signatures))
		for i := range signatures {
			if !railshotNonCallableFunction(in.c, i) {
				callable[i] = &signatures[i]
			}
		}
		return railshot.NewNativeTailSparseImportedOwner(image, in.jm, rv64NativeInvocationContext(in), in.funcRefDescs, state.tailTargets, in.c.Entry, callable, imports, trampoline)
	}
	return railshot.NewNativeTailImportedOwner(image, in.jm, rv64NativeInvocationContext(in), in.funcRefDescs, state.tailTargets, in.c.Entry, signatures, imports, trampoline)
}
