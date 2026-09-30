package gc

import "testing"

func TestNilCollectorProfile(t *testing.T) {
	var collector *Collector
	if got := collector.Profile(); got != ProfileThroughput {
		t.Fatalf("nil profile = %v, want throughput", got)
	}
}
