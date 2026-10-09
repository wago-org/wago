//go:build linux && amd64 && wago_sumunroll

package wagobench

import (
	wago "github.com/wago-org/wago"
	"os"
	"testing"
)

// Public compilation must not gain allocation objects from the opt-in reserve.
// Compare in one process with the same module, feature set and worker count.
func TestSumUnrollMitigationAllocations(t *testing.T) {
	v := os.Getenv("WAGO_SUM_VARIANT")
	if v != "DR" && v != "PR" {
		t.Skip("capacity candidate only")
	}
	for _, entry := range loadCorpus(t) {
		if entry.ID != "memory" {
			continue
		}
		compile := func() {
			c, err := wago.Compile(entry.bytes)
			if err != nil {
				t.Fatal(err)
			}
			if err = c.Close(); err != nil {
				t.Fatal(err)
			}
		}
		candidate := testing.AllocsPerRun(25, compile)
		setSumUnrollMeasurement(0, false, 0)
		defer setSumUnrollMeasurement(16, false, 0)
		baseline := testing.AllocsPerRun(25, compile)
		if candidate != baseline {
			t.Fatalf("public compile allocations: candidate=%g baseline=%g", candidate, baseline)
		}
		return
	}
	t.Fatal("memory corpus entry missing")
}
