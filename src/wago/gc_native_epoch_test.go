package wago

import (
	"testing"

	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
)

func TestNativeEpochInvalidatesAffectedDomains(t *testing.T) {
	for _, mode := range []string{"local", "imports", "dynamic"} {
		t.Run(mode, func(t *testing.T) {
			var collectors [3]*gc.Collector
			var domains [3]*gcStoreDomain
			for i := range collectors {
				var err error
				collectors[i], err = gc.NewCollector(gc.Config{Profile: gc.ProfileTiny, TinyHeapBytes: 1024, TinyBlockBytes: 16}, nil)
				if err != nil {
					t.Fatal(err)
				}
				defer collectors[i].Close()
				domains[i] = &gcStoreDomain{id: uint64(i + 1), collector: collectors[i]}
			}
			store := &referenceStore{instances: make(map[*Instance]*referenceStoreInstance)}
			in := &Instance{refStore: store}
			state := &referenceStoreInstance{}
			store.instances[in] = state
			var want [3]uint32
			switch mode {
			case "local":
				in.gc, state.gcDomain = collectors[0], domains[0]
				want = [3]uint32{2, 1, 1}
			case "imports":
				in.executionFlags.Store(executionFlagImportedGCDomain)
				state.invocationDomains = &gcInvocationDomainSet{}
				state.invocationDomains.add(domains[0])
				state.invocationDomains.add(domains[1])
				want = [3]uint32{2, 2, 1}
			case "dynamic":
				in.gc, state.gcDomain = collectors[0], domains[0]
				domains[0].private = true
				in.executionFlags.Store(executionFlagDynamicGCDomain)
				state.dynamicInvocationDomains = true
				store.gcDomains = &gcDomainTopology{}
				store.gcDomains.appendDomainLocked(domains[1])
				store.gcDomains.appendDomainLocked(domains[2])
				want = [3]uint32{2, 2, 2}
			}
			in.advanceNativeTypeEpochsAfterHost()
			for i, collector := range collectors {
				if got := collector.NativeTypeEpoch(); got != want[i] {
					t.Fatalf("domain%d epoch%d want%d", i, got, want[i])
				}
			}
		})
	}
}
