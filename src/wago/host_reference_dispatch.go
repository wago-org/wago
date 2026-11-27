// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package wago

import "fmt"

type hostReferenceCallState struct {
	hostGCResultJournal
	exactTypes *[]DefinedTypeDescriptor
}

func dispatchSyncHostReference(in *Instance, scope *hostCallScope, ctrl uintptr, importIdx uint32, binding *syncHostBinding, sig FuncSig, exact *DefinedTypeDescriptor, exactTypes []DefinedTypeDescriptor, exactTypesPtr *[]DefinedTypeDescriptor, args, results []uint64, invocation hostInvocationContext) {
	if gate := binding.callbackGate(); pluginRuntimeBuilt && gate != nil && !pluginReservationAllows(invocation.reservation, gate) {
		dispatchSyncHostReferenceGated(in, scope, ctrl, importIdx, binding, sig, exact, exactTypes, exactTypesPtr, args, results, invocation)
		return
	}
	if _, ok := binding.fn.(HostCallFunc); ok && !hasReferenceValType(sig.Params) && !hasReferenceValType(sig.Results) {
		binding.callBoundUnchecked(instanceHostModule{}, args, results)
		return
	}
	var exactParams, exactResults []ValueTypeDescriptor
	if exact != nil {
		exactParams, exactResults = exact.Params, exact.Results
	}
	if invocation.callbackScope != nil {
		scope = invocation.callbackScope
	} else {
		scope = railshotCallbackHostScope(in, ctrl, scope, invocation.parent)
	}
	caller := scope.beginReservedWithID(in, invocation.id, invocation.reservation, invocation.parent)
	defer caller.scope.end(caller.generation, caller.parentGeneration)
	if referenceSignaturesBuilt {
		var gcTemps *gcHostTempTokens
		if collectorObjectTokensBuilt {
			var temps gcHostTempTokens
			gcTemps = &temps
			defer temps.release(in)
		}
		if err := in.translateHostReferenceArgsMode(args, sig.Params, exactParams, exactTypes, gcTemps, &caller); err != nil {
			panic(invalidHostReference{err: fmt.Errorf("host import %d: %w", importIdx, err)})
		}
	}
	caller.exact = exact
	if referenceSignaturesBuilt {
		// Ordinary imports borrow the compiled type context directly. A host
		// function reference can carry its own context.
		separateTypes := exactTypesPtr != nil && (in.c == nil || exactTypesPtr != &in.c.Types)
		if separateTypes || wasmGCBuilt && hasReferenceValType(sig.Results) {
			state := &hostReferenceCallState{exactTypes: exactTypesPtr}
			caller.referenceCall = state
			if wasmGCBuilt {
				defer state.releaseGCResults(in)
			}
		}
	}
	if invocation.scope != nil && invocationMayCancel(invocation.parent) {
		scope.invocationContext(caller.generation, invocation.parent)
	}
	binding.callUnchecked(caller, args, results)
	if referenceSignaturesBuilt {
		if err := in.translateHostReferenceResults(invocation.gcActivation, results, sig.Results, exactResults, exactTypes); err != nil {
			panic(invalidHostReference{err: fmt.Errorf("host import %d: %w", importIdx, err)})
		}
	}
}

func dispatchSyncHostReferenceGated(in *Instance, scope *hostCallScope, ctrl uintptr, importIdx uint32, binding *syncHostBinding, sig FuncSig, exact *DefinedTypeDescriptor, exactTypes []DefinedTypeDescriptor, exactTypesPtr *[]DefinedTypeDescriptor, args, results []uint64, invocation hostInvocationContext) {
	gate := binding.callbackGate()
	if err := gate.enter(); err != nil {
		panic(HostTrap{Err: err})
	}
	defer gate.release()
	ungated := *binding
	ungated.setCallbackGate(nil)
	dispatchSyncHostReference(in, scope, ctrl, importIdx, &ungated, sig, exact, exactTypes, exactTypesPtr, args, results, invocation)
}
