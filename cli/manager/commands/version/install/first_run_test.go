package install

import "testing"

func TestEnsureRuntimeInstallsWhenMissing(t *testing.T) {
	active := false
	installs := 0
	ok := EnsureRuntime(
		func() bool { return active },
		func() {
			installs++
			active = true
		},
	)
	if !ok || installs != 1 {
		t.Fatalf("EnsureRuntime = %v, installs = %d", ok, installs)
	}
}

func TestEnsureRuntimeKeepsActiveRuntime(t *testing.T) {
	installs := 0
	ok := EnsureRuntime(
		func() bool { return true },
		func() { installs++ },
	)
	if !ok || installs != 0 {
		t.Fatalf("EnsureRuntime = %v, installs = %d", ok, installs)
	}
}

func TestEnsureRuntimeStopsAfterCancelledInstall(t *testing.T) {
	installs := 0
	ok := EnsureRuntime(
		func() bool { return false },
		func() { installs++ },
	)
	if ok || installs != 1 {
		t.Fatalf("EnsureRuntime = %v, installs = %d", ok, installs)
	}
}
