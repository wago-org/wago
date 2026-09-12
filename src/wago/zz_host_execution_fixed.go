package wago

func (a *hostLoopActivation) dispatchSingleTypedScalarFixedPortal(a0, a1 uint64) uint64 {
	active := a.root
	binding := &active.syncHosts[0]
	state := a.state
	flags := active.executionFlags.Load()
	if flags&(executionFlagIndependent|executionFlagNativeControlShared) != executionFlagIndependent {
		rawSlots, ok := binding.typedScalarSlots()
		if !ok {
			panic("wago: fixed scalar host portal lost its bound signature")
		}
		result, handled := a.dispatchTypedScalarExpandedPortal(a.ctrl, 0, rawSlots, a0, a1)
		if !handled {
			panic("wago: fixed scalar host portal rejected its bound signature")
		}
		return result
	}
	version := state.nativeContextVersion.Load()
	result := binding.callTypedScalar(a0, a1)
	if version == ^uint64(0) || !a.parkedNativeContextReusable ||
		active.executionFlags.Load()&(executionFlagIndependent|executionFlagNativeControlShared) != executionFlagIndependent ||
		state.nativeContextVersion.Load() != version {
		active.restoreTypedScalarNativeContext(a.ctrl)
	}
	return result
}

func (a *hostLoopActivation) dispatchSingleTypedI32x2VoidFixedPortal(a0, a1 uint64) uint64 {
	active := a.root
	binding := &active.syncHosts[0]
	state := a.state
	flags := active.executionFlags.Load()
	if flags&(executionFlagIndependent|executionFlagNativeControlShared) != executionFlagIndependent {
		rawSlots, ok := binding.typedScalarSlots()
		if !ok {
			panic("wago: fixed i32/i32 void host portal lost its bound signature")
		}
		result, handled := a.dispatchTypedScalarExpandedPortal(a.ctrl, 0, rawSlots, a0, a1)
		if !handled {
			panic("wago: fixed i32/i32 void host portal rejected its bound signature")
		}
		return result
	}
	version := state.nativeContextVersion.Load()
	binding.typedI32x2V(AsI32(a0), AsI32(a1))
	if version == ^uint64(0) || !a.parkedNativeContextReusable ||
		active.executionFlags.Load()&(executionFlagIndependent|executionFlagNativeControlShared) != executionFlagIndependent ||
		state.nativeContextVersion.Load() != version {
		active.restoreTypedScalarNativeContext(a.ctrl)
	}
	return 0
}

func (a *hostLoopActivation) dispatchSingleTypedI32PairFixedPortal(a0, _ uint64) uint64 {
	active := a.root
	binding := &active.syncHosts[0]
	state := a.state
	flags := active.executionFlags.Load()
	if flags&(executionFlagIndependent|executionFlagNativeControlShared) != executionFlagIndependent {
		rawSlots, ok := binding.typedScalarSlots()
		if !ok {
			panic("wago: fixed i32 pair-result host portal lost its bound signature")
		}
		result, handled := a.dispatchTypedScalarExpandedPortal(a.ctrl, 0, rawSlots, a0, 0)
		if !handled {
			panic("wago: fixed i32 pair-result host portal rejected its bound signature")
		}
		return result
	}
	version := state.nativeContextVersion.Load()
	first, second := binding.typedI32R2(AsI32(a0))
	result := uint64(uint32(first)) | uint64(uint32(second))<<32
	if version == ^uint64(0) || !a.parkedNativeContextReusable ||
		active.executionFlags.Load()&(executionFlagIndependent|executionFlagNativeControlShared) != executionFlagIndependent ||
		state.nativeContextVersion.Load() != version {
		active.restoreTypedScalarNativeContext(a.ctrl)
	}
	return result
}

func (a *hostLoopActivation) dispatchSingleTypedI32x2PairFixedPortal(a0, a1 uint64) uint64 {
	active := a.root
	binding := &active.syncHosts[0]
	state := a.state
	flags := active.executionFlags.Load()
	if flags&(executionFlagIndependent|executionFlagNativeControlShared) != executionFlagIndependent {
		rawSlots, ok := binding.typedScalarSlots()
		if !ok {
			panic("wago: fixed i32/i32 pair-result host portal lost its bound signature")
		}
		result, handled := a.dispatchTypedScalarExpandedPortal(a.ctrl, 0, rawSlots, a0, a1)
		if !handled {
			panic("wago: fixed i32/i32 pair-result host portal rejected its bound signature")
		}
		return result
	}
	version := state.nativeContextVersion.Load()
	first, second := binding.typedI32x2R2(AsI32(a0), AsI32(a1))
	result := uint64(uint32(first)) | uint64(uint32(second))<<32
	if version == ^uint64(0) || !a.parkedNativeContextReusable ||
		active.executionFlags.Load()&(executionFlagIndependent|executionFlagNativeControlShared) != executionFlagIndependent ||
		state.nativeContextVersion.Load() != version {
		active.restoreTypedScalarNativeContext(a.ctrl)
	}
	return result
}
