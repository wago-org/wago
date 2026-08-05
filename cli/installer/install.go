package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

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
	managerFromRelease  bool
	sourceMethod        string
	progressActive      bool
	progressStop        chan struct{}
	progressDone        chan struct{}
	pathAdded           bool
	pathRefresh         bool
	pathInitiallyReady  bool
}

type release struct {
	TagName     string `json:"tag_name"`
	PublishedAt string `json:"published_at"`
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
	i, err := newInstaller(os.Stderr)
	if err != nil {
		return err
	}
	err = i.run()
	if i.progressActive {
		i.stopProgress()
	}
	return err
}

func newInstaller(out io.Writer) (*installer, error) {
	home, err := os.UserHomeDir()
	if value := firstEnv("HOME", "USERPROFILE"); value != "" {
		home = value
	}
	if err != nil && home == "" {
		return nil, errors.New("home directory is not available")
	}
	version := envOr("WAGO_VERSION", "main")
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
		version:             version,
		repoURL:             envOr("WAGO_REPO_URL", "https://github.com/wago-org/wago.git"),
		archiveURL:          envOr("WAGO_ARCHIVE_URL", "https://api.github.com/repos/wago-org/wago/zipball/"+version),
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
	tag, base := i.version, ""
	if os.Getenv("WAGO_MANAGER_URL") == "" {
		var err error
		tag, base, err = i.resolveRelease()
		if err != nil {
			return err
		}
	}
	asset := "wago-" + runtime.GOOS + "-" + runtime.GOARCH
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
	i.managerTag, i.managerFromRelease = tag, true
	i.done("Downloaded Wago manager " + tag)
	return nil
}

func (i *installer) resolveRelease() (string, string, error) {
	version := i.version
	switch {
	case version == "latest":
		var item release
		if err := i.getJSON(i.releaseAPI+"/latest", &item); err != nil {
			return "", "", err
		}
		if item.TagName == "" {
			return "", "", errors.New("release response did not contain a tag")
		}
		return item.TagName, i.releaseDownloadBase + "/download/" + item.TagName, nil
	case strings.HasPrefix(version, "v") || strings.HasPrefix(version, "canary-") || strings.HasPrefix(version, "nightly-"):
		return version, i.releaseDownloadBase + "/download/" + version, nil
	}
	prefix := version
	if version == "main" {
		prefix = "canary"
	}
	if prefix != "canary" && prefix != "nightly" {
		return "", "", errors.New("custom source ref requires a source build")
	}
	var releases []release
	if err := i.getJSON(i.releaseAPI+"?per_page=100", &releases); err != nil {
		return "", "", err
	}
	sort.SliceStable(releases, func(a, b int) bool { return releases[a].PublishedAt > releases[b].PublishedAt })
	for _, item := range releases {
		if strings.HasPrefix(item.TagName, prefix+"-") {
			return item.TagName, i.releaseDownloadBase + "/download/" + item.TagName, nil
		}
	}
	return "", "", fmt.Errorf("no %s installer release found", prefix)
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
	fields := strings.Fields(string(wantData))
	if len(fields) == 0 || len(fields[0]) != 64 {
		return errors.New("release checksum is malformed")
	}
	want, err := hex.DecodeString(fields[0])
	if err != nil {
		return errors.New("release checksum is malformed")
	}
	file, err := os.Open(payload)
	if err != nil {
		return err
	}
	hash := sha256.New()
	_, copyErr := io.Copy(hash, file)
	closeErr := file.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if !strings.EqualFold(hex.EncodeToString(hash.Sum(nil)), hex.EncodeToString(want)) {
		return errors.New("release checksum did not match")
	}
	return os.Rename(payload, target)
}

func (i *installer) download(url, target string) error {
	response, err := i.httpClient.Get(url)
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

func (i *installer) fetchSource() (string, error) {
	target := filepath.Join(i.tmpDir, "src")
	sourceVersion := i.version
	archiveURL := i.archiveURL
	if i.managerFromRelease {
		sourceVersion = i.managerTag
		if os.Getenv("WAGO_ARCHIVE_URL") == "" {
			archiveURL = "https://api.github.com/repos/" + envOr("WAGO_RELEASE_REPO", "wago-org/wago") + "/zipball/" + sourceVersion
		}
	}
	i.begin("Fetching Wago source")
	gitLog, gitErr := exec.Command("git", "clone", "--depth", "1", "--branch", sourceVersion, i.repoURL, target).CombinedOutput()
	if gitErr == nil {
		i.sourceMethod = "git"
		i.done("Fetched Wago source")
		return target, nil
	}
	_ = os.RemoveAll(target)
	i.retry("Git fetch failed; trying source archive")
	archive := filepath.Join(i.tmpDir, "source.zip")
	if err := i.download(archiveURL, archive); err != nil {
		return "", fmt.Errorf("fetch source with Git (%v: %s) or archive: %w", gitErr, strings.TrimSpace(string(gitLog)), err)
	}
	if err := sourcearchive.Extract(archive, target); err != nil {
		return "", fmt.Errorf("unpack source archive: %w", err)
	}
	i.sourceMethod = "archive"
	i.done("Fetched Wago source")
	return target, nil
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
	i.begin("Installing Wago")
	if err := os.MkdirAll(i.binDir, 0o755); err != nil {
		return err
	}
	if err := os.Rename(source, target); err != nil {
		return fmt.Errorf("install Wago command: %w", err)
	}
	if err := os.Chmod(target, 0o755); err != nil {
		return err
	}
	i.done("Installed Wago command")
	return nil
}

func (i *installer) saveSource(source string) error {
	i.begin("Saving Wago source")
	if err := os.MkdirAll(filepath.Dir(i.srcDir), 0o755); err != nil {
		return err
	}
	backup := filepath.Join(i.tmpDir, "source-backup")
	if _, err := os.Stat(i.srcDir); err == nil {
		if err := os.Rename(i.srcDir, backup); err != nil {
			return err
		}
	}
	if err := os.Rename(source, i.srcDir); err != nil {
		_ = os.Rename(backup, i.srcDir)
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
