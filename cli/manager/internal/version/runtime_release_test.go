package version

import (
	"os"
	"testing"
)

func TestRunnerReleaseDoesNotExecuteCurrentRuntime(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	old := runtimeVersionOutput
	t.Cleanup(func() { runtimeVersionOutput = old })
	runtimeVersionOutput = func(string) ([]byte, error) {
		t.Fatal("RuntimeRelease executed the current runtime recursively")
		return nil, nil
	}
	if got := RuntimeRelease(executable, "canary"); got != "canary" {
		t.Fatalf("RuntimeRelease(current) = %q, want fallback", got)
	}
}

func TestReleaseFromOutput(t *testing.T) {
	for _, test := range []struct {
		name, output, fallback, want string
	}{
		{name: "diagnostic", output: "Wago\n  channel      canary\n  release      canary-20260729-7d8c58a\n", fallback: "canary", want: "canary-20260729-7d8c58a"},
		{name: "legacy", output: "wago v0.2.0 (darwin/arm64)\n", fallback: "canary", want: "v0.2.0"},
		{name: "fallback", output: "unknown\n", fallback: "nightly", want: "nightly"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := ReleaseFromOutput([]byte(test.output), test.fallback); got != test.want {
				t.Fatalf("release = %q, want %q", got, test.want)
			}
		})
	}
}
