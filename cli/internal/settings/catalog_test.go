package settings

import (
	"testing"

	"github.com/wago-org/wago/cli/internal/project"
)

func TestRegisteredBooleanSettingOwnsValueAccess(t *testing.T) {
	config := Default()
	for _, setting := range AllBoolean() {
		if setting.Name() == "" {
			t.Fatalf("setting %q has no unqualified name", setting.Key)
		}
		before := setting.Value(config)
		setting.SetValue(&config, !before)
		if got := setting.Value(config); got == before {
			t.Fatalf("setting %q value remained %v", setting.Key, got)
		}
		setting.SetValue(&config, before)
	}
}

func TestSchemaNamesComeFromRegisteredSettings(t *testing.T) {
	names := SchemaNames()
	want := len(allKnownBoolean()) + len(project.RetiredOptimizationNames())
	if len(names["features"])+len(names["optimizations"]) != want {
		t.Fatalf("schema names = %d, active plus retired v1 settings = %d", len(names["features"])+len(names["optimizations"]), want)
	}
}

// Lookup and CanonicalKey run once per configured value during every CLI start,
// so they must consult the cached catalog instead of rebuilding it per call.
func TestLookupDoesNotRebuildCatalog(t *testing.T) {
	setting := allKnownBoolean()[len(allKnownBoolean())-1]
	short := setting.Name()
	if _, ok := Lookup(short); !ok {
		t.Fatalf("Lookup(%q) failed", short)
	}
	allocs := testing.AllocsPerRun(100, func() {
		if _, ok := Lookup(setting.Key); !ok {
			t.Fatalf("Lookup(%q) failed", setting.Key)
		}
		_ = CanonicalKey(short)
	})
	if allocs > 4 {
		t.Fatalf("Lookup+CanonicalKey allocate %.0f times per call; the catalog is being rebuilt", allocs)
	}
}
