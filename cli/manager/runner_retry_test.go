package manager

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
)

type resolvedRunnerExitCoder interface {
	runnerExitCode(error) (int, bool)
}

func runResolvedRunnerForTest(environment commandEnvironment, path string, resolve func() (string, error), launch func(string) error) error {
	if retryer, ok := any(environment).(resolvedRunnerRetryer); ok {
		return retryer.runResolvedRunner(path, resolve, launch)
	}
	return launch(path)
}

func runnerExitCodeForTest(environment commandEnvironment, err error) (int, bool) {
	if classifier, ok := any(environment).(resolvedRunnerExitCoder); ok {
		return classifier.runnerExitCode(err)
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode(), true
	}
	return 0, false
}

func TestResolvedRunnerRetryBoundaries(t *testing.T) {
	missing := &os.PathError{Op: "fork/exec", Path: "runner", Err: os.ErrNotExist}
	denied := &os.PathError{Op: "fork/exec", Path: "runner", Err: os.ErrPermission}
	refreshErr := errors.New("refresh failed")
	for _, test := range []struct {
		name        string
		launchErr   error
		resolveErr  error
		wantErr     error
		wantLaunch  int
		wantResolve int
	}{
		{name: "success", wantLaunch: 1},
		{name: "permission denied", launchErr: denied, wantErr: denied, wantLaunch: 1},
		{name: "missing runtime", launchErr: missing, wantErr: missing, wantLaunch: 3, wantResolve: 2},
		{name: "wrapped missing runtime", launchErr: fmt.Errorf("start: %w", missing), wantErr: missing, wantLaunch: 3, wantResolve: 2},
		{name: "refresh failure", launchErr: missing, resolveErr: refreshErr, wantErr: refreshErr, wantLaunch: 1, wantResolve: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			launches, resolves := 0, 0
			environment := commandEnvironment{}
			err := runResolvedRunnerForTest(environment, "runner-0", func() (string, error) {
				resolves++
				if resolves > test.wantResolve {
					t.Fatal("unexpected runner refresh")
				}
				return fmt.Sprintf("runner-%d", resolves), test.resolveErr
			}, func(path string) error {
				if path != fmt.Sprintf("runner-%d", resolves) {
					t.Fatalf("launch path = %q, want refreshed runner-%d", path, resolves)
				}
				launches++
				if launches > test.wantLaunch {
					t.Fatal("runner launch exceeded retry bound")
				}
				return test.launchErr
			})
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("launch error = %v, want %v", err, test.wantErr)
			}
			if launches != test.wantLaunch || resolves != test.wantResolve {
				t.Fatalf("launch/refresh counts = %d/%d, want %d/%d", launches, resolves, test.wantLaunch, test.wantResolve)
			}
			if test.resolveErr != nil && err.Error() != "refresh runner after concurrent plugin publication: "+refreshErr.Error() {
				t.Fatalf("refresh error lost context: %v", err)
			}
		})
	}
}

func TestResolvedRunnerRetryExec(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, exitCode := range []int{0, 23} {
		t.Run(strconv.Itoa(exitCode), func(t *testing.T) {
			launches, resolves := 0, 0
			// Keep the native executable suffix so Windows exercises process start,
			// as real runtime .exe paths do, rather than PATHEXT lookup.
			missing := filepath.Join(t.TempDir(), filepath.Base(executable))
			environment := commandEnvironment{}
			err := runResolvedRunnerForTest(environment, missing, func() (string, error) {
				resolves++
				if resolves != 1 {
					t.Fatal("retried a runtime that already started")
				}
				return executable, nil
			}, func(path string) error {
				launches++
				cmd := exec.Command(path, "-test.run=^TestResolvedRunnerRetryChild$")
				cmd.Env = append(os.Environ(), "WAGO_TEST_RUNNER_RETRY_EXIT="+strconv.Itoa(exitCode))
				return cmd.Run()
			})
			if exitCode == 0 {
				if err != nil {
					t.Fatalf("launch refreshed runtime: %v", err)
				}
			} else {
				var exitErr *exec.ExitError
				if !errors.As(err, &exitErr) || exitErr.ExitCode() != exitCode {
					t.Fatalf("runtime exit = %v, want exit code %d", err, exitCode)
				}
			}
			if launches != 2 || resolves != 1 {
				t.Fatalf("launch/refresh counts = %d/%d, want 2/1", launches, resolves)
			}
		})
	}
}

func TestResolvedRunnerRetryChild(t *testing.T) {
	if value := os.Getenv("WAGO_TEST_RUNNER_RETRY_EXIT"); value != "" {
		exitCode, err := strconv.Atoi(value)
		if err != nil {
			t.Fatal(err)
		}
		os.Exit(exitCode)
	}
}

func TestRunnerExitCodePreservesRefreshFailure(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(executable, "-test.run=^TestResolvedRunnerRetryChild$")
	cmd.Env = append(os.Environ(), "WAGO_TEST_RUNNER_RETRY_EXIT=23")
	toolErr := cmd.Run()
	var exitErr *exec.ExitError
	if !errors.As(toolErr, &exitErr) || exitErr.ExitCode() != 23 {
		t.Fatalf("child exit = %v, want exit code 23", toolErr)
	}
	environment := commandEnvironment{}
	if code, ok := runnerExitCodeForTest(environment, toolErr); !ok || code != 23 {
		t.Fatalf("direct runner exit = %d/%v, want 23/true", code, ok)
	}
	wrappedToolErr := fmt.Errorf("refresh runner after concurrent plugin publication: read go.mod: %w", toolErr)
	if code, ok := runnerExitCodeForTest(environment, wrappedToolErr); ok {
		t.Errorf("wrapped tool failure classified as runner exit %d; its diagnostic would be lost", code)
	}
	refreshErr := runResolvedRunnerForTest(environment, "missing", func() (string, error) {
		return "", fmt.Errorf("read go.mod: %w", toolErr)
	}, func(string) error {
		return os.ErrNotExist
	})
	if !errors.Is(refreshErr, toolErr) {
		t.Errorf("refresh error = %v, want wrapped tool exit", refreshErr)
	}
	if code, ok := runnerExitCodeForTest(environment, refreshErr); ok {
		t.Fatalf("refresh failure classified as runner exit %d; its diagnostic would be lost", code)
	}
}
