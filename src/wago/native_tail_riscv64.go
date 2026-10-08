//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"encoding/binary"
	"errors"
	"fmt"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

// Reject every unsupported target before caller code publication. Normal Go
// callbacks use retained synchronous RV64 wrappers in the caller's context.
// Reexport bridges still have a different return/ownership protocol.
func rv64ValidateTailImports(c *Compiled, imports resolvedImports) error {
	m := rv64NativeTailModule(c)
	if m == nil {
		return nil
	}
	for _, p := range m.Plans {
		if p.Kind != 0 {
			return fmt.Errorf("%w: RV64 dynamic tail owner attachment is pending", railshot.ErrUnsupported)
		}
		target, ok := imports[c.functionImportBindingKey(int(p.ImportIndex))].(*InstanceExport)
		if !ok {
			if err := rv64ScalarHostSignature(c.importFuncSigs[p.ImportIndex]); err != nil {
				return err
			}
			// The ordinary import admission below authenticates callback/owned
			// handle bindings before mapping or retaining their owners.
			continue
		}
		if err := railshotValidateNativeExport(c, int(p.ImportIndex), target); err != nil {
			return err
		}
		// Hold physical lifetime while inspecting the actual anchor. Hand-built
		// metadata can omit it even with an otherwise valid framed numeric ABI.
		producer := target.inst
		if err := producer.beginInvocation(); err != nil {
			return err
		}
		valid := len(producer.funcRefDescs) >= coreruntime.FuncRefDescBytes
		if valid {
			valid = binary.LittleEndian.Uint64(producer.funcRefDescs[coreruntime.FuncRefContextOffset:]) == uint64(producer.nativeContext) && binary.LittleEndian.Uint64(producer.funcRefDescs[coreruntime.TableEntryHomeLinMemOffset:]) == uint64(producer.jm.LinMemBase()) && binary.LittleEndian.Uint64(rv64NativeInvocationContext(producer)[16:]) == uint64(offHeapSlicePtr(producer.funcRefDescs))
		}
		producer.endInvocation()
		if !valid {
			return fmt.Errorf("%w: RV64 native tail target has no canonical context anchor", railshot.ErrUnsupported)
		}
	}
	return nil
}
func rv64TailTypes(values []ValType) ([]wasm.ValType, error) {
	out := make([]wasm.ValType, len(values))
	for i, v := range values {
		switch v {
		case ValI32:
			out[i] = wasm.I32
		case ValI64:
			out[i] = wasm.I64
		case ValF32:
			out[i] = wasm.F32
		case ValF64:
			out[i] = wasm.F64
		case ValV128:
			out[i] = wasm.V128
		default:
			return nil, fmt.Errorf("RV64 staged tail transport type")
		}
	}
	return out, nil
}
func railshotPrepareNativeTail(in *Instance, imports resolvedImports, sharedBase uintptr, sharedOffsets []int, ownedAddr map[uint32]uint64) error {
	m := rv64NativeTailModule(in.c)
	if m != nil {
		for _, p := range m.Plans {
			if p.Kind != 0 {
				return fmt.Errorf("%w: RV64 dynamic tail runtime attachment is pending", railshot.ErrUnsupported)
			}
		}
	}
	if err := rv64PrepareTailTargets(in, imports, sharedBase, sharedOffsets, ownedAddr); err != nil {
		return err
	}
	if m == nil || len(m.Plans) == 0 {
		return nil
	}
	// Pending failed cleanup blocks further factory allocations for this module.
	cc := in.c.loadCodeCache()
	cc.mu.Lock()
	err := railshotRetryNativeTail(in.c)
	cc.mu.Unlock()
	if err != nil {
		return err
	}
	trampoline, err := coreruntime.HostCallTrampolinePointer()
	if err != nil {
		return err
	}
	sites := make([]railshot.NativeTailSite, len(m.Plans))
	for i, p := range m.Plans {
		sig := in.c.importFuncSigs[p.ImportIndex]
		params, err := rv64TailTypes(sig.Params)
		if err != nil {
			return err
		}
		results, err := rv64TailTypes(sig.Results)
		if err != nil {
			return err
		}
		sites[i] = railshot.NativeTailSite{
			CallerHome: in.jm.LinMemBase(), CallerContext: rv64NativeInvocationContext(in),
			FrameBytes: p.FrameBytes, SourceBase: p.SourceBase, SourceStride: p.SourceStride, FunctionIndex: p.FunctionIndex, SourcePC: p.SourcePC, Parameters: params, Results: results,
			Continuations: []railshot.NativeTailCodeRange{{Base: in.base, Bytes: uintptr(len(in.c.code))}},
		}
		if target, native := imports[in.c.functionImportBindingKey(int(p.ImportIndex))].(*InstanceExport); native {
			if target == nil || target.inst == nil || target.inst.jm == nil || target.localIdx < 0 || target.localIdx >= len(target.inst.c.Entry) {
				return fmt.Errorf("RV64 staged tail invalid attached native target")
			}
			producer := target.inst
			sites[i].TargetContext = rv64NativeInvocationContext(producer)
			sites[i].Target = railshot.NativeImportTarget{Entry: producer.base + uintptr(producer.c.Entry[target.localIdx]), Home: producer.jm.LinMemBase(), Context: producer.nativeContext, HostTrampoline: trampoline, StackOwnedActivation: true, InvocationContext: true}
			continue
		}
		if !in.syncMode {
			return fmt.Errorf("RV64 staged tail requires synchronous host protocol")
		}
		// Use only addresses returned by this construction's trusted thunk
		// owners; never dereference a caller-supplied native dispatch pointer.
		entry := uintptr(ownedAddr[p.ImportIndex])
		if entry == 0 && int(p.ImportIndex) < len(sharedOffsets) && sharedOffsets[p.ImportIndex] != noHostThunkOffset {
			entry = sharedBase + uintptr(sharedOffsets[p.ImportIndex])
		}
		if entry == 0 {
			return fmt.Errorf("RV64 staged tail lost retained host wrapper")
		}
		sites[i].HostEntry = true
		sites[i].TargetContext = rv64NativeInvocationContext(in)
		sites[i].Target = railshot.NativeImportTarget{Entry: entry, Home: in.jm.LinMemBase(), Context: in.nativeContext, HostTrampoline: trampoline, StackOwnedActivation: true, InvocationContext: true}
	}
	owner, err := railshot.NewNativeTailDispatcherInArena(sites, len(in.c.code), in.ar)
	// Install cleanup ownership even if publication failed partway through.
	in.ensurePluginState().railshotInvocation.tailDispatcher = owner
	return err
}
func railshotReleaseNativeTail(in *Instance) {
	state := in.pluginState.Load()
	if state == nil {
		return
	}
	// These borrowed bytes share the descriptor arena's physical lifetime,
	// including reference-only instances without a tail code dispatcher.
	state.railshotInvocation.tailTargets = nil
	owner := state.railshotInvocation.tailDispatcher
	if owner == nil {
		return
	}
	cc := in.c.loadCodeCache()
	cc.mu.Lock()
	defer cc.mu.Unlock()
	if err := owner.Close(); err != nil {
		in.c.ensureCompileIndexes().railshotTail.retired = append(in.c.ensureCompileIndexes().railshotTail.retired, owner)
	}
	state.railshotInvocation.tailDispatcher = nil
}

// Caller holds codeCache.mu. Failed detached mappings remain owned by the
// module, and are retried before its next instance/publication allocation.
func railshotRetryNativeTail(c *Compiled) error {
	indexes := c.loadCompileIndexes()
	if indexes == nil {
		return nil
	}
	owners := indexes.railshotTail.retired
	retained := owners[:0]
	var errs []error
	for _, owner := range owners {
		if err := owner.Close(); err != nil {
			retained = append(retained, owner)
			errs = append(errs, err)
		}
	}
	clear(owners[len(retained):])
	indexes.railshotTail.retired = retained
	return errors.Join(errs...)
}
