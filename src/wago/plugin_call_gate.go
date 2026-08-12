package wago

import (
	"fmt"
	"sync"
	"sync/atomic"
)

const pluginCallGateClosed = uint64(1) << 63

// pluginCallGate owns every callback that can enter one plugin from guest or
// retained native code. Shutdown closes admission before Stop, lets Stop release
// plugin-owned blockers, and then drains admitted calls before teardown proceeds.
// A reference that outlives its Runtime cannot enter stopped plugin code.
type pluginCallGate struct {
	state       atomic.Uint64 // high bit closes admission; low bits count calls or operation reservations
	drained     chan struct{}
	drainOnce   sync.Once
	inactiveErr error
}

// pluginOperationReservation pins the exact plugin generation selected when a
// runtime operation was admitted. Individual callbacks belonging to that
// operation may continue after ordinary callback admission closes, while
// unrelated callbacks still fail closed. A retained reservation keeps every
// participating plugin alive until the operation's terminal callback returns.
type pluginOperationReservation struct {
	gates []*pluginCallGate
	refs  atomic.Int32
}

func reservePluginOperation(gates []*pluginCallGate) (*pluginOperationReservation, error) {
	if len(gates) == 0 {
		return nil, nil
	}
	reservation := &pluginOperationReservation{gates: gates}
	reservation.refs.Store(1)
	for index, gate := range gates {
		if err := gate.enter(); err != nil {
			for prior := index - 1; prior >= 0; prior-- {
				gates[prior].release()
			}
			return nil, err
		}
	}
	return reservation, nil
}

func (r *pluginOperationReservation) release() {
	if r == nil {
		return
	}
	refs := r.refs.Add(-1)
	if refs < 0 {
		panic("wago: plugin operation reservation underflow")
	}
	if refs != 0 {
		return
	}
	for index := len(r.gates) - 1; index >= 0; index-- {
		r.gates[index].release()
	}
}

func (r *pluginOperationReservation) allows(gate *pluginCallGate) bool {
	if r == nil || gate == nil || r.refs.Load() == 0 {
		return false
	}
	for _, candidate := range r.gates {
		if candidate == gate {
			return true
		}
	}
	return false
}

func newPluginCallGate(plugin string) *pluginCallGate {
	return &pluginCallGate{
		drained:     make(chan struct{}),
		inactiveErr: fmt.Errorf("wago: plugin %q callback is inactive: %w", plugin, ErrPermissionDenied),
	}
}

func (g *pluginCallGate) enter() error {
	if g == nil {
		return nil
	}
	for {
		state := g.state.Load()
		if state&pluginCallGateClosed != 0 {
			return g.inactiveErr
		}
		if state&^pluginCallGateClosed == pluginCallGateClosed-1 {
			return fmt.Errorf("wago: plugin callback count overflow")
		}
		if g.state.CompareAndSwap(state, state+1) {
			return nil
		}
	}
}

func (g *pluginCallGate) wrap(fn HostFunc) HostFunc {
	return func(module HostModule, params, results []uint64) {
		if caller, ok := module.(instanceHostModule); ok && caller.reservation != nil && caller.reservation.allows(g) {
			fn(module, params, results)
			return
		}
		if err := g.enter(); err != nil {
			panic(HostTrap{Err: err})
		}
		defer g.release()
		fn(module, params, results)
	}
}

func (g *pluginCallGate) release() {
	state := g.state.Add(^uint64(0))
	if state == pluginCallGateClosed {
		g.signalDrained()
	}
}

func (g *pluginCallGate) signalDrained() { g.drainOnce.Do(func() { close(g.drained) }) }

func (g *pluginCallGate) closeAndWait() error {
	if g == nil {
		return nil
	}
	for {
		state := g.state.Load()
		if state&pluginCallGateClosed != 0 {
			break
		}
		if g.state.CompareAndSwap(state, state|pluginCallGateClosed) {
			if state == 0 {
				g.signalDrained()
			}
			break
		}
	}
	<-g.drained
	return nil
}

// deactivate closes admission without waiting. Stop may use this phase to
// release callbacks already in flight; closeAndWait drains them immediately
// afterward before any stopped code can be entered again.
func (g *pluginCallGate) deactivate() {
	if g == nil {
		return
	}
	for {
		state := g.state.Load()
		if state&pluginCallGateClosed != 0 {
			return
		}
		if g.state.CompareAndSwap(state, state|pluginCallGateClosed) {
			if state == 0 {
				g.signalDrained()
			}
			return
		}
	}
}
