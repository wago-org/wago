package wago

import (
	"testing"

	"github.com/wago-org/wago/src/core/runtime/gc/native"
)

func TestHostGCSuspensionAdmission(t *testing.T) {
	for _, tc := range []struct {
		name  string
		local bool
		flags uint32
		want  bool
	}{
		{"private", false, executionFlagIndependent, false},
		{"local", true, 0, true},
		{"imported", false, executionFlagImportedGCDomain, true},
		{"dynamic-empty", false, executionFlagDynamicGCDomain, true},
		{"dynamic-imported", false, executionFlagDynamicGCDomain | executionFlagImportedGCDomain, true},
		{"store-owned-uncertain", false, executionFlagStoreOwnedGCCollector, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var in Instance
			if tc.local {
				in.gc = new(gc.Collector)
			}
			in.executionFlags.Store(tc.flags)
			if got := in.hostCallNeedsGCSuspension(); got != tc.want {
				t.Fatalf("suspension=%v, want %v", got, tc.want)
			}
		})
	}
}
