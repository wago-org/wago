package main

import (
	"archive/zip"
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
	i, err := newInstaller(os.Stderr)
	if err != nil {
		return err
	}
	err = i.run()
	if i.progressActive {
		fmt.Fprint(i.out, "\r\x1b[2K")
		i.progressActive = false
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
	return &installer{
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
	}, nil
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
	stamp := i.managerTag
	if stamp == "" {
		stamp = i.version
	}
	fmt.Fprintln(i.out)
	i.done("Wago " + stamp + " is ready")
	i.detail("Command", displayPath(installed, i.home))
	i.next(pathReady, configFile)
	return nil
}

func (i *installer) welcome() {
	s := colors()
	fmt.Fprintf(i.out, "%s%sWago%s\n", s.bold, s.cyan, s.reset)
	fmt.Fprintf(i.out, "%sA fast, extensible WebAssembly JIT for Go.%s\n\n", s.dim, s.reset)
}

func (i *installer) installLocation() {
	s := colors()
	fmt.Fprintf(i.out, "%sInstall location%s\n", s.bold, s.reset)
	fmt.Fprintf(i.out, "  %s%s%s\n\n", s.cyan, displayPath(i.binDir, i.home), s.reset)
}

func (i *installer) plan() {
	s := colors()
	fmt.Fprintf(i.out, "%sPlan%s\n", s.bold, s.reset)
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
		s := colors()
		fmt.Fprintf(i.out, "\r\x1b[2K%s◇%s %s", s.dim, s.reset, label)
		i.progressActive = true
		return
	}
	fmt.Fprintf(i.out, "  … %s\n", label)
}
func (i *installer) done(label string) {
	s := colors()
	if i.progressActive {
		fmt.Fprint(i.out, "\r\x1b[2K")
		i.progressActive = false
	}
	fmt.Fprintf(i.out, "%s✓%s %s\n", s.cyan, s.reset, label)
}
func (i *installer) retry(label string) {
	s := colors()
	if i.progressActive {
		fmt.Fprint(i.out, "\r\x1b[2K")
		i.progressActive = false
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
		return "minimal", true, nil
	}
	if value := os.Getenv("WAGO_REINSTALL_MODE"); value != "" {
		if value == "full" || value == "partial" || value == "minimal" {
			return value, true, nil
		}
		return "", false, errors.New("WAGO_REINSTALL_MODE must be full, partial, or minimal")
	}
	if i.noTUI {
		return "minimal", true, nil
	}
	fmt.Fprintf(i.out, "\nWago is already installed at %s.\n", displayPath(installed, i.home))
	value, ok := radio("How should it be reinstalled?", []radioItem{
		{"Full", "Reset everything, including plugins and settings", "full", ""},
		{"Partial", "Reset Wago but keep global plugins for reinstall", "partial", ""},
		{"Minimal", "Replace binaries and keep existing state", "minimal", ""},
	}, 2)
	if ok {
		fmt.Fprintf(i.out, "Reinstall mode: %s\n\n", reinstallLabel(value))
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
		i.done("Fetched Wago source with Git")
		return target, nil
	}
	_ = os.RemoveAll(target)
	i.retry("Git fetch failed; trying source archive")
	archive := filepath.Join(i.tmpDir, "source.zip")
	if err := i.download(archiveURL, archive); err != nil {
		return "", fmt.Errorf("fetch source with Git (%v: %s) or archive: %w", gitErr, strings.TrimSpace(string(gitLog)), err)
	}
	if err := unzipSingleRoot(archive, target); err != nil {
		return "", fmt.Errorf("unpack source archive: %w", err)
	}
	i.sourceMethod = "archive"
	i.done("Fetched Wago source archive")
	return target, nil
}

func unzipSingleRoot(archive, target string) error {
	reader, err := zip.OpenReader(archive)
	if err != nil {
		return err
	}
	defer reader.Close()
	root := ""
	for _, entry := range reader.File {
		name := filepath.ToSlash(entry.Name)
		part := strings.SplitN(name, "/", 2)[0]
		if part == "" || part == "." || part == ".." {
			return errors.New("archive contains an invalid root")
		}
		if root == "" {
			root = part
		} else if root != part {
			return errors.New("archive contains multiple roots")
		}
	}
	if root == "" {
		return errors.New("archive is empty")
	}
	const maxExpandedSize = 512 << 20
	var expanded uint64
	for _, entry := range reader.File {
		name := filepath.ToSlash(entry.Name)
		rel := strings.TrimPrefix(name, root+"/")
		if rel == "" {
			continue
		}
		destination := filepath.Join(target, filepath.FromSlash(rel))
		if !strings.HasPrefix(filepath.Clean(destination), filepath.Clean(target)+string(os.PathSeparator)) {
			return errors.New("archive path escapes destination")
		}
		if entry.FileInfo().IsDir() {
			if err := os.MkdirAll(destination, 0o755); err != nil {
				return err
			}
			continue
		}
		expanded += entry.UncompressedSize64
		if expanded > maxExpandedSize {
			return errors.New("source archive expands beyond 512 MiB limit")
		}
		if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
			return err
		}
		source, err := entry.Open()
		if err != nil {
			return err
		}
		file, err := os.OpenFile(destination, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, entry.Mode().Perm())
		if err != nil {
			source.Close()
			return err
		}
		_, copyErr := io.Copy(file, source)
		closeErr := file.Close()
		sourceErr := source.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
		if sourceErr != nil {
			return sourceErr
		}
	}
	if _, err := os.Stat(filepath.Join(target, "go.mod")); err != nil {
		return errors.New("source archive does not contain go.mod")
	}
	return nil
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
	i.done("Cleaned existing Wago installation (" + mode + ")")
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
	if choice == "" {
		items := make([]radioItem, 0, len(targets)+1)
		for index, target := range targets {
			status := ""
			if target.current {
				status = "current"
			}
			items = append(items, radioItem{target.label, target.description, strconv.Itoa(index), status})
		}
		items = append(items, radioItem{"Not now", "", "none", ""})
		value, selected := radio(pathSetupQuestion(), items, 0)
		if !selected {
			return pathContains(i.binDir), ""
		}
		choice = value
	}
	if strings.EqualFold(choice, "no") || strings.EqualFold(choice, "n") || choice == "none" {
		fmt.Fprintln(i.out, "PATH setup: skipped")
		fmt.Fprintln(i.out)
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
	if message := pathSetupTargetMessage(target, i.home); message != "" {
		fmt.Fprintln(i.out, message)
		fmt.Fprintln(i.out)
	}
	already, err := addPath(i.binDir, target.configFile, target.shell)
	if err != nil {
		i.retry("Could not add Wago to PATH")
		return false, ""
	}
	if already {
		i.done("Wago is already on PATH")
	} else {
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
	value, ok := radio("Enable Wago completions for "+shellName+"?", []radioItem{
		{"Yes", "Enable command completion", "yes", ""},
		{"No", "", "no", ""},
	}, 0)
	if !ok {
		return
	}
	if value != "yes" {
		fmt.Fprintln(i.out, "Completions: skipped")
		fmt.Fprintln(i.out)
		return
	}
	if err := exec.Command(installed, "config", "completions", shellName, "--install", "--rc", configFile).Run(); err != nil {
		i.retry("Could not enable Wago completions")
		return
	}
	i.done("Enabled completions for " + shellName)
}

func (i *installer) next(pathReady bool, configFile string) {
	s := colors()
	pathReady = pathReady || pathContains(i.binDir)
	fmt.Fprintf(i.out, "\n%sNext%s\n", s.bold, s.reset)
	if pathReady && configFile != "" && !pathContains(i.binDir) {
		fmt.Fprintln(i.out, "  Open a new shell, or run:")
		fmt.Fprintf(i.out, "    %s%s%s\n", s.cyan, sourceCommand(configFile)+" && wago version install", s.reset)
		return
	}
	if pathReady {
		fmt.Fprintf(i.out, "  %swago version install%s\n", s.cyan, s.reset)
		return
	}
	fmt.Fprintf(i.out, "  Add %s to PATH, then run:\n", displayPath(i.binDir, i.home))
	fmt.Fprintf(i.out, "    %swago version install%s\n", s.cyan, s.reset)
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
