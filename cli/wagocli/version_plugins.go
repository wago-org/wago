package wagocli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/wago-org/wago/internal/wagopaths"
)

// pluginTransfer describes a one-way copy of the active version's global plugin
// declaration into another installed runtime. The destination runtime rebuilds
// the declaration for itself; compiled binaries are never copied between Wago
// versions.
type pluginTransfer struct {
	sourceVersion string
	sourceDir     string
	targetDir     string
	targetRuntime string
	dependencies  []string
}

const versionPluginManifest = "wago.json"

var (
	inspectPluginRunnerRelease = runnerRelease
	buildTransferredRuntime    = runTransferredRuntimeBuild
)

func pluginTransferPlan(d wagopaths.Dirs, targetVersion string, targetProfile wagopaths.Profile, targetBuild wagopaths.Build) (pluginTransfer, bool) {
	sourceVersion := activeVersion(d)
	if sourceVersion == "" || sourceVersion == targetVersion || targetProfile == wagopaths.ProfileMinimal {
		return pluginTransfer{}, false
	}
	sourceRuntime, _, _, _, sourceOK := activeRunner(d)
	targetRuntime, _, _, targetOK := installedRuntime(d, targetVersion, targetProfile, targetBuild)
	if !sourceOK || !targetOK {
		return pluginTransfer{}, false
	}
	sourceRelease := inspectPluginRunnerRelease(sourceRuntime, sourceVersion)
	targetRelease := inspectPluginRunnerRelease(targetRuntime, targetVersion)
	sourceDir, dependencies := versionPluginSet(d, sourceRelease, sourceVersion)
	if len(dependencies) == 0 {
		return pluginTransfer{}, false
	}
	targetDir := filepath.Join(d.Versions, targetRelease, "plugins")
	if filepath.Clean(sourceDir) == filepath.Clean(targetDir) {
		return pluginTransfer{}, false
	}
	if _, configured := versionPluginSet(d, targetRelease, targetVersion); len(configured) != 0 {
		return pluginTransfer{}, false
	}
	return pluginTransfer{
		sourceVersion: sourceVersion,
		sourceDir:     sourceDir,
		targetDir:     targetDir,
		targetRuntime: targetRuntime,
		dependencies:  dependencies,
	}, true
}

func runnerRelease(path, fallback string) string {
	output, err := exec.Command(path, "--version").Output()
	if err != nil {
		return fallback
	}
	return releaseFromVersionOutput(output, fallback)
}

func releaseFromVersionOutput(output []byte, fallback string) string {
	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) >= 2 && strings.EqualFold(fields[0], "release") {
			return fields[1]
		}
	}
	if len(lines) != 0 {
		fields := strings.Fields(lines[0])
		if len(fields) >= 2 && strings.EqualFold(fields[0], "wago") {
			return fields[1]
		}
	}
	return fallback
}

func versionPluginSet(d wagopaths.Dirs, release, alias string) (string, []string) {
	versions := []string{release}
	if alias != "" && alias != release {
		versions = append(versions, alias)
	}
	for _, version := range versions {
		dir := filepath.Join(d.Versions, version, "plugins")
		data, err := os.ReadFile(filepath.Join(dir, versionPluginManifest))
		if err != nil {
			continue
		}
		var manifest struct {
			Dependencies []string `json:"dependencies"`
		}
		if json.Unmarshal(data, &manifest) != nil {
			continue
		}
		dependencies := make([]string, 0, len(manifest.Dependencies))
		for _, dependency := range manifest.Dependencies {
			if dependency = strings.TrimSpace(dependency); dependency != "" {
				dependencies = append(dependencies, dependency)
			}
		}
		if len(dependencies) != 0 {
			sort.Strings(dependencies)
			return dir, dependencies
		}
	}
	return "", nil
}

func pluginTransferPicker(plan pluginTransfer, targetVersion string, targetProfile wagopaths.Profile, targetBuild wagopaths.Build) *picker {
	count := len(plan.dependencies)
	source := "Wago " + transferVersionLabel(plan.sourceVersion)
	return newPicker(fmt.Sprintf("Install plugins for %s?", installedWagoLabel(targetVersion, targetVersion, targetProfile, targetBuild)), []pickerItem{
		{label: "Yes", desc: fmt.Sprintf("Transfer %d plugin%s from %s", count, versionPlural(count), source), value: "yes"},
		{label: "No", value: "no"},
	})
}

func offerPluginTransfer(d wagopaths.Dirs, targetVersion string, targetProfile wagopaths.Profile, targetBuild wagopaths.Build) {
	plan, ok := pluginTransferPlan(d, targetVersion, targetProfile, targetBuild)
	if !ok {
		return
	}
	install := false
	if stdinIsTTY() {
		p := pluginTransferPicker(plan, targetVersion, targetProfile, targetBuild)
		submitted, cancelled := runSelector(p)
		install = submitted && !cancelled && p.selected() == "yes"
	} else {
		count := len(plan.dependencies)
		prompt := fmt.Sprintf("Install %d plugin%s from Wago %s for %s?", count, versionPlural(count), transferVersionLabel(plan.sourceVersion), installedWagoLabel(targetVersion, targetVersion, targetProfile, targetBuild))
		install = promptYesNo(os.Stdin, os.Stdout, prompt)
	}
	if !install {
		return
	}
	progress := newInstallProgress(os.Stderr)
	progress.title("Setting Up Plugins")
	progress.begin(fmt.Sprintf("installing %d plugin%s", len(plan.dependencies), versionPlural(len(plan.dependencies))))
	if err := transferPlugins(plan); err != nil {
		progress.fail("could not install plugins")
		fmt.Fprintf(os.Stderr, "%s %v\n", dim("wago:"), err)
		return
	}
	progress.finish(fmt.Sprintf("Installed %d plugin%s for %s", len(plan.dependencies), versionPlural(len(plan.dependencies)), installedWagoLabel(targetVersion, targetVersion, targetProfile, targetBuild)))
}

func transferPlugins(plan pluginTransfer) error {
	if err := os.MkdirAll(plan.targetDir, 0o755); err != nil {
		return err
	}
	for _, name := range []string{versionPluginManifest, "wago-lock.json"} {
		data, err := os.ReadFile(filepath.Join(plan.sourceDir, name))
		if os.IsNotExist(err) && name != versionPluginManifest {
			continue
		}
		if err != nil {
			return fmt.Errorf("copy %s: %w", name, err)
		}
		if err := os.WriteFile(filepath.Join(plan.targetDir, name), data, 0o644); err != nil {
			return fmt.Errorf("copy %s: %w", name, err)
		}
	}
	return buildTransferredRuntime(plan.targetRuntime, pluginTransferEnv(), len(plan.dependencies))
}

func runTransferredRuntimeBuild(targetRuntime string, env []string, minimumPlugins int) error {
	command := exec.Command(targetRuntime, "plugin", "list", "--json")
	command.Env = env
	var stdout bytes.Buffer
	command.Stdout = &stdout
	var stderr bytes.Buffer
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail != "" {
			return fmt.Errorf("build destination runtime: %w: %s", err, detail)
		}
		return fmt.Errorf("build destination runtime: %w", err)
	}
	return verifyTransferredPluginList(stdout.Bytes(), minimumPlugins)
}

func verifyTransferredPluginList(output []byte, minimumPlugins int) error {
	var plugins []json.RawMessage
	if err := json.Unmarshal(output, &plugins); err != nil {
		return fmt.Errorf("verify destination runtime plugins: %w", err)
	}
	if len(plugins) < minimumPlugins {
		return fmt.Errorf("verify destination runtime plugins: found %d, want at least %d", len(plugins), minimumPlugins)
	}
	return nil
}

func transferVersionLabel(version string) string {
	label := releasePickerLabel(version)
	if label == "latest" || isRollingChannel(label) {
		return strings.ToUpper(label[:1]) + label[1:]
	}
	return label
}

func versionPlural(count int) string {
	if count == 1 {
		return ""
	}
	return "s"
}

func pluginTransferEnv() []string {
	remove := map[string]bool{
		"WAGO_BARE":          true,
		"WAGO_GLOBAL":        true,
		"WAGO_PLUGIN_ACTIVE": true,
	}
	env := make([]string, 0, len(os.Environ())+1)
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if !remove[key] {
			env = append(env, entry)
		}
	}
	env = append(env, "WAGO_GLOBAL=1")
	return env
}
