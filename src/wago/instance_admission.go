package wago

import "sync/atomic"

// The low half counts lifetime leases and closes entry; the high half owns the
// invocation gate. Updating either half preserves concurrent changes to the other.
type instanceInvocationState struct {
	word atomic.Uint64
}

func (s *instanceInvocationState) Load() uint32 { return uint32(s.word.Load()) }

func (s *instanceInvocationState) Store(next uint32) {
	for {
		old := s.word.Load()
		if s.word.CompareAndSwap(old, old&0xffffffff00000000|uint64(next)) {
			return
		}
	}
}

func (s *instanceInvocationState) CompareAndSwap(old, next uint32) bool {
	for {
		word := s.word.Load()
		if uint32(word) != old {
			return false
		}
		if s.word.CompareAndSwap(word, word&0xffffffff00000000|uint64(next)) {
			return true
		}
	}
}

// Standalone gates keep a local word. Instance gates bind their immutable shared
// pointer before publication, so lifetime and gate owners use the same atomic.
type invocationGateState struct {
	local  atomic.Uint32
	shared *atomic.Uint64
}

func (s *invocationGateState) Load() uint32 {
	if s.shared == nil {
		return s.local.Load()
	}
	return uint32(s.shared.Load() >> 32)
}

func (s *invocationGateState) Store(next uint32) {
	if s.shared == nil {
		s.local.Store(next)
		return
	}
	for {
		word := s.shared.Load()
		if s.shared.CompareAndSwap(word, uint64(uint32(word))|uint64(next)<<32) {
			return
		}
	}
}

func (s *invocationGateState) CompareAndSwap(old, next uint32) bool {
	if s.shared == nil {
		return s.local.CompareAndSwap(old, next)
	}
	for {
		word := s.shared.Load()
		if uint32(word>>32) != old {
			return false
		}
		if s.shared.CompareAndSwap(word, uint64(uint32(word))|uint64(next)<<32) {
			return true
		}
	}
}

const instanceFastAdmission = uint64(invocationGateHeld|invocationGateFast)<<32 | 1

func (fn *WasmFunc) tryBeginFastInvocation() bool {
	in := fn.in
	if in.rt != nil || fn.directGate == nil || fn.directGate.state.shared != &in.invocationState.word ||
		!in.invocationState.word.CompareAndSwap(0, instanceFastAdmission) {
		return false
	}
	if in.guestStorageBorrowed() || !in.preparedFastStateValid() {
		in.endFastInvocation(in.pluginState.Load())
		return false
	}
	return true
}

func (in *Instance) endFastInvocation(state *instancePluginState) {
	if !in.invocationState.word.CompareAndSwap(instanceFastAdmission, 0) {
		in.endFastInvocationSlow(state)
	}
}

//go:noinline
func (in *Instance) endFastInvocationSlow(state *instancePluginState) {
	state.invokeMu.Unlock()
	in.endInvocation()
}
