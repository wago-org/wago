package settings

import "testing"

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
	if len(names["features"])+len(names["optimizations"]) != len(allKnownBoolean()) {
		t.Fatalf("schema names = %d, registered settings = %d", len(names["features"])+len(names["optimizations"]), len(allKnownBoolean()))
	}
}
