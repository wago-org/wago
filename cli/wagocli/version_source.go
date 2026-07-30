//go:build !wago_lean

package wagocli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/wago-org/wago/internal/wagopaths"
)

var (
	buildRunnerSource  = buildRunnerFromSource
	buildManagerSource = buildManagerFromSource
)

func sourceRepository() string {
	if value := os.Getenv("WAGO_SOURCE_REPO"); value != "" {
		return value
	}
	return "https://github.com/wago-org/wago.git"
}

func buildRunnerFromSource(ref string, profile wagopaths.Profile, build wagopaths.Build, dest string, progress *installProgress) error {
	temp, source, stamp, err := checkoutWagoSource(ref, progress)
	if err != nil {
		return err
	}
	defer os.RemoveAll(temp)

	tempRunner := dest + ".tmp"
	if progress != nil {
		progress.begin("building " + string(profile) + " runtime from source")
	}
	if build == wagopaths.BuildTiny {
		if _, err := exec.LookPath("tinygo"); err != nil {
			return fmt.Errorf("build tiny runtime from source: TinyGo is not installed")
		}
		args := []string{
			"build", "-scheduler=tasks", "-no-debug", "-opt=z", "-gc=conservative",
			"-ldflags", "-X main.version=" + stamp,
		}
		if tags := sourceBuildTag(profile); tags != "" {
			args = append(args, "-tags", tags)
		}
		args = append(args, "-o", tempRunner, "./cli/wago")
		output, buildErr := runSourceCommand(source, nil, "tinygo", args...)
		if buildErr != nil {
			if progress != nil {
				progress.fail("TinyGo source build failed")
			}
			return commandFailure("tinygo build", buildErr, output)
		}
		return finishSourceBuild(tempRunner, dest, progress, "built "+string(profile)+" runtime with TinyGo")
	}

	tags := sourceBuildTag(profile)
	args := []string{"build", "-trimpath", "-ldflags", "-s -w -X main.version=" + stamp}
	if tags != "" {
		args = append(args, "-tags", tags)
	}
	args = append(args, "-o", tempRunner, "./cli/wago")
	output, err := runSourceCommand(source, append(os.Environ(), "CGO_ENABLED=0"), "go", args...)
	if err != nil {
		if progress != nil {
			progress.fail("source build failed")
		}
		return commandFailure("go build", err, output)
	}
	return finishSourceBuild(tempRunner, dest, progress, "built "+string(profile)+" runtime with Go")
}

func buildManagerFromSource(ref, dest string, progress *installProgress) error {
	temp, source, stamp, err := checkoutWagoSource(ref, progress)
	if err != nil {
		return err
	}
	defer os.RemoveAll(temp)

	tempManager := dest + ".tmp"
	if progress != nil {
		progress.begin("building Wago manager from source")
	}
	args := []string{
		"build", "-trimpath", "-tags", "wago_manager",
		"-ldflags", "-s -w -X main.version=" + stamp,
		"-o", tempManager, "./cli/wago",
	}
	output, err := runSourceCommand(source, append(os.Environ(), "CGO_ENABLED=0"), "go", args...)
	if err != nil {
		if progress != nil {
			progress.fail("manager source build failed")
		}
		return commandFailure("go build", err, output)
	}
	return finishSourceBuild(tempManager, dest, progress, "built Wago manager with Go")
}

func checkoutWagoSource(ref string, progress *installProgress) (temp, source, stamp string, err error) {
	temp, err = os.MkdirTemp("", "wago-source-*")
	if err != nil {
		return "", "", "", fmt.Errorf("prepare source build: %w", err)
	}
	source = filepath.Join(temp, "src")
	if progress != nil {
		progress.begin("fetching source")
	}
	stamp = ref
	if sha, canaryCommit := canaryCommitSHA(ref); canaryCommit {
		stamp = canaryCommitVersion(ref)
		commands := [][]string{
			{"init", "--quiet", source},
			{"-C", source, "remote", "add", "origin", sourceRepository()},
			{"-C", source, "fetch", "--quiet", "--depth", "1", "origin", sha},
			{"-C", source, "checkout", "--quiet", "--detach", "FETCH_HEAD"},
		}
		for _, args := range commands {
			if output, err := runSourceCommand("", nil, "git", args...); err != nil {
				if progress != nil {
					progress.fail("could not fetch source")
				}
				os.RemoveAll(temp)
				return "", "", "", commandFailure("git "+args[0], err, output)
			}
		}
	} else if output, err := runSourceCommand("", nil, "git", "clone", "--depth", "1", "--single-branch", "--branch", ref, "--", sourceRepository(), source); err != nil {
		if progress != nil {
			progress.fail("could not fetch source")
		}
		os.RemoveAll(temp)
		return "", "", "", commandFailure("git clone", err, output)
	}
	if progress != nil {
		progress.done("fetched source")
	}
	return temp, source, stamp, nil
}

func sourceBuildTag(profile wagopaths.Profile) string {
	switch profile {
	case wagopaths.ProfileMinimal:
		return "wago_minimal"
	default:
		return ""
	}
}

var runSourceCommand = executeSourceCommand

func executeSourceCommand(dir string, env []string, name string, args ...string) ([]byte, error) {
	command := exec.Command(name, args...)
	command.Dir = dir
	if env != nil {
		command.Env = env
	}
	return command.CombinedOutput()
}

func finishSourceBuild(tempRunner, dest string, progress *installProgress, message string) error {
	if err := os.Chmod(tempRunner, 0o755); err != nil {
		return err
	}
	if err := os.Rename(tempRunner, dest); err != nil {
		return err
	}
	if progress != nil {
		progress.done(message)
	}
	return nil
}

func commandFailure(step string, err error, output []byte) error {
	detail := strings.TrimSpace(string(output))
	if len(detail) > 4096 {
		detail = detail[len(detail)-4096:]
	}
	if detail == "" {
		return fmt.Errorf("%s: %w", step, err)
	}
	return fmt.Errorf("%s: %w\n%s", step, err, detail)
}
