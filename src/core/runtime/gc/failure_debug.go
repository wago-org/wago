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

func injectFailure(point failurePoint) error {
	debugFailures.Lock()
	defer debugFailures.Unlock()
	if debugFailures.point != point {
		return nil
	}
	if debugFailures.after > 0 {
		debugFailures.after--
		return nil
	}
	debugFailures.point = 0
	return errInjectedFailure
}

func armFailure(point failurePoint, after int) func() {
	debugFailures.Lock()
	debugFailures.point, debugFailures.after = point, after
	debugFailures.Unlock()
	return func() {
		debugFailures.Lock()
		debugFailures.point, debugFailures.after = 0, 0
		debugFailures.Unlock()
	}
}
