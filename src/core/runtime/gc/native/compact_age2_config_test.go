package gc

import "testing"

func TestCompactAge2RequiresExplicitCompatibleBuild(t *testing.T) {
	for _, cfg := range []Config{
		{ExperimentalCompactAge2: true},
		{ExperimentalCompactAge2: true, ExperimentalDirectOffsets: true},
		{ExperimentalCompactAge2: true, ExperimentalCompactOffsets: true, Profile: ProfileTiny},
		{ExperimentalCompactAge2: true, ExperimentalCompactOffsets: true, DisableCollection: true},
		{ExperimentalCompactAge2: true, ExperimentalCompactOffsets: true, DisableMovingNursery: true},
	} {
		if ValidateConfig(cfg) == nil {
			t.Fatalf("accepted incompatible config: %+v", cfg)
		}
	}
	if !compactAge2Build || !directHeapCompact {
		if ValidateConfig(Config{ExperimentalCompactAge2: true, ExperimentalCompactOffsets: true, NurseryBytes: 128}) == nil {
			t.Fatal("age2 without supported build")
		}
	}
}
