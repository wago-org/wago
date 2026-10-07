package wago

func (a *hostLoopActivation) dispatchSingleHostCallFixedView(args, results []uint64) {
	active := a.root
	binding := &active.syncHosts[0]
	if mu := a.localNativeMu(); mu != nil {
		resume := a.parkIndependentHostCallbackWithMu(a.ctrl, mu)
		defer resume.resume(a)
		binding.fn.(HostCallFunc)(HostCall{params: compactHostSlots(args), results: compactHostSlots(results), sig: binding.sig, exact: binding.exact})
		return
	}

	epoch := nativeExecutionEpoch
	nativeExecutionMu.Unlock()
	defer func() {
		nativeExecutionMu.Lock()
		if nativeExecutionEpoch != epoch {
			active.restoreTypedScalarNativeContext(a.ctrl)
		}
	}()
	binding.fn.(HostCallFunc)(HostCall{params: compactHostSlots(args), results: compactHostSlots(results), sig: binding.sig, exact: binding.exact})
}

func (a *hostLoopActivation) dispatchSingleTypedNoneFixedPortal(a0, a1 uint64) uint64 {
	active := a.root
	mu := a.localNativeMu()
	if mu == nil {
		return a.dispatchSingleTypedScalarFixedPortal(a0, a1)
	}
	resume := a.parkIndependentHostCallbackWithMu(a.ctrl, mu)
	defer resume.resume(a)
	active.syncHosts[0].typedNone()
	return 0
}

func (a *hostLoopActivation) dispatchSingleTypedI32VoidFixedPortal(a0, a1 uint64) uint64 {
	active := a.root
	mu := a.localNativeMu()
	if mu == nil {
		return a.dispatchSingleTypedScalarFixedPortal(a0, a1)
	}
	resume := a.parkIndependentHostCallbackWithMu(a.ctrl, mu)
	defer resume.resume(a)
	active.syncHosts[0].typedI32V(AsI32(a0))
	return 0
}

func (a *hostLoopActivation) dispatchSingleTypedI32FixedPortal(a0, a1 uint64) uint64 {
	mu := a.localNativeMu()
	if mu == nil {
		return a.dispatchSingleTypedScalarFixedPortal(a0, a1)
	}
	resume := a.parkIndependentHostCallbackWithMu(a.ctrl, mu)
	defer resume.resume(a)
	return I32(a.root.syncHosts[0].typedI32(AsI32(a0)))
}

func (a *hostLoopActivation) dispatchSingleTypedI32x2FixedPortal(a0, a1 uint64) uint64 {
	mu := a.localNativeMu()
	if mu == nil {
		return a.dispatchSingleTypedScalarFixedPortal(a0, a1)
	}
	resume := a.parkIndependentHostCallbackWithMu(a.ctrl, mu)
	defer resume.resume(a)
	return I32(a.root.syncHosts[0].typedI32x2(AsI32(a0), AsI32(a1)))
}

func (a *hostLoopActivation) dispatchSingleTypedI64FixedPortal(a0, a1 uint64) uint64 {
	active := a.root
	mu := a.localNativeMu()
	if mu == nil {
		return a.dispatchSingleTypedScalarFixedPortal(a0, a1)
	}
	resume := a.parkIndependentHostCallbackWithMu(a.ctrl, mu)
	defer resume.resume(a)
	result := I64(active.syncHosts[0].fn.(func(int64) int64)(AsI64(a0)))
	return result
}

func (a *hostLoopActivation) dispatchSingleTypedI64x2FixedPortal(a0, a1 uint64) uint64 {
	active := a.root
	mu := a.localNativeMu()
	if mu == nil {
		return a.dispatchSingleTypedScalarFixedPortal(a0, a1)
	}
	resume := a.parkIndependentHostCallbackWithMu(a.ctrl, mu)
	defer resume.resume(a)
	result := I64(active.syncHosts[0].fn.(func(int64, int64) int64)(AsI64(a0), AsI64(a1)))
	return result
}

func (a *hostLoopActivation) dispatchSingleTypedF32FixedPortal(a0, a1 uint64) uint64 {
	active := a.root
	mu := a.localNativeMu()
	if mu == nil {
		return a.dispatchSingleTypedScalarFixedPortal(a0, a1)
	}
	resume := a.parkIndependentHostCallbackWithMu(a.ctrl, mu)
	defer resume.resume(a)
	result := F32(active.syncHosts[0].fn.(func(float32) float32)(AsF32(a0)))
	return result
}

func (a *hostLoopActivation) dispatchSingleTypedF32x2FixedPortal(a0, a1 uint64) uint64 {
	active := a.root
	mu := a.localNativeMu()
	if mu == nil {
		return a.dispatchSingleTypedScalarFixedPortal(a0, a1)
	}
	resume := a.parkIndependentHostCallbackWithMu(a.ctrl, mu)
	defer resume.resume(a)
	result := F32(active.syncHosts[0].fn.(func(float32, float32) float32)(AsF32(a0), AsF32(a1)))
	return result
}

func (a *hostLoopActivation) dispatchSingleTypedF64FixedPortal(a0, a1 uint64) uint64 {
	active := a.root
	mu := a.localNativeMu()
	if mu == nil {
		return a.dispatchSingleTypedScalarFixedPortal(a0, a1)
	}
	resume := a.parkIndependentHostCallbackWithMu(a.ctrl, mu)
	defer resume.resume(a)
	result := F64(active.syncHosts[0].fn.(func(float64) float64)(AsF64(a0)))
	return result
}

func (a *hostLoopActivation) dispatchSingleTypedF64x2FixedPortal(a0, a1 uint64) uint64 {
	active := a.root
	mu := a.localNativeMu()
	if mu == nil {
		return a.dispatchSingleTypedScalarFixedPortal(a0, a1)
	}
	resume := a.parkIndependentHostCallbackWithMu(a.ctrl, mu)
	defer resume.resume(a)
	result := F64(active.syncHosts[0].fn.(func(float64, float64) float64)(AsF64(a0), AsF64(a1)))
	return result
}

func (a *hostLoopActivation) dispatchSingleTypedScalarFixedPortal(a0, a1 uint64) uint64 {
	active := a.root
	binding := &active.syncHosts[0]
	mu := a.localNativeMu()
	if mu == nil {
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
	resume := a.parkIndependentHostCallbackWithMu(a.ctrl, mu)
	defer resume.resume(a)
	result := binding.callTypedScalar(a0, a1)
	return result
}

func (a *hostLoopActivation) dispatchSingleTypedI32x2VoidFixedPortal(a0, a1 uint64) uint64 {
	active := a.root
	binding := &active.syncHosts[0]
	mu := a.localNativeMu()
	if mu == nil {
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
	resume := a.parkIndependentHostCallbackWithMu(a.ctrl, mu)
	defer resume.resume(a)
	binding.typedI32x2V(AsI32(a0), AsI32(a1))
	return 0
}

func (a *hostLoopActivation) dispatchSingleTypedI32PairFixedPortal(a0, _ uint64) uint64 {
	active := a.root
	binding := &active.syncHosts[0]
	mu := a.localNativeMu()
	if mu == nil {
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
	resume := a.parkIndependentHostCallbackWithMu(a.ctrl, mu)
	defer resume.resume(a)
	first, second := binding.typedI32R2(AsI32(a0))
	result := uint64(uint32(first)) | uint64(uint32(second))<<32
	return result
}

func (a *hostLoopActivation) dispatchSingleTypedI32x2PairFixedPortal(a0, a1 uint64) uint64 {
	active := a.root
	binding := &active.syncHosts[0]
	mu := a.localNativeMu()
	if mu == nil {
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
	resume := a.parkIndependentHostCallbackWithMu(a.ctrl, mu)
	defer resume.resume(a)
	first, second := binding.typedI32x2R2(AsI32(a0), AsI32(a1))
	result := uint64(uint32(first)) | uint64(uint32(second))<<32
	return result
}
