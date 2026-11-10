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
