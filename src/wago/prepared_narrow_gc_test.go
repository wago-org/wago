//go:build linux && (amd64 || arm64) && !tinygo

package wago

import (
	"fmt"
	"testing"
	"time"
)

func TestPreparedNumericGCEntryKeepsDomainLease(t *testing.T) {
	c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcAllocatingScalarProducerModule())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	for _, sharedStore := range []bool{false, true} {
		t.Run(fmt.Sprint(sharedStore), func(t *testing.T) {
			store := newReferenceStore(!sharedStore)
			defer store.closeRuntime()
			in, err := instantiateCore(c, InstantiateOptions{store: store, GC: GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, ForceMajorEveryMinor: true, VerifyAfterCollect: true}})
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			fn, err := in.PrepareFunction("allocate")
			if err != nil {
				t.Fatal(err)
			}
			if !fn.scalarFast || fn.directIntFast || in.gc == nil {
				t.Fatal("numeric GC fixture selected invalid entry metadata")
			}
			if in.gcInvocationDomains().len() == 0 {
				t.Fatal("fixture has no registered collector domain")
			}
			lease := in.lockGCInvocation(newInvocationID())
			done := make(chan error, 1)
			go func() {
				out, err := fn.Invoke0()
				if err == nil && (len(out) != 1 || out[0] != 7) {
					err = fmt.Errorf("result = %v", out)
				}
				done <- err
			}()
			select {
			case err := <-done:
				lease.unlock()
				t.Fatalf("prepared GC call bypassed collector lease: %v", err)
			case <-time.After(20 * time.Millisecond):
			}
			lease.unlock()
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("prepared GC call did not resume")
			}
		})
	}
}
