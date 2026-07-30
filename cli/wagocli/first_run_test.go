package wagocli

import "testing"

func TestEnsureFirstRunRuntimeInstallsWhenMissing(t *testing.T) {
	active := false
	installCalls := 0
	ok := ensureFirstRunRuntime(
		func() bool { return active },
		func() {
			installCalls++
			active = true
		},
	)
	if !ok || installCalls != 1 {
		t.Fatalf("ensure first runtime = %v, install calls = %d; want true, 1", ok, installCalls)
	}
}

func TestEnsureFirstRunRuntimeStopsAfterCancelledInstall(t *testing.T) {
	installCalls := 0
	ok := ensureFirstRunRuntime(
		func() bool { return false },
		func() { installCalls++ },
	)
	if ok || installCalls != 1 {
		t.Fatalf("ensure first runtime = %v, install calls = %d; want false, 1", ok, installCalls)
	}
}

func TestEnsureFirstRunRuntimeKeepsActiveRuntime(t *testing.T) {
	installCalls := 0
	ok := ensureFirstRunRuntime(
		func() bool { return true },
		func() { installCalls++ },
	)
	if !ok || installCalls != 0 {
		t.Fatalf("ensure first runtime = %v, install calls = %d; want true, 0", ok, installCalls)
	}
}
