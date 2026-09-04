package version

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/wago-org/wago/cli/internal/automation"
	managerprogress "github.com/wago-org/wago/cli/manager/internal/progress"
	"github.com/wago-org/wago/internal/atomicfile"
	"github.com/wago-org/wago/internal/httpclient"
	"github.com/wago-org/wago/internal/sourcearchive"
	"github.com/wago-org/wago/internal/wagopaths"
)

const sourceArchiveLimit int64 = 256 << 20

var (
	buildRunnerSource    = buildRunnerFromSourceContext
	buildManagerSource   = buildManagerFromSourceContext
	sourceArchiveMaximum = sourceArchiveLimit
)

func sourceRepository() string {
	if value := os.Getenv("WAGO_SOURCE_REPO"); value != "" {
		return value
	}
	return "https://github.com/wago-org/wago.git"
}

func sourceArchiveURL(ref string) string {
	if value := os.Getenv("WAGO_ARCHIVE_URL"); value != "" {
		return value
	}
	if _, sha, rollingCommit := rollingCommitSHA(ref); rollingCommit {
		ref = sha
	}
	return strings.TrimRight(releaseAPI(), "/") + "/repos/wago-org/wago/zipball/" + url.PathEscape(ref)
}

func buildRunnerFromSource(ref string, profile wagopaths.Profile, build wagopaths.Build, dest string, progress *managerprogress.Progress) error {
	return buildRunnerFromSourceContext(context.Background(), ref, profile, build, dest, progress)
}

func buildRunnerFromSourceContext(ctx context.Context, ref string, profile wagopaths.Profile, build wagopaths.Build, dest string, progress *managerprogress.Progress) error {
	temp, source, stamp, err := checkoutWagoSourceContext(ctx, ref, progress)
	if err != nil {
		return err
	}
	defer os.RemoveAll(temp)

	tempFile, err := atomicfile.CreateTemp(dest)
	if err != nil {
		return err
	}
	tempRunner := tempFile.Name()
	if err := tempFile.Close(); err != nil {
		_ = os.Remove(tempRunner)
		return err
	}
	defer os.Remove(tempRunner)
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
		output, buildErr := runSourceCommand(ctx, source, nil, "tinygo", args...)
		if buildErr != nil {
			if progress != nil {
				progress.Fail("TinyGo source build failed")
			}
			if ctxErr := ctx.Err(); ctxErr != nil {
				return ctxErr
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
	output, err := runSourceCommand(ctx, source, append(os.Environ(), "CGO_ENABLED=0"), "go", args...)
	if err != nil {
		if progress != nil {
			progress.Fail("source build failed")
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		return commandFailure("go build", err, output)
	}
	return finishSourceBuild(tempRunner, dest, progress, "built "+string(profile)+" runtime with Go")
}

func buildManagerFromSourceContext(ctx context.Context, ref, dest string, progress *managerprogress.Progress) error {
	temp, source, stamp, err := checkoutWagoSourceContext(ctx, ref, progress)
	if err != nil {
		return err
	}
	defer os.RemoveAll(temp)

	tempFile, err := atomicfile.CreateTemp(dest)
	if err != nil {
		return err
	}
	tempManager := tempFile.Name()
	if err := tempFile.Close(); err != nil {
		_ = os.Remove(tempManager)
		return err
	}
	defer os.Remove(tempManager)
	if progress != nil {
		progress.Begin("building Wago from source")
	}
	args := []string{
		"build", "-trimpath",
		"-ldflags", "-s -w -X main.version=" + stamp,
		"-o", tempManager, "./cli/wago",
	}
	output, err := runSourceCommand(ctx, source, append(os.Environ(), "CGO_ENABLED=0"), "go", args...)
	if err != nil {
		if progress != nil {
			progress.Fail("manager source build failed")
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		return commandFailure("go build", err, output)
	}
	return finishSourceBuild(tempManager, dest, progress, "built Wago with Go")
}

func checkoutWagoSourceContext(ctx context.Context, ref string, progress *managerprogress.Progress) (temp, source, stamp string, err error) {
	return checkoutWagoSourceInContext(ctx, "", ref, progress)
}

func checkoutWagoSourceInContext(ctx context.Context, parent, ref string, progress *managerprogress.Progress) (temp, source, stamp string, err error) {
	if ctx == nil {
		return "", "", "", errors.New("nil source checkout context")
	}
	if err := ctx.Err(); err != nil {
		return "", "", "", err
	}
	if err := automation.RequireOnline("source checkout"); err != nil {
		return "", "", "", err
	}
	temp, err = os.MkdirTemp(parent, ".wago-source-*")
	if err != nil {
		return "", "", "", fmt.Errorf("prepare source build: %w", err)
	}
	source = filepath.Join(temp, "src")
	if progress != nil {
		progress.Begin("fetching source")
	}
	stamp = ref
	gitErr := checkoutWagoSourceWithGit(ctx, ref, source)
	if gitErr != nil {
		_ = os.RemoveAll(source)
		if ctxErr := ctx.Err(); ctxErr != nil {
			if progress != nil {
				progress.Fail("source fetch canceled")
			}
			_ = os.RemoveAll(temp)
			return "", "", "", ctxErr
		}
		if progress != nil {
			progress.Begin("fetching source archive")
		}
		if archiveErr := checkoutWagoSourceArchiveContext(ctx, ref, temp, source); archiveErr != nil {
			if progress != nil {
				progress.Fail("could not fetch source")
			}
			os.RemoveAll(temp)
			return "", "", "", fmt.Errorf("fetch source with Git (%v) or archive: %w", gitErr, archiveErr)
		}
	}
	if _, _, rollingCommit := rollingCommitSHA(ref); rollingCommit {
		stamp = rollingVersionStamp(ref)
	}
	if progress != nil {
		progress.Done("fetched source")
	}
	return temp, source, stamp, nil
}

func checkoutWagoSourceWithGit(ctx context.Context, ref, source string) error {
	if _, sha, rollingCommit := rollingCommitSHA(ref); rollingCommit {
		commands := [][]string{
			{"init", "--quiet", source},
			{"-C", source, "remote", "add", "origin", sourceRepository()},
			{"-C", source, "fetch", "--quiet", "--depth", "1", "origin", sha},
			{"-C", source, "checkout", "--quiet", "--detach", "FETCH_HEAD"},
		}
		for _, args := range commands {
			if output, err := runSourceCommand(ctx, "", nil, "git", args...); err != nil {
				return commandFailure("git "+args[0], err, output)
			}
		}
	} else if output, err := runSourceCommand(ctx, "", nil, "git", "clone", "--depth", "1", "--single-branch", "--branch", ref, "--", sourceRepository(), source); err != nil {
		return commandFailure("git clone", err, output)
	}
	return nil
}

func checkoutWagoSourceArchiveContext(ctx context.Context, ref, temp, source string) error {
	archive := filepath.Join(temp, "source.zip")
	if err := downloadSourceArchiveContext(ctx, sourceArchiveURL(ref), archive); err != nil {
		return err
	}
	return sourcearchive.ExtractContext(ctx, archive, source)
}

func downloadSourceArchiveContext(ctx context.Context, address, target string) error {
	response, err := openReleaseStream(ctx, "source archive download", address)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return &httpStatusError{url: address, code: response.StatusCode, status: response.Status}
	}
	if response.ContentLength > sourceArchiveMaximum {
		return &httpclient.BodyTooLargeError{URL: address, Limit: sourceArchiveMaximum, ContentLength: response.ContentLength}
	}
	file, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	remove := true
	defer func() {
		if remove {
			_ = os.Remove(target)
		}
	}()
	written, copyErr := io.Copy(file, io.LimitReader(response.Body, sourceArchiveMaximum))
	if copyErr == nil && response.ContentLength >= 0 && written != response.ContentLength {
		copyErr = io.ErrUnexpectedEOF
	}
	if copyErr == nil && written == sourceArchiveMaximum {
		var extra [1]byte
		read, readErr := io.ReadFull(response.Body, extra[:])
		if read != 0 {
			copyErr = &httpclient.BodyTooLargeError{URL: address, Limit: sourceArchiveMaximum, ContentLength: -1}
		} else if readErr != nil && readErr != io.EOF {
			copyErr = readErr
		}
	}
	closeErr := file.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	remove = false
	return nil
}

func syncInstalledSource(ref, dest string, progress *managerprogress.Progress) error {
	return syncInstalledSourceContext(context.Background(), ref, dest, progress)
}

func syncInstalledSourceContext(ctx context.Context, ref, dest string, progress *managerprogress.Progress) error {
	parent := filepath.Dir(dest)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return err
	}
	temp, source, _, err := checkoutWagoSourceInContext(ctx, parent, ref, progress)
	if err != nil {
		return err
	}
	defer os.RemoveAll(temp)

	return publishSourceUsing(source, dest, progress, os.Rename)
}

func publishSourceUsing(source, dest string, progress *managerprogress.Progress, rename func(string, string) error) error {
	parent := filepath.Dir(dest)

	backupRoot, err := os.MkdirTemp(parent, ".wago-source-backup-*")
	if err != nil {
		return fmt.Errorf("prepare source backup: %w", err)
	}
	removeBackup := true
	defer func() {
		if removeBackup {
			_ = os.RemoveAll(backupRoot)
		}
	}()
	backup := filepath.Join(backupRoot, "src")

	if progress != nil {
		progress.Begin("updating plugin build source")
	}
	hadSource := false
	if _, statErr := os.Lstat(dest); statErr == nil {
		hadSource = true
		if err := rename(dest, backup); err != nil {
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
	if err := rename(source, dest); err != nil {
		if hadSource {
			if restoreErr := rename(backup, dest); restoreErr != nil {
				removeBackup = false
				err = errors.Join(err, fmt.Errorf("restore source failed; backup retained at %s: %w", backup, restoreErr))
			}
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

func executeSourceCommand(ctx context.Context, dir string, env []string, name string, args ...string) ([]byte, error) {
	if ctx == nil {
		return nil, errors.New("nil source command context")
	}
	command := exec.CommandContext(ctx, name, args...)
	command.Dir = dir
	if env != nil {
		command.Env = env
	}
	return command.CombinedOutput()
}

func finishSourceBuild(tempRunner, dest string, progress *managerprogress.Progress, message string) error {
	if err := atomicfile.CommitTempFile(tempRunner, dest, atomicfile.Options{Mode: 0o755, Sync: true}); err != nil {
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
