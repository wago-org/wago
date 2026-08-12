package plugin

import "testing"

func TestPluginUpdateRequiredNeedsBuildAndLockToMatch(t *testing.T) {
	latest := "v1.2.3"
	if pluginUpdateRequired(latest, latest, latest, false) {
		t.Fatal("matching state should not update")
	}
	if !pluginUpdateRequired("v1.2.2", latest, latest, false) || !pluginUpdateRequired(latest, "v1.2.2", latest, false) {
		t.Fatal("stale build or lock should update")
	}
	if !pluginUpdateRequired(latest, latest, latest, true) {
		t.Fatal("force should update matching state")
	}
}
