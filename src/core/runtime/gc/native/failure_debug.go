//go:build wagodebug

package gc

import (
	"errors"
	"sync"
	"sync/atomic"
)

const failureInjectionEnabled = true

var errInjectedFailure = errors.New("gc: injected failure")

var debugFailures struct {
	sync.Mutex
	armed map[any]debugFailure
}

type debugFailure struct {
	point failurePoint
	after int
}

var stressCollectionSequence atomic.Uint64

func stressFullCollection() bool {
	// SplitMix64 gives every dedicated stress run a deterministic but well-mixed
	// minor/full sequence without collector-local release-build state.
	x := stressCollectionSequence.Add(0x9e3779b97f4a7c15)
	x = (x ^ (x >> 30)) * 0xbf58476d1ce4e5b9
	x = (x ^ (x >> 27)) * 0x94d049bb133111eb
	return (x^(x>>31))&1 != 0
}

func isInjectedFailure(err error) bool { return errors.Is(err, errInjectedFailure) }

func injectFailure(target any, point failurePoint) error {
	debugFailures.Lock()
	defer debugFailures.Unlock()
	failure, ok := debugFailures.armed[target]
	if !ok || failure.point != point {
		return nil
	}
	if failure.after > 0 {
		failure.after--
		debugFailures.armed[target] = failure
		return nil
	}
	delete(debugFailures.armed, target)
	return errInjectedFailure
}

func armFailure(target any, point failurePoint, after int) func() {
	debugFailures.Lock()
	if debugFailures.armed == nil {
		debugFailures.armed = make(map[any]debugFailure)
	}
	debugFailures.armed[target] = debugFailure{point: point, after: after}
	debugFailures.Unlock()
	return func() {
		debugFailures.Lock()
		delete(debugFailures.armed, target)
		debugFailures.Unlock()
	}
}
