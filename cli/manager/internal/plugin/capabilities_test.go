package plugin

import "testing"

func TestCapabilityHelpers(t *testing.T) {
	if CapabilityDescription("host.imports") == "" || CapabilityDescription("unknown") != "" {
		t.Fatal("capability description lookup mismatch")
	}
	if !ContainsDependency([]string{"github.com/acme/plugin", "other"}, "acme/plugin") ||
		ContainsDependency([]string{"acme/plugin"}, "other") {
		t.Fatal("dependency identity lookup mismatch")
	}
}
