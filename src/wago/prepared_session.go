package wago

import "fmt"

// PreparedSession reserves one instance for repeated calls to a prepared
// function. Holding the reservation removes per-call lifecycle and invocation
// gate traffic. The session and its instance are not safe for concurrent use;
// Close must be called before the instance is closed or shared resources are
// exported.
type PreparedSession struct {
	fn     *PreparedFunction
	lease  preparedInvocationLease
	fast   bool
	native bool
	host   bool
	entry  executionLease
	linMem uintptr
}

// OpenSession acquires an instance reservation for repeated calls. Numeric
// signatures use the specialized scalar paths; other signatures retain the
// prepared general dispatcher while sharing the same reservation.
func (fn *PreparedFunction) OpenSession() (*PreparedSession, error) {
	if fn == nil || fn.in == nil {
		return nil, fmt.Errorf("wago: open prepared session: closed prepared function")
	}
	in := fn.in
	if err := in.beginInvocation(); err != nil {
		return nil, fmt.Errorf("wago: open prepared session: %w", err)
	}
	s := &PreparedSession{fn: fn}
	if fn.directIntFast && fn.directIsolated && in.tryPreparedDirect() {
		s.fast = true
		return s, nil
	}
	s.lease = in.lockPreparedInvocation()
	// Synchronous host callbacks still park the independent native lease while
	// arbitrary Go runs. Reserving it across outer calls only removes repeated
	// context binding; callback re-entry and host-side access retain the normal
	// unlock/reacquire protocol.
	if fn.scalarFast && in.syncMode && in.usesIndependentExecution() {
		entry, err := in.beginNativeEntry()
		if err == nil {
			if err = in.jm.RebindTrapCell(in.trap); err != nil {
				entry.unlockExecution()
				s.lease.unlock()
				in.endInvocation()
				return nil, fmt.Errorf("wago: open prepared session host trap cell: %w", err)
			}
			in.jm.SetStackFence(in.eng.StackLimit())
			in.jm.SetCustomCtx(offHeapSlicePtr(in.ctrl))
			s.host = true
			s.entry = entry
			s.linMem = in.jm.LinMemBase()
			return s, nil
		}
	}
	// A private, import-free instance cannot re-enter Go or transfer its native
	// context to another instance. Reserve the process native context once for the
	// session so signal-bounds calls do not lock and rebind it on every entry.
	if fn.scalarFast && in.c != nil && in.c.NumImports == 0 && len(in.hostLog) == 0 &&
		in.preparedEntryModeFor(true) != preparedEntryGeneral {
		nativeExecutionMu.Lock()
		if in.lockPreparedFastState() {
			nativeExecutionEpoch++
			if err := validateNativeGCEntry(in); err == nil {
				if err = refreshNativeControl(true, in.eng, in.jm, in.trap); err == nil {
					s.native = true
					s.linMem = in.jm.LinMemBase()
					return s, nil
				}
			}
			in.unlockPreparedFastState()
		}
		nativeExecutionMu.Unlock()
	}
	return s, nil
}

// Close releases the session reservation. It is safe to call more than once.
func (s *PreparedSession) Close() {
	if s == nil || s.fn == nil {
		return
	}
	fn := s.fn
	s.fn = nil
	if s.fast {
		fn.in.ensurePluginState().invokeMu.Unlock()
	} else {
		if s.host {
			s.entry.unlockExecution()
		} else if s.native {
			fn.in.unlockPreparedFastState()
			nativeExecutionMu.Unlock()
		}
		s.lease.unlock()
	}
	fn.in.endInvocation()
}

// Invoke calls the reserved prepared function. Returned results have the same
// instance-owned lifetime as PreparedFunction.Invoke results.
func (s *PreparedSession) Invoke(args ...uint64) ([]uint64, error) {
	if s != nil && s.fn != nil {
		switch len(args) {
		case 0:
			return s.invokeFixed(0, 0, 0, 0, 0)
		case 1:
			return s.invokeFixed(1, args[0], 0, 0, 0)
		case 2:
			return s.invokeFixed(2, args[0], args[1], 0, 0)
		case 3:
			return s.invokeFixed(3, args[0], args[1], args[2], 0)
		case 4:
			return s.invokeFixed(4, args[0], args[1], args[2], args[3])
		}
	}
	return s.invokeArgs(args)
}

// Invoke0 calls the reserved function with no argument slots.
func (s *PreparedSession) Invoke0() ([]uint64, error) { return s.invokeFixed(0, 0, 0, 0, 0) }

// Invoke1 calls the reserved function with one argument slot.
func (s *PreparedSession) Invoke1(a0 uint64) ([]uint64, error) { return s.invokeFixed(1, a0, 0, 0, 0) }

// Invoke2 calls the reserved function with two argument slots.
func (s *PreparedSession) Invoke2(a0, a1 uint64) ([]uint64, error) {
	return s.invokeFixed(2, a0, a1, 0, 0)
}

// Invoke3 calls the reserved function with three argument slots.
func (s *PreparedSession) Invoke3(a0, a1, a2 uint64) ([]uint64, error) {
	return s.invokeFixed(3, a0, a1, a2, 0)
}

// Invoke4 calls the reserved function with four argument slots.
func (s *PreparedSession) Invoke4(a0, a1, a2, a3 uint64) ([]uint64, error) {
	return s.invokeFixed(4, a0, a1, a2, a3)
}

func (s *PreparedSession) invokeFixed(count int, a0, a1, a2, a3 uint64) ([]uint64, error) {
	if s == nil || s.fn == nil {
		return nil, fmt.Errorf("wago: invoke closed prepared session")
	}
	fn := s.fn
	if count != fn.paramSlots {
		return nil, fmt.Errorf("%s expects %d arg slot(s), got %d", fn.export, fn.paramSlots, count)
	}
	if s.fast {
		return fn.invokeDirectIntSession(a0, a1, a2, a3)
	}
	args := [4]uint64{a0, a1, a2, a3}
	if s.host {
		return fn.invokeScalarHostReserved(args[:count])
	}
	if s.native {
		return fn.invokeScalarReserved(args[:count], s.linMem)
	}
	if fn.scalarFast {
		return fn.invokeScalarAdmitted(args[:count])
	}
	return fn.invokeGeneralAdmitted(args[:count])
}

func (s *PreparedSession) invokeArgs(args []uint64) ([]uint64, error) {
	if s == nil || s.fn == nil {
		return nil, fmt.Errorf("wago: invoke closed prepared session")
	}
	fn := s.fn
	if len(args) != fn.paramSlots {
		return nil, fmt.Errorf("%s expects %d arg slot(s), got %d", fn.export, fn.paramSlots, len(args))
	}
	if s.host {
		return fn.invokeScalarHostReserved(args)
	}
	if s.native {
		return fn.invokeScalarReserved(args, s.linMem)
	}
	if fn.scalarFast {
		return fn.invokeScalarAdmitted(args)
	}
	return fn.invokeGeneralAdmitted(args)
}
