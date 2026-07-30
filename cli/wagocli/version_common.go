package wagocli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/wago-org/wago/internal/wagopaths"
)

// ---- installed-version state (net-free) ---------------------------------

// installedVersions returns the versions that have an installed binary, sorted
// in numeric semver order.
func installedVersions(d wagopaths.Dirs) []string {
	entries, err := os.ReadDir(d.Versions)
	if err != nil {
		return nil
	}
	var vers []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if _, _, _, ok := installedRuntime(d, e.Name(), "", ""); ok {
			vers = append(vers, e.Name())
		}
	}
	sort.Slice(vers, func(i, j int) bool { return compareSemver(vers[i], vers[j]) < 0 })
	return vers
}

func activeVersion(d wagopaths.Dirs) string {
	b, err := os.ReadFile(d.ConfigFile("active-version"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func activeProfile(d wagopaths.Dirs) wagopaths.Profile {
	b, err := os.ReadFile(d.ConfigFile("active-profile"))
	if err == nil {
		if profile, parseErr := wagopaths.ParseProfile(strings.TrimSpace(string(b))); parseErr == nil {
			return profile
		}
	}
	return wagopaths.ProfileStandard
}

func activeBuild(d wagopaths.Dirs) wagopaths.Build {
	b, err := os.ReadFile(d.ConfigFile("active-build"))
	if err == nil {
		if build, parseErr := wagopaths.ParseBuild(strings.TrimSpace(string(b))); parseErr == nil {
			return build
		}
	}
	return wagopaths.BuildNormal
}

func installedRuntime(d wagopaths.Dirs, ver string, requestedProfile wagopaths.Profile, requestedBuild wagopaths.Build) (string, wagopaths.Profile, wagopaths.Build, bool) {
	if requestedProfile != "" && requestedBuild != "" {
		path := d.RuntimeBinary(ver, string(requestedProfile), string(requestedBuild))
		if fi, err := os.Stat(path); err == nil && !fi.IsDir() {
			return path, requestedProfile, requestedBuild, true
		}
		if requestedBuild == wagopaths.BuildNormal {
			previousRuntime := d.RunnerBinary(ver, string(requestedProfile))
			if fi, err := os.Stat(previousRuntime); err == nil && !fi.IsDir() {
				return previousRuntime, requestedProfile, requestedBuild, true
			}
			legacyRuntime := d.LegacyRunnerBinary(ver, string(requestedProfile))
			if fi, err := os.Stat(legacyRuntime); err == nil && !fi.IsDir() {
				return legacyRuntime, requestedProfile, requestedBuild, true
			}
			if requestedProfile == wagopaths.ProfileStandard {
				legacy := d.VersionBinary(ver)
				if fi, err := os.Stat(legacy); err == nil && !fi.IsDir() {
					return legacy, requestedProfile, requestedBuild, true
				}
			}
		}
		return "", "", "", false
	}
	for _, profile := range wagopaths.Profiles {
		builds := wagopaths.Builds
		if requestedBuild != "" {
			builds = []wagopaths.Build{requestedBuild}
		}
		if requestedProfile != "" && profile != requestedProfile {
			continue
		}
		for _, build := range builds {
			if path, foundProfile, foundBuild, ok := installedRuntime(d, ver, profile, build); ok {
				return path, foundProfile, foundBuild, true
			}
		}
	}
	return "", "", "", false
}

func activeRunner(d wagopaths.Dirs) (path, version string, profile wagopaths.Profile, build wagopaths.Build, ok bool) {
	version = activeVersion(d)
	if version == "" {
		return "", "", "", "", false
	}
	profile = activeProfile(d)
	build = activeBuild(d)
	path, profile, build, ok = installedRuntime(d, version, profile, build)
	return path, version, profile, build, ok
}

func setActiveInstallation(d wagopaths.Dirs, ver string, profile wagopaths.Profile, build wagopaths.Build) error {
	if err := d.Ensure(); err != nil {
		return err
	}
	if err := os.WriteFile(d.ConfigFile("active-version"), []byte(ver+"\n"), 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(d.ConfigFile("active-profile"), []byte(string(profile)+"\n"), 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(d.ConfigFile("active-build"), []byte(string(build)+"\n"), 0o644); err != nil {
		return err
	}
	return nil
}

func setActiveVersion(d wagopaths.Dirs, ver string) error {
	_, profile, build, ok := installedRuntime(d, ver, "", "")
	if !ok {
		profile = wagopaths.ProfileStandard
		build = wagopaths.BuildNormal
	}
	return setActiveInstallation(d, ver, profile, build)
}

// ---- net-free subcommands -----------------------------------------------

func vmList(d wagopaths.Dirs) {
	vers := installedVersions(d)
	if len(vers) == 0 {
		fmt.Println(dim("no versions installed; try: wago version install <ver>"))
		return
	}
	active, profile, build := activeVersion(d), activeProfile(d), activeBuild(d)
	for _, v := range vers {
		marker := "  "
		if v == active {
			marker = cyan("* ")
		}
		profiles := installedProfiles(d, v)
		suffix := ""
		if len(profiles) != 0 {
			suffix = "  " + strings.Join(profiles, ", ")
		}
		if v == active {
			suffix = "  " + string(profile) + "/" + string(build)
		}
		fmt.Printf("%s%s%s\n", marker, v, dim(suffix))
	}
}

func installedProfiles(d wagopaths.Dirs, ver string) []string {
	var profiles []string
	for _, profile := range wagopaths.Profiles {
		for _, build := range wagopaths.Builds {
			if _, _, _, ok := installedRuntime(d, ver, profile, build); ok {
				profiles = append(profiles, string(profile)+"/"+string(build))
			}
		}
	}
	return profiles
}

func vmCurrent(d wagopaths.Dirs) {
	if a := activeVersion(d); a != "" {
		fmt.Printf("%s %s %s\n", a, activeProfile(d), activeBuild(d))
		return
	}
	fmt.Println(dim("no active version set"))
}

func vmWhich(d wagopaths.Dirs) {
	a := activeVersion(d)
	if a == "" {
		fatal("version which: no active version set")
	}
	path, _, _, _, ok := activeRunner(d)
	if !ok {
		fatal("version which: active runtime is not installed")
	}
	fmt.Println(path)
}

func vmUse(d wagopaths.Dirs, ver string, profile wagopaths.Profile, build wagopaths.Build) {
	_, profile, build, ok := installedRuntime(d, ver, profile, build)
	if !ok {
		if profile == "" {
			fatal("version use: %s is not installed (try: wago version install %s)", ver, ver)
		}
		fatal("version use: %s %s/%s is not installed (try: wago version install %s --profile %s --build %s)", ver, profile, build, ver, profile, build)
	}
	if err := setActiveInstallation(d, ver, profile, build); err != nil {
		fatal("version use: %v", err)
	}
	fmt.Printf("%s\n", cyan("Using "+installedWagoLabel(ver, ver, profile, build)))
}

func vmSwitchTo(d wagopaths.Dirs, ver string, profile wagopaths.Profile, build wagopaths.Build) {
	offerPluginTransfer(d, ver, profile, build)
	vmUse(d, ver, profile, build)
}

func offerUseInstalled(d wagopaths.Dirs, ver string, profile wagopaths.Profile, build wagopaths.Build) {
	if activeVersion(d) == ver && activeProfile(d) == profile && activeBuild(d) == build {
		return
	}
	offerUseInstallation(d, ver, profile, build)
}

func offerUseInstallation(d wagopaths.Dirs, ver string, profile wagopaths.Profile, build wagopaths.Build) {
	use := false
	if stdinIsTTY() {
		p := useInstalledPicker(ver, profile, build)
		submitted, cancelled := runSelector(p)
		use = submitted && !cancelled && p.selected() == "yes"
	} else {
		use = promptYesNo(os.Stdin, os.Stdout, fmt.Sprintf("Use %s now?", installedWagoLabel(ver, ver, profile, build)))
	}
	if use {
		vmUse(d, ver, profile, build)
	}
}

func finishVersionInstall(d wagopaths.Dirs, ver string, profile wagopaths.Profile, build wagopaths.Build) {
	offerPluginTransfer(d, ver, profile, build)
	offerUseInstalled(d, ver, profile, build)
}

func useInstalledPicker(ver string, profile wagopaths.Profile, build wagopaths.Build) *picker {
	p := newPicker(fmt.Sprintf("Use %s now?", installedWagoLabel(ver, ver, profile, build)), []pickerItem{
		{label: "Yes", value: "yes"},
		{label: "No", value: "no"},
	})
	return p
}

func updateChannelPicker(active string) *picker {
	items := []pickerItem{
		{label: "Canary", value: "canary"},
		{label: "Nightly", value: "nightly"},
	}
	p := newPicker("Update Wago channel", items)
	channel := active
	if !isRollingChannel(channel) {
		channel = channelRelease(channel)
	}
	if channel == "nightly" {
		p.page().cursor = 1
	}
	return p
}

func chooseUpdateChannel(active string) (string, bool) {
	p := updateChannelPicker(active)
	submitted, cancelled := runSelector(p)
	if cancelled {
		return "", false
	}
	// Non-interactive callers cannot drive the selector; retain the selected
	// default so scripts continue to refresh the active channel.
	if !submitted {
		return p.selected(), p.selected() != ""
	}
	return p.selected(), true
}

func offerUseUpdated(d wagopaths.Dirs, ver string, profile wagopaths.Profile, build wagopaths.Build) {
	offerUseInstallation(d, ver, profile, build)
}

func promptYesNo(in io.Reader, out io.Writer, prompt string) bool {
	fmt.Fprintf(out, "%s [Y/n] ", prompt)
	var answer string
	_, _ = fmt.Fscanln(in, &answer)
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "n", "no":
		return false
	case "", "y", "yes":
		return true
	default:
		return false
	}
}

func vmChooseInstalled(d wagopaths.Dirs) {
	vers := installedVersions(d)
	if len(vers) == 0 {
		fatal("version use: no installed versions")
	}
	p := installedVersionPicker(d, vers)
	submitted, cancelled := runSelector(p)
	if !submitted || cancelled {
		return
	}
	ver, profile, build, ok := parseInstalledSelection(p.selected())
	if !ok {
		return
	}
	if _, _, _, installed := installedRuntime(d, ver, profile, build); !installed {
		vmInstallForSwitch(d, ver, profile, build)
	}
	vmSwitchTo(d, ver, profile, build)
}

func installedVersionPicker(d wagopaths.Dirs, vers []string) *picker {
	active, currentProfile, currentBuild := activeVersion(d), activeProfile(d), activeBuild(d)
	items := make([]pickerItem, 0, len(vers))
	cursor := 0
	profileWidth := 0
	for _, profile := range wagopaths.Profiles {
		for _, build := range wagopaths.Builds {
			if width := len(fmt.Sprintf("(%s/%s)", profile, build)); width > profileWidth {
				profileWidth = width
			}
		}
	}
	for _, ver := range vers {
		selections := installedRuntimeValues(d, ver)
		if len(selections) == 0 {
			continue
		}
		selected := selections[0]
		desc := ""
		if ver == active {
			cursor = len(items)
			desc = "current"
			for _, selection := range selections {
				if selection.profile == currentProfile && selection.build == currentBuild {
					selected = selection
					break
				}
			}
		}
		profileChildren := make([]pickerItem, 0, len(wagopaths.Profiles))
		profileCursor := 0
		for _, profile := range wagopaths.Profiles {
			selectedBuild := wagopaths.BuildNormal
			if ver == active && profile == currentProfile {
				profileCursor = len(profileChildren)
				selectedBuild = currentBuild
			} else {
				for _, build := range wagopaths.Builds {
					if _, _, _, installed := installedRuntime(d, ver, profile, build); installed {
						selectedBuild = build
						break
					}
				}
			}
			buildChildren := make([]pickerItem, 0, len(wagopaths.Builds))
			buildCursor := 0
			for _, build := range wagopaths.Builds {
				buildDesc := build.Description()
				if ver == active && profile == currentProfile && build == currentBuild {
					buildCursor = len(buildChildren)
					buildDesc += " · current"
				} else if _, _, _, installed := installedRuntime(d, ver, profile, build); !installed {
					buildDesc += " · not installed"
				}
				buildChildren = append(buildChildren, pickerItem{
					label: titleBuild(build),
					desc:  buildDesc,
					value: installedSelectionValue(ver, profile, build),
				})
			}
			profileDesc := profile.Description()
			if ver == active && profile == currentProfile {
				profileDesc += " · current"
			} else if _, _, _, installed := installedRuntime(d, ver, profile, selectedBuild); !installed {
				profileDesc += " · not installed"
			}
			profileChildren = append(profileChildren, pickerItem{
				label:       titleProfile(profile),
				meta:        fmt.Sprintf("(%s)", selectedBuild),
				desc:        profileDesc,
				value:       installedSelectionValue(ver, profile, selectedBuild),
				children:    buildChildren,
				childCursor: buildCursor,
			})
		}
		items = append(items, pickerItem{
			label:       releasePickerLabel(ver),
			meta:        fmt.Sprintf("(%s/%s)", selected.profile, selected.build),
			metaWidth:   profileWidth,
			desc:        desc,
			value:       installedSelectionValue(ver, selected.profile, selected.build),
			children:    profileChildren,
			childCursor: profileCursor,
		})
	}
	p := newPicker("Select installed Wago version", items)
	p.page().cursor = cursor
	return p
}

type runtimeValue struct {
	profile wagopaths.Profile
	build   wagopaths.Build
}

func installedRuntimeValues(d wagopaths.Dirs, ver string) []runtimeValue {
	var values []runtimeValue
	for _, profile := range wagopaths.Profiles {
		for _, build := range wagopaths.Builds {
			if _, _, _, ok := installedRuntime(d, ver, profile, build); ok {
				values = append(values, runtimeValue{profile: profile, build: build})
			}
		}
	}
	return values
}

func installedSelectionValue(ver string, profile wagopaths.Profile, build wagopaths.Build) string {
	return ver + "\x00" + string(profile) + "\x00" + string(build)
}

func parseInstalledSelection(value string) (string, wagopaths.Profile, wagopaths.Build, bool) {
	parts := strings.SplitN(value, "\x00", 3)
	if len(parts) != 3 || parts[0] == "" {
		return "", "", "", false
	}
	profile, err := wagopaths.ParseProfile(parts[1])
	if err != nil {
		return "", "", "", false
	}
	build, err := wagopaths.ParseBuild(parts[2])
	return parts[0], profile, build, err == nil
}

func titleBuild(build wagopaths.Build) string {
	value := string(build)
	return strings.ToUpper(value[:1]) + value[1:]
}

func chooseInstallVariant(profileValue, buildValue string) (wagopaths.Profile, wagopaths.Build, bool) {
	profile, profileErr := requestedProfile(profileValue)
	build, buildErr := requestedBuild(buildValue)
	if profileErr != nil || buildErr != nil {
		return "", "", false
	}
	if (profileValue != "" && buildValue != "") || !stdinIsTTY() {
		return profile, build, true
	}
	if profileValue != "" {
		items := make([]pickerItem, 0, len(wagopaths.Builds))
		for _, candidate := range wagopaths.Builds {
			items = append(items, pickerItem{label: titleBuild(candidate), desc: candidate.Description(), value: string(candidate)})
		}
		choice, ok := choosePicker("Choose Wago build", items)
		if !ok {
			return "", "", false
		}
		build, buildErr = wagopaths.ParseBuild(choice)
		return profile, build, buildErr == nil
	}
	items := make([]pickerItem, 0, len(wagopaths.Profiles))
	for _, candidate := range wagopaths.Profiles {
		item := pickerItem{label: titleProfile(candidate), desc: candidate.Description()}
		if buildValue != "" {
			item.value = string(candidate)
		} else {
			item.acceptTitle = "Choose Wago build"
			for _, candidateBuild := range wagopaths.Builds {
				item.acceptItems = append(item.acceptItems, pickerItem{
					label: titleBuild(candidateBuild),
					desc:  candidateBuild.Description(),
					value: string(candidate) + "\x00" + string(candidateBuild),
				})
			}
		}
		items = append(items, item)
	}
	p := newPicker("Choose Wago profile", items)
	submitted, cancelled := runSelector(p)
	if !submitted || cancelled {
		return "", "", false
	}
	if buildValue != "" {
		profile, profileErr = wagopaths.ParseProfile(p.selected())
		return profile, build, profileErr == nil
	}
	parts := strings.SplitN(p.selected(), "\x00", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	profile, profileErr = wagopaths.ParseProfile(parts[0])
	build, buildErr = wagopaths.ParseBuild(parts[1])
	return profile, build, profileErr == nil && buildErr == nil
}

func titleProfile(profile wagopaths.Profile) string {
	value := string(profile)
	return strings.ToUpper(value[:1]) + value[1:]
}

func installedWagoLabel(requested, resolved string, profile wagopaths.Profile, build wagopaths.Build) string {
	channel := ""
	if isRollingChannel(requested) {
		channel = requested
	} else {
		channel = channelRelease(resolved)
		if channel == "" {
			channel = channelRelease(requested)
		}
	}
	if channel != "" {
		qualifier := string(profile) + "/" + string(build)
		if hash := releaseCommit(resolved); hash != "" {
			qualifier = hash + "/" + qualifier
		}
		return fmt.Sprintf("Wago %s (%s)", titleProfile(wagopaths.Profile(channel)), qualifier)
	}
	return fmt.Sprintf("Wago %s (%s/%s)", releasePickerLabel(requested), profile, build)
}

func releaseCommit(release string) string {
	if channelRelease(release) == "" {
		return ""
	}
	parts := strings.Split(release, "-")
	if len(parts) < 3 {
		return ""
	}
	return parts[len(parts)-1]
}

func diagnosticChannel(activeVersion, release string) string {
	switch {
	case activeVersion == "canary", strings.HasPrefix(release, "canary-"):
		return "canary"
	case activeVersion == "nightly", strings.HasPrefix(release, "nightly-"):
		return "nightly"
	case activeVersion == "latest":
		return "latest"
	case strings.HasPrefix(release, "v"):
		return "stable"
	case activeVersion != "":
		return activeVersion
	default:
		return "development"
	}
}

func vmUninstall(d wagopaths.Dirs, ver string) {
	dir := filepath.Join(d.Versions, ver)
	if _, err := os.Stat(dir); err != nil {
		fatal("version uninstall: %s is not installed", ver)
	}
	if err := os.RemoveAll(dir); err != nil {
		fatal("version uninstall: %v", err)
	}
	if activeVersion(d) == ver {
		_ = os.Remove(d.ConfigFile("active-version"))
		_ = os.Remove(d.ConfigFile("active-profile"))
		_ = os.Remove(d.ConfigFile("active-build"))
	}
	fmt.Printf("uninstalled wago %s\n", ver)
}

func uninstallVersionPicker(d wagopaths.Dirs, versions []string) *multiSelect {
	active := activeVersion(d)
	items := make([]selItem, 0, len(versions))
	for _, version := range versions {
		desc := strings.Join(installedProfiles(d, version), ", ")
		if version == active {
			if desc != "" {
				desc += " · "
			}
			desc += "current"
		}
		items = append(items, selItem{label: version, desc: desc})
	}
	return &multiSelect{
		title:  "Uninstall Wago versions",
		prompt: "↑/↓ move · space toggle · a all · enter/→ uninstall · esc cancel",
		items:  items,
	}
}

func vmChooseUninstall(d wagopaths.Dirs) {
	versions := installedVersions(d)
	if len(versions) == 0 {
		fmt.Println(dim("no versions installed"))
		return
	}
	m := uninstallVersionPicker(d, versions)
	submitted, cancelled := runSelector(m)
	if !submitted || cancelled {
		return
	}
	for _, version := range m.chosen() {
		vmUninstall(d, version)
	}
}

// rollingChannels are version names whose build moves under a fixed name:
// "canary" tracks the latest main commit, "nightly" the latest nightly release.
// Installing or updating one always re-fetches, unlike an immutable release.
var rollingChannels = map[string]bool{"canary": true, "nightly": true}

type remoteRelease struct {
	TagName     string `json:"tag_name"`
	PublishedAt string `json:"published_at"`
}

type remoteCommit struct {
	SHA    string `json:"sha"`
	Commit struct {
		Author struct {
			Date string `json:"date"`
		} `json:"author"`
		Committer struct {
			Date string `json:"date"`
		} `json:"committer"`
	} `json:"commit"`
}

// isRollingChannel reports whether ver names a rolling release channel rather
// than a pinned, immutable version.
func isRollingChannel(ver string) bool { return rollingChannels[ver] }

// channelRelease returns the moving channel represented by an immutable
// prerelease tag, if any. Release APIs return newest-first, so callers keep the
// first tag seen for each channel.
func channelRelease(tag string) string {
	for channel := range rollingChannels {
		if strings.HasPrefix(tag, channel+"-") {
			return channel
		}
	}
	return ""
}

func channelReleaseNames(tags []string, channel string) []string {
	names := make([]string, 0, len(tags))
	seen := make(map[string]bool, len(tags))
	for _, tag := range tags {
		if channelRelease(tag) == channel && !seen[tag] {
			names = append(names, tag)
			seen[tag] = true
		}
	}
	return names
}

func stableReleaseNames(tags []string) []string {
	names := make([]string, 0, len(tags))
	seen := make(map[string]bool, len(tags))
	for _, tag := range tags {
		if tag == "" || isRollingChannel(tag) || channelRelease(tag) != "" {
			continue
		}
		name := tag
		if !strings.HasPrefix(name, "v") {
			name = "v" + name
		}
		if !seen[name] {
			names = append(names, name)
			seen[name] = true
		}
	}
	sort.Slice(names, func(i, j int) bool { return compareSemver(names[i], names[j]) > 0 })
	return names
}

func releasePickerItems(releases []remoteRelease, channel string, now time.Time) []pickerItem {
	items := make([]pickerItem, 0, len(releases))
	for _, release := range releases {
		if channel == "" {
			if release.TagName == "" || isRollingChannel(release.TagName) || channelRelease(release.TagName) != "" {
				continue
			}
		} else if channelRelease(release.TagName) != channel {
			continue
		}
		label := releasePickerLabel(release.TagName)
		items = append(items, pickerItem{
			label: label,
			desc:  releasePickerDescription(release, now),
			value: release.TagName,
		})
	}
	if channel == "" {
		sort.Slice(items, func(i, j int) bool { return compareSemver(items[i].label, items[j].label) > 0 })
	}
	return items
}

func canaryCommitItems(commits []remoteCommit, now time.Time) []pickerItem {
	items := make([]pickerItem, 0, len(commits))
	seen := make(map[string]bool, len(commits))
	for _, commit := range commits {
		sha := strings.ToLower(strings.TrimSpace(commit.SHA))
		if !validCommitSHA(sha) || seen[sha] {
			continue
		}
		seen[sha] = true
		published := commit.Commit.Author.Date
		if published == "" {
			published = commit.Commit.Committer.Date
		}
		items = append(items, pickerItem{
			label: "canary-" + sha[:7],
			desc:  releasePickerDescription(remoteRelease{PublishedAt: published}, now),
			value: canaryCommitTarget(sha),
		})
	}
	return items
}

func canaryCommitTarget(sha string) string {
	return "canary@" + strings.ToLower(strings.TrimSpace(sha))
}

func canaryCommitSHA(target string) (string, bool) {
	sha, found := strings.CutPrefix(target, "canary@")
	return sha, found && validCommitSHA(sha)
}

func validCommitSHA(sha string) bool {
	if len(sha) != 40 {
		return false
	}
	for _, char := range sha {
		if !strings.ContainsRune("0123456789abcdef", char) {
			return false
		}
	}
	return true
}

func canaryCommitVersion(target string) string {
	if sha, ok := canaryCommitSHA(target); ok {
		return "canary-" + sha[:7]
	}
	return target
}

func releasePickerLabel(tag string) string {
	if isRollingChannel(tag) || tag == "latest" {
		return tag
	}
	if channelRelease(tag) != "" {
		parts := strings.Split(tag, "-")
		if len(parts) >= 3 && len(parts[1]) == 8 {
			if _, ok := atoiOK(parts[1]); ok {
				return parts[0] + "-" + strings.Join(parts[2:], "-")
			}
		}
		return tag
	}
	if strings.HasPrefix(tag, "v") {
		return tag
	}
	return "v" + tag
}

func releasePickerDescription(release remoteRelease, now time.Time) string {
	published, err := time.Parse(time.RFC3339, release.PublishedAt)
	if err != nil {
		return ""
	}
	return published.UTC().Format("01/02/2006") + "  " + relativeReleaseAge(published, now)
}

func relativeReleaseAge(published, now time.Time) string {
	elapsed := now.Sub(published)
	future := elapsed < 0
	if future {
		elapsed = -elapsed
	}
	value, unit := int(elapsed.Minutes()), "m"
	switch {
	case elapsed < time.Hour:
	case elapsed < 24*time.Hour:
		value, unit = int(elapsed.Hours()), "h"
	case elapsed < 365*24*time.Hour:
		value, unit = int(elapsed.Hours()/24), "d"
	default:
		value, unit = int(elapsed.Hours()/(365*24)), "y"
	}
	if future {
		return fmt.Sprintf("in %d%s", value, unit)
	}
	return fmt.Sprintf("%d%s ago", value, unit)
}

func releasePickerChildren(channel string, versions []pickerItem) []pickerItem {
	if len(versions) == 0 {
		return nil
	}
	items := []pickerItem{{label: "latest", value: channel}}
	return append(items, versions...)
}

func versionPickerItemsWithCommits(releases []remoteRelease, commits []remoteCommit, now time.Time) []pickerItem {
	canary := canaryCommitItems(commits, now)
	nightly := releasePickerItems(releases, "nightly", now)
	stable := releasePickerItems(releases, "", now)
	items := []pickerItem{
		{label: "canary", value: "canary", children: releasePickerChildren("canary", canary)},
		{label: "nightly", value: "nightly", children: releasePickerChildren("nightly", nightly)},
		{label: "latest", value: "latest", children: releasePickerChildren("latest", stable)},
	}
	return append(items, stable...)
}

func installPickerItemsWithCommits(releases []remoteRelease, commits []remoteCommit, now time.Time) []pickerItem {
	return addProfileChoices(versionPickerItemsWithCommits(releases, commits, now))
}

func addProfileChoices(items []pickerItem) []pickerItem {
	for i := range items {
		items[i].children = addProfileChoices(items[i].children)
		if items[i].value == "" {
			continue
		}
		items[i].acceptTitle = "Choose Wago profile"
		items[i].acceptItems = make([]pickerItem, 0, len(wagopaths.Profiles))
		for _, profile := range wagopaths.Profiles {
			builds := make([]pickerItem, 0, len(wagopaths.Builds))
			for _, build := range wagopaths.Builds {
				builds = append(builds, pickerItem{
					label: titleBuild(build),
					desc:  build.Description(),
					value: installedSelectionValue(items[i].value, profile, build),
				})
			}
			items[i].acceptItems = append(items[i].acceptItems, pickerItem{
				label:       titleProfile(profile),
				desc:        profile.Description(),
				acceptTitle: "Choose Wago build",
				acceptItems: builds,
			})
		}
	}
	return items
}

func chooseInstallPicker(releases []remoteRelease, commits []remoteCommit, now time.Time, profileValue, buildValue string) (string, wagopaths.Profile, wagopaths.Build, bool) {
	if profileValue != "" || buildValue != "" {
		choice, ok := choosePicker("Install Wago version", versionPickerItemsWithCommits(releases, commits, now))
		if !ok {
			return "", "", "", false
		}
		profile, build, ok := chooseInstallVariant(profileValue, buildValue)
		return choice, profile, build, ok
	}
	p := newPicker("Install Wago version", installPickerItemsWithCommits(releases, commits, now))
	submitted, cancelled := runSelector(p)
	if !submitted || cancelled {
		return "", "", "", false
	}
	return parseInstalledSelection(p.selected())
}

// updateVersionTarget chooses the version refreshed by `wago version update`.
// No selector means the active version; explicit channel flags make refreshing a
// rolling channel convenient without first selecting it.
func updateVersionTarget(active string, args []string, nightly, canary bool) (string, error) {
	if nightly && canary {
		return "", fmt.Errorf("--nightly and --canary cannot be used together")
	}
	if len(args) > 1 {
		return "", fmt.Errorf("accepts at most one [version]")
	}
	if (nightly || canary) && len(args) != 0 {
		return "", fmt.Errorf("a release-channel flag cannot be used with [version]")
	}
	if nightly {
		return "nightly", nil
	}
	if canary {
		return "canary", nil
	}
	if len(args) == 1 {
		if !isRollingChannel(args[0]) {
			return "", fmt.Errorf("%s is pinned; update only refreshes canary or nightly", args[0])
		}
		return args[0], nil
	}
	if active == "" {
		return "", fmt.Errorf("no active version; use `wago version update <version>` or select --nightly/--canary")
	}
	if !isRollingChannel(active) {
		return "", fmt.Errorf("active version %s is pinned; switch to canary or nightly to update", active)
	}
	return active, nil
}

// ---- semver ordering ----------------------------------------------------

// compareSemver does a numeric dotted compare of two version strings, ignoring a
// leading 'v'. Non-numeric components sort after numeric ones.
func compareSemver(a, b string) int {
	as := strings.Split(strings.TrimPrefix(a, "v"), ".")
	bs := strings.Split(strings.TrimPrefix(b, "v"), ".")
	for i := 0; i < len(as) || i < len(bs); i++ {
		var av, bv int
		var ao, bo bool
		if i < len(as) {
			av, ao = atoiOK(as[i])
		}
		if i < len(bs) {
			bv, bo = atoiOK(bs[i])
		}
		if ao && bo {
			if av != bv {
				return sign(av - bv)
			}
			continue
		}
		if c := strings.Compare(get(as, i), get(bs, i)); c != 0 {
			return c
		}
	}
	return 0
}

func atoiOK(s string) (int, bool) {
	if s == "" {
		return 0, false
	}
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, false
		}
		n = n*10 + int(c-'0')
	}
	return n, true
}

func get(s []string, i int) string {
	if i < len(s) {
		return s[i]
	}
	return ""
}

func sign(n int) int {
	switch {
	case n < 0:
		return -1
	case n > 0:
		return 1
	default:
		return 0
	}
}
