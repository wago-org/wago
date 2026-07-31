package version

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	managerprogress "github.com/wago-org/wago/cli/manager/internal/progress"
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

func buildRunnerFromSource(ref string, profile wagopaths.Profile, build wagopaths.Build, dest string, progress *managerprogress.Progress) error {
	temp, source, stamp, err := checkoutWagoSource(ref, progress)
	if err != nil {
		return err
	}
	defer os.RemoveAll(temp)

	tempRunner := dest + ".tmp"
	if progress != nil {
		progress.Begin("building " + string(profile) + " runtime from source")
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
				progress.Fail("TinyGo source build failed")
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
			progress.Fail("source build failed")
		}
		return commandFailure("go build", err, output)
	}
	return finishSourceBuild(tempRunner, dest, progress, "built "+string(profile)+" runtime with Go")
}

func buildManagerFromSource(ref, dest string, progress *managerprogress.Progress) error {
	temp, source, stamp, err := checkoutWagoSource(ref, progress)
	if err != nil {
		return err
	}
	defer os.RemoveAll(temp)

	tempManager := dest + ".tmp"
	if progress != nil {
		progress.Begin("building Wago from source")
	}
	args := []string{
		"build", "-trimpath",
		"-ldflags", "-s -w -X main.version=" + stamp,
		"-o", tempManager, "./cli/wago",
	}
	output, err := runSourceCommand(source, append(os.Environ(), "CGO_ENABLED=0"), "go", args...)
	if err != nil {
		if progress != nil {
			progress.Fail("manager source build failed")
		}
		return commandFailure("go build", err, output)
	}
	return finishSourceBuild(tempManager, dest, progress, "built Wago with Go")
}

func checkoutWagoSource(ref string, progress *managerprogress.Progress) (temp, source, stamp string, err error) {
	return checkoutWagoSourceIn("", ref, progress)
}

func checkoutWagoSourceIn(parent, ref string, progress *managerprogress.Progress) (temp, source, stamp string, err error) {
	temp, err = os.MkdirTemp(parent, ".wago-source-*")
	if err != nil {
		return "", "", "", fmt.Errorf("prepare source build: %w", err)
	}
	source = filepath.Join(temp, "src")
	if progress != nil {
		progress.Begin("fetching source")
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
					progress.Fail("could not fetch source")
				}
				os.RemoveAll(temp)
				return "", "", "", commandFailure("git "+args[0], err, output)
			}
		}
	} else if output, err := runSourceCommand("", nil, "git", "clone", "--depth", "1", "--single-branch", "--branch", ref, "--", sourceRepository(), source); err != nil {
		if progress != nil {
			progress.Fail("could not fetch source")
		}
		os.RemoveAll(temp)
		return "", "", "", commandFailure("git clone", err, output)
	}
	if progress != nil {
		progress.Done("fetched source")
	}
	return temp, source, stamp, nil
}

func syncInstalledSource(ref, dest string, progress *managerprogress.Progress) error {
	parent := filepath.Dir(dest)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return err
	}
	temp, source, _, err := checkoutWagoSourceIn(parent, ref, progress)
	if err != nil {
		return err
	}
	defer os.RemoveAll(temp)

	backupRoot, err := os.MkdirTemp(parent, ".wago-source-backup-*")
	if err != nil {
		return fmt.Errorf("prepare source backup: %w", err)
	}
	defer os.RemoveAll(backupRoot)
	backup := filepath.Join(backupRoot, "src")

	if progress != nil {
		progress.Begin("updating plugin build source")
	}
	hadSource := false
	if _, statErr := os.Lstat(dest); statErr == nil {
		hadSource = true
		if err := os.Rename(dest, backup); err != nil {
			if progress != nil {
				progress.Fail("could not replace plugin build source")
			}
			return err
		}
	} else if !os.IsNotExist(statErr) {
		if progress != nil {
			progress.Fail("could not inspect plugin build source")
		}
		return statErr
	}
	if err := os.Rename(source, dest); err != nil {
		if hadSource {
			_ = os.Rename(backup, dest)
		}
		if progress != nil {
			progress.Fail("could not replace plugin build source")
		}
		return err
	}
	if progress != nil {
		progress.Done("updated plugin build source")
	}
	return nil
}

func sourceBuildTag(profile wagopaths.Profile) string {
	switch profile {
	case wagopaths.ProfileMinimal:
		return "wago_runtime,wago_minimal"
	default:
		return "wago_runtime"
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

func finishSourceBuild(tempRunner, dest string, progress *managerprogress.Progress, message string) error {
	if err := os.Chmod(tempRunner, 0o755); err != nil {
		return err
	}
	if err := os.Rename(tempRunner, dest); err != nil {
		return err
	}
	if progress != nil {
		progress.Done(message)
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
