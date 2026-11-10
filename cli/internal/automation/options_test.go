package automation

import (
	"os"
	"os/exec"
	"slices"
	"testing"
)

func resetForTest(t *testing.T) {
	t.Helper()
	Reset()
	for _, name := range []string{EnvJSON, EnvNonInteractive, EnvDryRun, EnvLocked, EnvOffline} {
		t.Setenv(name, "")
	}
	t.Cleanup(Reset)
}

func TestParseLeadingAndEnvironment(t *testing.T) {
	resetForTest(t)
	remaining, err := ParseLeading([]string{"--json", "--no-input", "--offline", "status", "--locked"})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(remaining, []string{"status", "--locked"}) {
		t.Fatalf("remaining = %q", remaining)
	}
	if !JSON() || !NoInput() || !Offline() || DryRun() || Locked() {
		t.Fatalf("options = %#v", Current())
	}
	if _, err := ParseLeading([]string{"version", "list"}); err != nil || !JSON() || !NoInput() || !Offline() {
		t.Fatalf("nested parse discarded outer policy: %v, %#v", err, Current())
	}
	env := Environment([]string{"PATH=/bin", EnvJSON + "=0", EnvLocked + "=1"})
	if !slices.Contains(env, "PATH=/bin") || !slices.Contains(env, EnvJSON+"=1") || !slices.Contains(env, EnvNonInteractive+"=1") || !slices.Contains(env, EnvOffline+"=1") {
		t.Fatalf("environment = %q", env)
	}
	if slices.Contains(env, EnvLocked+"=1") {
		t.Fatalf("disabled state leaked from input environment: %q", env)
	}
}

func TestEnvironmentPolicyAndOfflineGoCommand(t *testing.T) {
	resetForTest(t)
	t.Setenv(EnvDryRun, "yes")
	if !DryRun() {
		t.Fatal("truthy environment setting was ignored")
	}
	if !slices.Contains(Environment(nil), EnvDryRun+"=1") {
		t.Fatal("environment policy was not forwarded")
	}

	t.Setenv(EnvOffline, "1")
	command := exec.Command("go", "version")
	command.Env = append(os.Environ(), "GOPROXY=https://proxy.example")
	ConfigureCommand(command)
	if !slices.Contains(command.Env, "GOPROXY=off") || slices.Contains(command.Env, "GOPROXY=https://proxy.example") {
		t.Fatalf("offline child environment = %q", command.Env)
	}
	if err := RequireOnline("download releases"); err == nil {
		t.Fatal("offline network action was allowed")
	}
}
