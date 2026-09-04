package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/wago-org/wago/internal/installbootstrap"
	"github.com/wago-org/wago/internal/sourcearchive"
)

type installer struct {
	out                 io.Writer
	home                string
	version             string
	repoURL             string
	archiveURL          string
	releaseAPI          string
	releaseDownloadBase string
	binDir              string
	srcDir              string
	dataDir             string
	configDir           string
	cacheDir            string
	binExplicit         bool
	dryRun              bool
	noModifyPath        bool
	noCompletions       bool
	noTUI               bool
	httpClient          *http.Client
	tmpDir              string
	managerTag          string
	managerSourceRef    string
	managerFromRelease  bool
	sourceMethod        string
	progressActive      bool
	progressStop        chan struct{}
	progressDone        chan struct{}
	pathAdded           bool
	pathRefresh         bool
	pathInitiallyReady  bool
	ctx                 context.Context
}

type pathTarget struct {
	label       string
	description string
	shell       string
	configFile  string
	current     bool
}

func runInstaller() error {
	clearPipedCmdHeader()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	i, err := newInstaller(os.Stderr)
	if err != nil {
		return err
	}
	i.ctx = ctx
	err = i.run()
	if i.progressActive {
		i.stopProgress()
	}
	return err
}

var installerSourceArchiveURL = func(repo, ref string) string {
	return "https://api.github.com/repos/" + repo + "/zipball/" + ref
}

func newInstaller(out io.Writer) (*installer, error) {
	home, err := os.UserHomeDir()
	if value := firstEnv("HOME", "USERPROFILE"); value != "" {
		home = value
	}
	if err != nil && home == "" {
		return nil, errors.New("home directory is not available")
	}
	requestedVersion := envOr("WAGO_VERSION", version)
	if requestedVersion == "" || requestedVersion == "dev" {
		requestedVersion = "main"
	}
	archiveRef := installerSourceRef(requestedVersion)
	releaseRepo := envOr("WAGO_RELEASE_REPO", "wago-org/wago")
	binDir := os.Getenv("WAGO_BIN_DIR")
	binExplicit := binDir != ""
	if binDir == "" {
		binDir = filepath.Join(home, ".wago", "bin")
	}
	wagoRoot := envOr("WAGO_HOME", filepath.Join(home, ".wago"))
	dataDir, configDir, cacheDir := platformDirs(home, wagoRoot, os.Getenv("WAGO_HOME") != "")
	i := &installer{
		out:                 out,
		home:                home,
		version:             requestedVersion,
		repoURL:             envOr("WAGO_REPO_URL", "https://github.com/wago-org/wago.git"),
		archiveURL:          envOr("WAGO_ARCHIVE_URL", installerSourceArchiveURL(releaseRepo, archiveRef)),
		releaseAPI:          envOr("WAGO_RELEASES_API_URL", "https://api.github.com/repos/"+releaseRepo+"/releases"),
		releaseDownloadBase: envOr("WAGO_RELEASE_DOWNLOAD_BASE", "https://github.com/"+releaseRepo+"/releases"),
		binDir:              filepath.Clean(binDir),
		srcDir:              filepath.Clean(envOr("WAGO_SRC_DIR", filepath.Join(home, ".wago", "src"))),
		dataDir:             dataDir,
		configDir:           configDir,
		cacheDir:            cacheDir,
		binExplicit:         binExplicit,
		dryRun:              os.Getenv("WAGO_DRY_RUN") == "1",
		noModifyPath:        os.Getenv("WAGO_NO_MODIFY_PATH") == "1",
		noCompletions:       os.Getenv("WAGO_NO_COMPLETIONS") == "1",
		noTUI:               os.Getenv("WAGO_NO_TUI") == "1",
		httpClient:          &http.Client{Timeout: 45 * time.Second},
	}
	i.pathInitiallyReady = pathContains(i.binDir)
	return i, nil
}

func (i *installer) run() error {
	i.welcome()
	if !i.chooseInstallDir() {
		fmt.Fprintln(i.out, "Cancelled.")
		return nil
	}
	i.installLocation()
	if i.dryRun {
		i.plan()
		return nil
	}

	tmpDir, err := os.MkdirTemp("", "wago-install-")
	if err != nil {
		return fmt.Errorf("create temporary directory: %w", err)
	}
	i.tmpDir = tmpDir
	defer os.RemoveAll(tmpDir)

	reinstallMode, ok, err := i.chooseReinstallMode()
	if err != nil {
		return err
	}
	if !ok {
		fmt.Fprintln(i.out, "Cancelled.")
		return nil
	}

	managerPath := filepath.Join(tmpDir, executableName("wago"))
	if err := i.downloadManager(managerPath); err != nil {
		i.retry("Release manager unavailable; building from source")
	}
	sourceDir, err := i.fetchSource()
	if err != nil {
		return err
	}
	if !i.managerFromRelease {
		if err := i.buildManager(sourceDir, managerPath); err != nil {
			return err
		}
	}
	if reinstallMode != "minimal" {
		if err := i.cleanExisting(reinstallMode); err != nil {
			return err
		}
	}
	installed := filepath.Join(i.binDir, executableName("wago"))
	if err := i.installManager(managerPath, installed); err != nil {
		return err
	}
	if err := i.saveSource(sourceDir); err != nil {
		return err
	}
	if err := i.verify(installed); err != nil {
		return err
	}

	pathReady, configFile := i.offerPathSetup()
	if pathReady && configFile != "" && !i.noCompletions {
		i.offerCompletions(installed, configFile)
	}
	i.offerPathRefresh(configFile)
	stamp := i.managerTag
	if stamp == "" {
		stamp = i.version
	}
	i.finish(stamp, installed, pathReady, configFile)
	return nil
}

func (i *installer) offerPathRefresh(configFile string) {
	requestFile := os.Getenv("WAGO_PATH_REFRESH_FILE")
	if !i.pathAdded || requestFile == "" || i.pathInitiallyReady {
		return
	}
	choice := os.Getenv("WAGO_REFRESH_PATH")
	if choice == "" && i.noTUI {
		return
	}
	fmt.Fprintln(i.out)
	question := "Refresh PATH now?"
	if choice == "" {
		value, ok := radio(question, []radioItem{
			{"Yes", "Use Wago without reopening the terminal", "yes", ""},
			{"No", "", "no", ""},
		}, 0)
		if !ok {
			return
		}
		choice = value
	}
	yes := strings.EqualFold(choice, "yes") || strings.EqualFold(choice, "y")
	if yes {
		i.answer(question, "Yes")
		if err := os.WriteFile(requestFile, []byte(configFile+"\n"), 0o600); err != nil {
			i.retry("Could not prepare PATH refresh")
			return
		}
		i.pathRefresh = true
		return
	}
	i.answer(question, "No")
}

func (i *installer) welcome() {
	s := colors()
	fmt.Fprintf(i.out, "%s%sWelcome to Wago!%s Let’s get you set up.\n\n", s.bold, s.cyan, s.reset)
}

func (i *installer) installLocation() {
	i.field("Install location", displayPath(i.binDir, i.home))
}

func (i *installer) field(label, value string) {
	s := colors()
	fmt.Fprintf(i.out, "%s%s:%s %s%s%s\n", s.bold, label, s.reset, s.cyan, value, s.reset)
}

func (i *installer) answer(question, answer string) {
	s := colors()
	fmt.Fprintf(i.out, "%s%s%s %s%s%s\n", s.bold, question, s.reset, s.cyan, answer, s.reset)
}

func (i *installer) plan() {
	s := colors()
	fmt.Fprintf(i.out, "\n%sPlan%s\n", s.bold, s.reset)
	i.detail("Version", i.version)
	i.detail("Command", displayPath(filepath.Join(i.binDir, executableName("wago")), i.home))
	i.detail("Source", displayPath(i.srcDir, i.home))
	fmt.Fprintf(i.out, "\n%sDry run · no changes made.%s\n", s.dim, s.reset)
}

func (i *installer) detail(label, value string) {
	s := colors()
	fmt.Fprintf(i.out, "  %s%-8s%s %s\n", s.dim, label, s.reset, value)
}

func (i *installer) begin(label string) {
	if stderrIsConsole() {
		i.progressStop = make(chan struct{})
		i.progressDone = make(chan struct{})
		i.progressActive = true
		s := colors()
		go func() {
			defer close(i.progressDone)
			ticker := time.NewTicker(80 * time.Millisecond)
			defer ticker.Stop()
			for index := 0; ; index++ {
				clearProgressLine()
				fmt.Fprintf(i.out, "%s%s%s %s", s.dim, spinnerFrames[index%len(spinnerFrames)], s.reset, label)
				select {
				case <-i.progressStop:
					clearProgressLine()
					return
				case <-ticker.C:
				}
			}
		}()
		return
	}
	fmt.Fprintf(i.out, "  … %s\n", label)
}

func (i *installer) stopProgress() {
	close(i.progressStop)
	<-i.progressDone
	i.progressStop = nil
	i.progressDone = nil
	i.progressActive = false
}

func (i *installer) done(label string) {
	s := colors()
	if i.progressActive {
		i.stopProgress()
	}
	fmt.Fprintf(i.out, "%s✓%s %s\n", s.cyan, s.reset, label)
}
func (i *installer) retry(label string) {
	s := colors()
	if i.progressActive {
		i.stopProgress()
	}
	fmt.Fprintf(i.out, "%s→%s %s\n", s.dim, s.reset, label)
}

func (i *installer) chooseInstallDir() bool {
	if i.binExplicit {
		return true
	}
	if choice := os.Getenv("WAGO_INSTALL_CHOICE"); choice != "" {
		if choice == "1" {
			return true
		}
		if choice != "2" {
			return false
		}
		custom := os.Getenv("WAGO_CUSTOM_INSTALL_DIR")
		if custom == "" {
			return false
		}
		i.binDir = resolvePath(custom, mustGetwd(), i.home)
		return true
	}
	if i.noTUI {
		return true
	}
	os.Setenv("WAGO_UI_BIN_DIR", i.binDir)
	os.Setenv("WAGO_UI_CWD", mustGetwd())
	selected, ok := installDir()
	if ok && selected != "" {
		i.binDir = filepath.Clean(selected)
	}
	return ok
}

func (i *installer) chooseReinstallMode() (string, bool, error) {
	installed := filepath.Join(i.binDir, executableName("wago"))
	if _, err := os.Stat(installed); errors.Is(err, os.ErrNotExist) {
		fmt.Fprintln(i.out)
		return "minimal", true, nil
	}
	if value := os.Getenv("WAGO_REINSTALL_MODE"); value != "" {
		if value == "full" || value == "partial" || value == "minimal" {
			i.field("Reinstall method", reinstallLabel(value))
			fmt.Fprintln(i.out)
			return value, true, nil
		}
		return "", false, errors.New("WAGO_REINSTALL_MODE must be full, partial, or minimal")
	}
	if i.noTUI {
		i.field("Reinstall method", "Minimal")
		fmt.Fprintln(i.out)
		return "minimal", true, nil
	}
	value, ok := radio("Reinstall method", []radioItem{
		{"Full", "Reset everything, including plugins and settings", "full", ""},
		{"Partial", "Reset Wago but keep global plugins for reinstall", "partial", ""},
		{"Minimal", "Replace binaries and keep existing state", "minimal", ""},
	}, 2)
	if ok {
		i.field("Reinstall method", reinstallLabel(value))
		fmt.Fprintln(i.out)
	}
	return value, ok, nil
}

func reinstallLabel(mode string) string {
	switch mode {
	case "full":
		return "Full"
	case "partial":
		return "Partial"
	default:
		return "Minimal"
	}
}

func (i *installer) downloadManager(target string) error {
	resolved := installbootstrap.ResolvedRelease{Tag: i.version, SourceRef: installerSourceRef(i.version)}
	base := ""
	if os.Getenv("WAGO_MANAGER_URL") == "" {
		var err error
		resolved, base, err = i.resolveRelease()
		if err != nil {
			return err
		}
	}
	tag := resolved.Tag
	asset, err := installbootstrap.Asset("wago", runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return err
	}
	url := base + "/" + asset
	if override := os.Getenv("WAGO_MANAGER_URL"); override != "" {
		url = override
	}
	i.begin("Downloading Wago manager " + tag)
	if err := i.downloadChecked(url, target); err != nil {
		return err
	}
	if err := os.Chmod(target, 0o755); err != nil {
		return err
	}
	i.managerTag, i.managerSourceRef, i.managerFromRelease = tag, resolved.SourceRef, true
	i.done("Downloaded Wago manager " + tag)
	return nil
}

func (i *installer) resolveRelease() (installbootstrap.ResolvedRelease, string, error) {
	resolved, err := installbootstrap.ResolveRelease(i.version, installerReleaseCatalog{i})
	if err != nil {
		return installbootstrap.ResolvedRelease{}, "", err
	}
	return resolved, i.releaseDownloadBase + "/download/" + resolved.Tag, nil
}

type installerReleaseCatalog struct{ installer *installer }

func (catalog installerReleaseCatalog) Latest() (installbootstrap.Release, error) {
	var item installbootstrap.Release
	err := catalog.installer.getJSON(catalog.installer.releaseAPI+"/latest", &item)
	return item, err
}

func (catalog installerReleaseCatalog) Releases() ([]installbootstrap.Release, error) {
	const pageLimit = 10
	var releases []installbootstrap.Release
	base, err := url.Parse(catalog.installer.releaseAPI)
	if err != nil {
		return nil, fmt.Errorf("parse release catalog URL: %w", err)
	}
	for page := 1; page <= pageLimit; page++ {
		var batch []installbootstrap.Release
		address := *base
		query := address.Query()
		query.Set("per_page", "100")
		query.Set("page", strconv.Itoa(page))
		address.RawQuery = query.Encode()
		if err := catalog.installer.getJSON(address.String(), &batch); err != nil {
			return nil, err
		}
		if len(batch) > 100 {
			return nil, fmt.Errorf("release catalog returned too many releases on page %d", page)
		}
		releases = append(releases, batch...)
		if len(batch) < 100 {
			return releases, nil
		}
	}
	return nil, fmt.Errorf("release catalog exceeded %d pages", pageLimit)
}

func (i *installer) getJSON(url string, value any) error {
	response, err := i.httpClient.Get(url)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode/100 != 2 {
		return fmt.Errorf("%s returned %s", url, response.Status)
	}
	return json.NewDecoder(io.LimitReader(response.Body, 4<<20)).Decode(value)
}

func (i *installer) downloadChecked(url, target string) error {
	payload := target + ".download"
	checksum := target + ".sha256"
	defer os.Remove(payload)
	defer os.Remove(checksum)
	if err := i.download(url, payload); err != nil {
		return err
	}
	checksumURL := url + ".sha256"
	if override := os.Getenv("WAGO_MANAGER_CHECKSUM_URL"); override != "" && os.Getenv("WAGO_MANAGER_URL") != "" {
		checksumURL = override
	}
	if err := i.download(checksumURL, checksum); err != nil {
		return err
	}
	wantData, err := os.ReadFile(checksum)
	if err != nil {
		return err
	}
	if err := installbootstrap.VerifyFile(payload, wantData); err != nil {
		return err
	}
	return os.Rename(payload, target)
}

func (i *installer) download(url, target string) error {
	request, err := http.NewRequestWithContext(i.installContext(), http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	response, err := i.httpClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode/100 != 2 {
		return fmt.Errorf("%s returned %s", url, response.Status)
	}
	file, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	const maxDownloadSize = 256 << 20
	written, copyErr := io.Copy(file, io.LimitReader(response.Body, maxDownloadSize+1))
	closeErr := file.Close()
	if copyErr != nil {
		return copyErr
	}
	if written > maxDownloadSize {
		return errors.New("download exceeds 256 MiB limit")
	}
	return closeErr
}

var runInstallerGit = func(args ...string) ([]byte, error) {
	return exec.Command("git", args...).CombinedOutput()
}

func (i *installer) fetchSource() (string, error) {
	target := filepath.Join(i.tmpDir, "src")
	sourceVersion := installerSourceRef(i.version)
	if i.managerFromRelease && i.managerSourceRef != "" {
		sourceVersion = i.managerSourceRef
	}
	archiveURL := i.archiveURL
	if os.Getenv("WAGO_ARCHIVE_URL") == "" {
		archiveURL = installerSourceArchiveURL(envOr("WAGO_RELEASE_REPO", "wago-org/wago"), sourceVersion)
	}
	i.begin("Fetching Wago source")
	gitLog, gitErr := i.fetchSourceWithGit(target, sourceVersion)
	if gitErr == nil {
		i.sourceMethod = "git"
		i.done("Fetched Wago source")
		return target, nil
	}
	_ = os.RemoveAll(target)
	return i.fetchSourceArchiveAfterGitFailure(target, archiveURL, gitLog, gitErr)
}

func (i *installer) fetchSourceWithGit(target, sourceVersion string) ([]byte, error) {
	if fullInstallerCommitSHA(sourceVersion) {
		sha := sourceVersion
		commands := [][]string{
			{"-c", "init.defaultBranch=main", "init", target},
			{"-C", target, "fetch", "--quiet", "--depth", "1", i.repoURL, sha},
			{"-C", target, "checkout", "--quiet", "--detach", "FETCH_HEAD"},
		}
		for _, args := range commands {
			if output, err := runInstallerGit(args...); err != nil {
				return output, err
			}
		}
		return nil, nil
	}
	return runInstallerGit("clone", "--depth", "1", "--branch", sourceVersion, i.repoURL, target)
}

func (i *installer) fetchSourceArchiveAfterGitFailure(target, archiveURL string, gitLog []byte, gitErr error) (string, error) {
	i.retry("Git fetch failed; trying source archive")
	archive := filepath.Join(i.tmpDir, "source.zip")
	if err := i.download(archiveURL, archive); err != nil {
		return "", fmt.Errorf("fetch source with Git (%v: %s) or archive: %w", gitErr, strings.TrimSpace(string(gitLog)), err)
	}
	if err := sourcearchive.ExtractContext(i.installContext(), archive, target); err != nil {
		return "", fmt.Errorf("unpack source archive: %w", err)
	}
	i.sourceMethod = "archive"
	i.done("Fetched Wago source")
	return target, nil
}

func (i *installer) installContext() context.Context {
	if i.ctx == nil {
		return context.Background()
	}
	return i.ctx
}

func installerSourceRef(version string) string {
	if _, sha, canonical := installerRollingCommit(version); canonical {
		return sha
	}
	return version
}

func fullInstallerCommitSHA(value string) bool {
	if len(value) != 40 {
		return false
	}
	for _, char := range strings.ToLower(value) {
		if !strings.ContainsRune("0123456789abcdef", char) {
			return false
		}
	}
	return true
}

func installerRollingCommit(version string) (channel, sha string, canonical bool) {
	channel, sha, found := strings.Cut(strings.ToLower(strings.TrimSpace(version)), "@")
	if !found || (channel != "canary" && channel != "nightly") || len(sha) != 40 {
		return "", "", false
	}
	for _, char := range sha {
		if !strings.ContainsRune("0123456789abcdef", char) {
			return "", "", false
		}
	}
	return channel, sha, true
}

func (i *installer) buildManager(sourceDir, target string) error {
	i.begin("Building Wago")
	command := exec.Command("go", "build", "-trimpath", "-ldflags", "-s -w -X main.version="+i.version, "-o", target, "./cli/wago")
	command.Dir = sourceDir
	command.Env = append(os.Environ(), "CGO_ENABLED=0")
	output, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("build Wago: %w\n%s", err, output)
	}
	i.done("Built Wago")
	return nil
}

func (i *installer) cleanExisting(mode string) error {
	i.begin("Cleaning existing Wago installation")
	if err := cleanPlatformInstall(mode, i.home, i.binDir, i.srcDir, i.dataDir, i.configDir, i.cacheDir); err != nil {
		return fmt.Errorf("clean existing installation: %w", err)
	}
	i.done("Cleaned existing Wago installation")
	return nil
}

func (i *installer) installManager(source, target string) error {
	return i.installManagerUsing(source, target, os.Rename, isCrossDeviceError)
}

type pathRenamer func(string, string) error

func (i *installer) installManagerUsing(source, target string, rename pathRenamer, crossDevice func(error) bool) error {
	i.begin("Installing Wago")
	if err := os.MkdirAll(i.binDir, 0o755); err != nil {
		return err
	}
	if err := movePathUsing(source, target, rename, crossDevice); err != nil {
		return fmt.Errorf("install Wago command: %w", err)
	}
	if err := os.Chmod(target, 0o755); err != nil {
		return err
	}
	i.done("Installed Wago command")
	return nil
}

func (i *installer) saveSource(source string) error {
	return i.saveSourceUsing(source, os.Rename, isCrossDeviceError)
}

func (i *installer) saveSourceUsing(source string, rename pathRenamer, crossDevice func(error) bool) error {
	i.begin("Saving Wago source")
	if err := os.MkdirAll(filepath.Dir(i.srcDir), 0o755); err != nil {
		return err
	}
	var backupRoot, backup string
	removeBackup := true
	if _, err := os.Stat(i.srcDir); err == nil {
		backupRoot, err = os.MkdirTemp(filepath.Dir(i.srcDir), ".wago-source-backup-")
		if err != nil {
			return err
		}
		defer func() {
			if removeBackup {
				_ = os.RemoveAll(backupRoot)
			}
		}()
		backup = filepath.Join(backupRoot, "source")
		if err := rename(i.srcDir, backup); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := movePathUsing(source, i.srcDir, rename, crossDevice); err != nil {
		if backup != "" {
			if restoreErr := rename(backup, i.srcDir); restoreErr != nil {
				removeBackup = false
				err = errors.Join(err, fmt.Errorf("restore source failed; backup retained at %s: %w", backup, restoreErr))
			}
		}
		return fmt.Errorf("save Wago source: %w", err)
	}
	i.done("Saved Wago source")
	return nil
}

func (i *installer) verify(commandPath string) error {
	i.begin("Verifying installation")
	timeout := 10 * time.Second
	if raw := os.Getenv("WAGO_VERIFY_TIMEOUT"); raw != "" {
		if parsed, err := time.ParseDuration(raw + "s"); err == nil {
			timeout = parsed
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	if err := exec.CommandContext(ctx, commandPath, "self", "--help").Run(); err != nil {
		return fmt.Errorf("verify installed Wago command: %w", err)
	}
	i.done("Verified installation")
	return nil
}

func (i *installer) offerPathSetup() (bool, string) {
	if i.noModifyPath {
		return pathContains(i.binDir), ""
	}
	targets := pathTargets(i.home)
	if len(targets) == 0 {
		return pathContains(i.binDir), ""
	}
	choice := os.Getenv("WAGO_PATH_SETUP")
	if choice == "" && i.noTUI {
		return pathContains(i.binDir), ""
	}
	fmt.Fprintln(i.out)
	if choice == "" {
		items := pathSetupItems(targets)
		value, selected := radio(pathSetupQuestion(), items, 0)
		if !selected {
			return pathContains(i.binDir), ""
		}
		choice = value
	}
	if strings.EqualFold(choice, "no") || strings.EqualFold(choice, "n") || choice == "none" {
		i.answer(pathSetupQuestion(), "No")
		return pathContains(i.binDir), ""
	}
	selectedIndex := 0
	if !strings.EqualFold(choice, "yes") && !strings.EqualFold(choice, "y") {
		parsed, err := strconv.Atoi(choice)
		if err != nil || parsed < 0 || parsed >= len(targets) {
			return pathContains(i.binDir), ""
		}
		selectedIndex = parsed
	}
	target := targets[selectedIndex]
	i.answer(pathSetupSelectionQuestion(target, i.home), "Yes")
	already, err := addPath(i.binDir, target.configFile, target.shell)
	if err != nil {
		i.retry("Could not add Wago to PATH")
		return false, ""
	}
	if already {
		i.done("Wago is already on PATH")
	} else {
		i.pathAdded = true
		i.done("Added Wago to PATH")
	}
	return true, target.configFile
}

func (i *installer) offerCompletions(installed, configFile string) {
	if i.noTUI {
		return
	}
	shellName := shellFromConfig(configFile)
	if shellName != "zsh" && shellName != "bash" && shellName != "fish" {
		return
	}
	fmt.Fprintln(i.out)
	question := "Enable " + shellName + " completions?"
	value, ok := radio(question, []radioItem{
		{"Yes", "Enable command completion", "yes", ""},
		{"No", "", "no", ""},
	}, 0)
	if !ok {
		return
	}
	answer := "No"
	if value == "yes" {
		answer = "Yes"
	}
	i.answer(question, answer)
	if value != "yes" {
		return
	}
	if err := exec.Command(installed, "config", "completions", shellName, "--install", "--rc", configFile).Run(); err != nil {
		i.retry("Could not enable Wago completions")
		return
	}
	i.done("Enabled " + shellName + " completions")
}

func (i *installer) finish(stamp, installed string, pathReady bool, configFile string) {
	s := colors()
	currentPathReady := i.pathInitiallyReady
	pathReady = pathReady || currentPathReady
	fmt.Fprintf(i.out, "\n%sSweet, Wago %s is ready at %s%s\n", s.bold, stamp, displayPath(installed, i.home), s.reset)
	needsPathStep := false
	if !i.pathRefresh && !currentPathReady {
		if i.pathAdded {
			needsPathStep = true
			fmt.Fprintln(i.out)
			if command := sourceCommand(configFile); command != "" {
				fmt.Fprintln(i.out, "Open a new terminal or run:")
				fmt.Fprintf(i.out, "\n%s%s%s\n", s.cyan, command, s.reset)
			} else {
				fmt.Fprintln(i.out, "Open a new terminal.")
			}
		} else if pathReady && configFile != "" {
			needsPathStep = true
			fmt.Fprintln(i.out)
			fmt.Fprintln(i.out, "Open a new terminal or run:")
			fmt.Fprintf(i.out, "\n%s%s%s\n", s.cyan, sourceCommand(configFile), s.reset)
		} else if !pathReady {
			needsPathStep = true
			fmt.Fprintln(i.out)
			fmt.Fprintf(i.out, "Before you continue, add %s to your PATH.\n", displayPath(i.binDir, i.home))
		}
	}
	next := "Now, install the Wago version you want:"
	if needsPathStep {
		next = "Then install the Wago version you want:"
	}
	fmt.Fprintln(i.out, "\n"+next)
	fmt.Fprintf(i.out, "\n%s%s%s\n", s.cyan, "wago version install", s.reset)
}

func executableName(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".exe"
	}
	return name
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func firstEnv(names ...string) string {
	for _, name := range names {
		if value := os.Getenv(name); value != "" {
			return value
		}
	}
	return ""
}

func mustGetwd() string {
	directory, err := os.Getwd()
	if err != nil {
		return "."
	}
	return directory
}
