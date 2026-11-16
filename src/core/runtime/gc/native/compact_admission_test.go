package gc

import "testing"

func TestCompactConfigurationIsolation(t *testing.T) {
	if _, e := NewCollector(Config{ExperimentalCompactOffsets: true, ExperimentalDirectOffsets: true}, nil); e == nil {
		t.Fatal("mixed layouts admitted")
	}
	if !directHeapCompact || !compactTargetEnabled {
		if _, e := NewExperimentalCompactCollector(Config{}, nil); e == nil {
			t.Fatal("disabled compact backend admitted")
		}
	}
	if directHeapCompact {
		if _, e := NewExperimentalDirectCollector(Config{}, nil); e == nil {
			t.Fatal("native direct layout admitted in compact build")
		}
		if _, e := NewExperimentalCompactCollector(Config{Profile: ProfileTiny}, nil); e == nil {
			t.Fatal("Tiny compact admitted")
		}
	}
}
