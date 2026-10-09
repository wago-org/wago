//go:build linux && (amd64 || arm64 || riscv64) && !tinygo && !wago_target_tinygo

package riscv64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
)

// Permission is not an ABI witness. Only actual defined bindings select this
// profile; unused reference control types and ordinary modules keep their ABI.
// Directory bounds precede this allocation-free selection pass.
func hasDefinedReferenceFunctions(m *wasm.Module) (bool, error) {
	if m == nil || len(m.Code) > MaxFunctions || len(m.Types) > MaxFunctions || len(m.FuncTypes) != len(m.Code) {
		return false, unsupported("closed reference selection directory bound")
	}
	references := false
	for _, index := range m.FuncTypes {
		if index.Rec || uint64(index.Index) >= uint64(len(m.Types)) || len(m.Types[index.Index].SubTypes) != 1 {
			return false, unsupported("closed reference selection function binding")
		}
		sig := &m.Types[index.Index].SubTypes[0].Comp
		if sig.Kind != wasm.CompFunc || len(sig.Params) > 256 || len(sig.Results) > 256 {
			return false, unsupported("closed reference selection signature bound")
		}
		for _, list := range [][]wasm.ValType{sig.Params, sig.Results} {
			for _, v := range list {
				references = references || v.Kind() == wasm.ValRef
			}
		}
	}
	return references, nil
}

func admitClosedReferenceProductionOptions(opts CompileOptions) error {
	if !opts.NativeReferenceFunctions || !opts.referenceFunctions || !opts.Interruptible || !opts.ForeignTableCalls || !opts.InvocationContext || opts.importTailCalls || opts.dynamicTailCalls || opts.localTailCalls != opts.NativeTailCalls || opts.Profile || opts.SourceMaps || opts.UnwindMaps {
		return unsupported("closed reference staging requires cooperative local-call production")
	}
	if opts.NativeTailCalls && opts.tailPlans == nil || opts.tailPlans != nil && len(*opts.tailPlans) != 0 || opts.referenceTailInstructions || opts.referenceCallInstructions {
		return unsupported("closed reference staging excludes symbolic foreign call plans")
	}
	return nil
}

// This is the sole staging entrance for the closed function ABI. It rechecks
// source confinement and the complete emitted mode before any image allocation.
// Legacy staging and isolated instantiation continue to reject private images.
func stageClosedReferenceProductionModule(c *Compiled, opts CompileOptions, m *wasm.Module, vector bool) (*enc.CompiledModule, error) {
	if err := admitClosedReferenceProductionOptions(opts); err != nil {
		return nil, err
	}
	if err := admitClosedReferenceFunctionModule(m, vector); err != nil {
		return nil, err
	}
	if opts.NativeTailCalls && opts.tailModule != m || !opts.NativeTailCalls && opts.tailModule != nil {
		return nil, unsupported("closed reference staged source identity")
	}
	_, _, control, err := admitProductionDynamicTails(m, opts.NativeReferenceCalls, false, vector, true)
	if err != nil {
		return nil, err
	}
	if opts.referenceControlInstructions != control {
		return nil, unsupported("closed reference staged control witness")
	}
	if c == nil || !c.state.referenceFunctions || c.state.importedFunctions != m.ImportedFuncCount() || len(c.functions) != len(m.Code) || len(c.code) == 0 || len(c.code) > MaxCodeBytes {
		return nil, unsupported("closed reference staged function extent")
	}
	for _, f := range c.functions {
		mode := f.mode
		if !mode.referenceFunctions || !mode.production || !mode.interruptible || !mode.foreignTableCalls || !mode.invocationContext || mode.importTailCalls || mode.dynamicTailCalls || mode.localTailCalls != opts.NativeTailCalls || mode.referenceCalls != opts.NativeReferenceCalls || mode.profileMode != (profileMode{}) || f.FrameHeaderBytes != 16 && f.FrameHeaderBytes != callFrameHeader {
			return nil, unsupported("closed reference staged function mode")
		}
	}
	return stageAdmittedProductionModule(c, opts)
}
