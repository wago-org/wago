package plugin

import "testing"

func TestPluginUpdateRequiredComparesPseudoVersionCommits(t *testing.T) {
	current := "v0.0.0-20260731012752-8992be17acf6"
	sameCommitDifferentTimestamp := "v0.0.0-20260801000000-8992be17acf6"
	newer := "v0.0.0-20260801000000-a2eab19f1234"

	if pluginUpdateRequired(current, current, sameCommitDifferentTimestamp, false) {
		t.Fatal("matching commit hashes should skip update")
	}
	if !pluginUpdateRequired(current, current, newer, false) {
		t.Fatal("different commit hashes should update")
	}
	if !pluginUpdateRequired(current, current, current, true) {
		t.Fatal("force should update matching commits")
	}
}

func TestPluginUpdateRequiredNeedsBuildAndLockToMatch(t *testing.T) {
	latest := "v0.0.0-20260731012752-8992be17acf6"
	old := "v0.0.0-20260730000000-123456789abc"
	if !pluginUpdateRequired(old, latest, latest, false) {
		t.Fatal("stale build module should update")
	}
	if !pluginUpdateRequired(latest, old, latest, false) {
		t.Fatal("stale lockfile should update")
	}
}

func TestModuleRevision(t *testing.T) {
	const version = "v0.0.0-20260731012752-8992be17acf6"
	if got := moduleRevision(version); got != "8992be17acf6" {
		t.Fatalf("moduleRevision(%q) = %q", version, got)
	}
	if got := shortModuleRevision(version); got != "8992be1" {
		t.Fatalf("shortModuleRevision(%q) = %q", version, got)
	}
	if got := moduleRevision("v1.2.3"); got != "" {
		t.Fatalf("tag revision = %q", got)
	}
}
