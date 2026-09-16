package wago

// These test-only adapters keep low-level slot-ABI engine fixtures readable
// while the production package exposes only HostCall callback forms.

func testImports(pairs ...any) *Imports {
	im := NewImports()
	for i := 0; i < len(pairs); i += 2 {
		testSetImport(im, pairs[i].(string), pairs[i+1])
	}
	return im
}

func testSetImport(im *Imports, key string, value any) {
	module, name := splitImportKey(key)
	mapKey := importBindingMapKey(module, name)
	im.mu.Lock()
	_, replacing := im.identities[mapKey]
	if replacing {
		im.bindings[mapKey] = value
	}
	im.mu.Unlock()
	if replacing {
		return
	}
	switch fn := value.(type) {
	case slotHostFunc:
		im.HostFunc(module, name, func(caller Caller, call HostCall) {
			fn(caller.instanceHostModule, call.ParamSlots(), call.ResultSlots())
		}).Params()
		im.bindings[importBindingMapKey(module, name)] = CallerHostCallFunc(func(caller Caller, call HostCall) {
			fn(caller.instanceHostModule, call.ParamSlots(), call.ResultSlots())
		})
	case callerSlotHostFunc:
		im.HostFunc(module, name, func(caller Caller, call HostCall) {
			fn(caller, call.ParamSlots(), call.ResultSlots())
		})
	case func(HostModule, []uint64, []uint64):
		im.HostFunc(module, name, func(caller Caller, call HostCall) {
			fn(caller.instanceHostModule, call.ParamSlots(), call.ResultSlots())
		})
	case func(Caller, []uint64, []uint64):
		im.HostFunc(module, name, func(caller Caller, call HostCall) {
			fn(caller, call.ParamSlots(), call.ResultSlots())
		})
	case I32HostEvent:
		im.I32Event(module, name, fn)
	case *HostFuncRef, *InstanceExport:
		im.Function(module, name, fn)
	default:
		if isHostCallback(value) || isHostCallCallback(value) {
			im.HostFunc(module, name, value)
		} else {
			im.bind(module, name, value)
		}
	}
}

func testCloneImports(source *Imports) *Imports {
	clone := NewImports()
	if source == nil {
		return clone
	}
	source.mu.Lock()
	defer source.mu.Unlock()
	for mapKey, identity := range source.identities {
		testSetImport(clone, identity.module+"."+identity.name, source.bindings[mapKey])
	}
	return clone
}

func testImportKey(key string) string {
	module, name := splitImportKey(key)
	return importBindingMapKey(module, name)
}

func testRegisterHostFunc(reg *HostImportRegistrar, module, name string, fn any) *ImportFuncBuilder {
	switch callback := fn.(type) {
	case slotHostFunc:
		fn = CallerHostCallFunc(func(caller Caller, call HostCall) {
			callback(caller.instanceHostModule, call.ParamSlots(), call.ResultSlots())
		})
	case callerSlotHostFunc:
		fn = CallerHostCallFunc(func(caller Caller, call HostCall) { callback(caller, call.ParamSlots(), call.ResultSlots()) })
	case func(HostModule, []uint64, []uint64):
		fn = CallerHostCallFunc(func(caller Caller, call HostCall) {
			callback(caller.instanceHostModule, call.ParamSlots(), call.ResultSlots())
		})
	case func(Caller, []uint64, []uint64):
		fn = CallerHostCallFunc(func(caller Caller, call HostCall) { callback(caller, call.ParamSlots(), call.ResultSlots()) })
	}
	return reg.HostFunc(module, name, fn)
}
