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
	state       atomic.Uint64 // high bit closes admission; low bits count calls
	drained     chan struct{}
	drainOnce   sync.Once
	inactiveErr error
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
