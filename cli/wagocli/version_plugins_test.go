package wagocli

import (
	"os"
	"testing"
)

func TestRunnerReleaseDoesNotExecuteCurrentRuntime(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	old := runnerVersionOutput
	t.Cleanup(func() { runnerVersionOutput = old })
	runnerVersionOutput = func(string) ([]byte, error) {
		t.Fatal("runnerRelease executed the current runtime recursively")
		return nil, nil
	}
	if got := runnerRelease(executable, "canary"); got != "canary" {
		t.Fatalf("runnerRelease(current) = %q, want fallback", got)
	}
}
