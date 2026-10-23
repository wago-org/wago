//go:build amd64

package amd64

import "github.com/wago-org/wago/src/core/compiler/wasm"

var directReferenceABIEnabled = true

// directReferenceRegABI admits only ordinary local reference calls. Dynamic
// calls retain wrapper bodies; tail and EH modules keep their existing
// entry/return contracts. Wrapper adapters terminate native root scans, so only
// private direct-only targets may acquire a new internal entry. A reference pair
// needs a separate result-capture proof.
func (f *fn) directReferenceRegABI(localIdx int, ft *wasm.CompType) bool {
	if !f.opt(optDirectReferenceABI) || f.stagedTailDescriptors || f.moduleEH ||
		localIdx < 0 || localIdx >= len(f.referenceWrapperTargets) || f.referenceWrapperTargets[localIdx] ||
		!sigFitsDirectReferenceRegABI(ft) {
		return false
	}
	return true
}

func sigFitsDirectReferenceRegABI(ft *wasm.CompType) bool {
	if ft == nil || len(ft.Results) > 1 || !sigFitsTypedReferenceRegABI(ft) {
		return false
	}
	for _, typ := range ft.Params {
		if typ.Kind() == wasm.ValRef {
			return true
		}
	}
	return len(ft.Results) == 1 && ft.Results[0].Kind() == wasm.ValRef
}

// Keep source reachability separate from emitted adapters. Compact adapter and
// trap-body sharing must see every actual adapter as a layout boundary.
func directReferenceHostAdapters(m *wasm.Module, hints []funcHints, policy CodegenPolicy, wrappers []bool) []bool {
	if !policy.EnabledOption(optRegABI) || !policy.EnabledOption(optDirectReferenceABI) {
		return wrappers
	}
	emitted := wrappers
	copied := false
	for i, reachable := range wrappers {
		if reachable || hints[i].flags.has(hintHasTailCall|hintModuleEH) {
			continue
		}
		ft, ok := m.LocalFuncType(i)
		if !ok || !sigFitsDirectReferenceRegABI(ft) {
			continue
		}
		if !copied {
			emitted = append([]bool(nil), wrappers...)
			copied = true
		}
		emitted[i] = true
	}
	return emitted
}
