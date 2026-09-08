package version

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/wago-org/wago/cli/internal/tui"
	"github.com/wago-org/wago/internal/wagopaths"
)

func offerUseInstallation(d wagopaths.Dirs, ver string, profile wagopaths.Profile, build wagopaths.Build, mode string) {
	if mode == "yes" {
		vmUse(d, ver, profile, build)
		return
	}
	if mode == "no" {
		return
	}
	use := false
	if tui.StdinIsTTY() {
		p := useInstalledPicker(ver, profile, build)
		submitted, cancelled := tui.Run(p)
		use = submitted && !cancelled && p.Selected() == "yes"
	} else {
		use = promptYesNo(os.Stdin, os.Stdout, fmt.Sprintf("Use %s now?", installedWagoLabel(ver, ver, profile, build)))
	}
	if use {
		vmUse(d, ver, profile, build)
	}
}

func finishVersionInstall(d wagopaths.Dirs, ver string, profile wagopaths.Profile, build wagopaths.Build, mode string) {
	active, currentProfile, currentBuild := activeTuple(d)
	if active == ver && currentProfile == profile && currentBuild == build {
		return
	}
	offerUseInstallation(d, ver, profile, build, mode)
}

func useInstalledPicker(ver string, profile wagopaths.Profile, build wagopaths.Build) *tui.Picker {
	p := tui.NewPicker(fmt.Sprintf("Use %s now?", installedWagoLabel(ver, ver, profile, build)), []tui.Item{
		{Label: "Yes", Value: "yes"},
		{Label: "No", Value: "no"},
	})
	return p
}

func updateChannelPicker(active string) *tui.Picker {
	items := []tui.Item{
		{Label: "Canary", Value: "canary"},
		{Label: "Nightly", Value: "nightly"},
	}
	p := tui.NewPicker("Update Wago channel", items)
	channel := active
	if !isRollingChannel(channel) {
		channel = channelRelease(channel)
	}
	if channel == "nightly" {
		p.SetCursor(1)
	}
	return p
}

func chooseUpdateChannel(active string) (string, bool) {
	p := updateChannelPicker(active)
	submitted, cancelled := tui.Run(p)
	if cancelled {
		return "", false
	}
	// Non-interactive callers cannot drive the selector; retain the selected
	// default so scripts continue to refresh the active channel.
	if !submitted {
		return p.Selected(), p.Selected() != ""
	}
	return p.Selected(), true
}

func offerUseUpdated(d wagopaths.Dirs, ver string, profile wagopaths.Profile, build wagopaths.Build, mode string) {
	offerUseInstallation(d, ver, profile, build, mode)
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
	submitted, cancelled := tui.Run(p)
	if !submitted || cancelled {
		return
	}
	ver, profile, build, ok := parseInstalledSelection(p.Selected())
	if !ok {
		return
	}
	if _, _, _, installed := installedRuntime(d, ver, profile, build); !installed {
		vmInstallForSwitch(d, ver, profile, build)
	}
	vmSwitchTo(d, ver, profile, build)
}

func installedVersionPicker(d wagopaths.Dirs, vers []string) *tui.Picker {
	active, currentProfile, currentBuild := activeTuple(d)
	items := make([]tui.Item, 0, len(vers))
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
		profileChildren := make([]tui.Item, 0, len(wagopaths.Profiles))
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
			buildChildren := make([]tui.Item, 0, len(wagopaths.Builds))
			buildCursor := 0
			for _, build := range wagopaths.Builds {
				buildDesc := build.Description()
				if ver == active && profile == currentProfile && build == currentBuild {
					buildCursor = len(buildChildren)
					buildDesc += " · current"
				} else if _, _, _, installed := installedRuntime(d, ver, profile, build); !installed {
					buildDesc += " · not installed"
				}
				buildChildren = append(buildChildren, tui.Item{
					Label:       titleBuild(build),
					Description: buildDesc,
					Value:       installedSelectionValue(ver, profile, build),
				})
			}
			profileDesc := profile.Description()
			if ver == active && profile == currentProfile {
				profileDesc += " · current"
			} else if _, _, _, installed := installedRuntime(d, ver, profile, selectedBuild); !installed {
				profileDesc += " · not installed"
			}
			profileChildren = append(profileChildren, tui.Item{
				Label:       titleProfile(profile),
				Meta:        fmt.Sprintf("(%s)", selectedBuild),
				Description: profileDesc,
				Value:       installedSelectionValue(ver, profile, selectedBuild),
				Children:    buildChildren,
				ChildCursor: buildCursor,
			})
		}
		items = append(items, tui.Item{
			Label:       releasePickerLabel(ver),
			Meta:        fmt.Sprintf("(%s/%s)", selected.profile, selected.build),
			MetaWidth:   profileWidth,
			Description: desc,
			Value:       installedSelectionValue(ver, selected.profile, selected.build),
			Children:    profileChildren,
			ChildCursor: profileCursor,
		})
	}
	p := tui.NewPicker("Select installed Wago version", items)
	p.SetCursor(cursor)
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
	if (profileValue != "" && buildValue != "") || !tui.StdinIsTTY() {
		return profile, build, true
	}
	if profileValue != "" {
		items := make([]tui.Item, 0, len(wagopaths.Builds))
		for _, candidate := range wagopaths.Builds {
			items = append(items, tui.Item{Label: titleBuild(candidate), Description: candidate.Description(), Value: string(candidate)})
		}
		choice, ok := tui.Choose("Choose Wago build", items)
		if !ok {
			return "", "", false
		}
		build, buildErr = wagopaths.ParseBuild(choice)
		return profile, build, buildErr == nil
	}
	items := make([]tui.Item, 0, len(wagopaths.Profiles))
	for _, candidate := range wagopaths.Profiles {
		item := tui.Item{Label: titleProfile(candidate), Description: candidate.Description()}
		if buildValue != "" {
			item.Value = string(candidate)
		} else {
			item.AcceptTitle = "Choose Wago build"
			for _, candidateBuild := range wagopaths.Builds {
				item.AcceptItems = append(item.AcceptItems, tui.Item{
					Label:       titleBuild(candidateBuild),
					Description: candidateBuild.Description(),
					Value:       string(candidate) + "\x00" + string(candidateBuild),
				})
			}
		}
		items = append(items, item)
	}
	p := tui.NewPicker("Choose Wago profile", items)
	submitted, cancelled := tui.Run(p)
	if !submitted || cancelled {
		return "", "", false
	}
	if buildValue != "" {
		profile, profileErr = wagopaths.ParseProfile(p.Selected())
		return profile, build, profileErr == nil
	}
	parts := strings.SplitN(p.Selected(), "\x00", 2)
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
		channel, _, _ = rollingCommitSHA(resolved)
		if channel == "" {
			channel = channelRelease(resolved)
		}
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
	if _, sha, canonical := rollingCommitSHA(release); canonical {
		return sha[:7]
	}
	if channelRelease(release) == "" {
		return ""
	}
	parts := strings.Split(release, "-")
	if len(parts) < 3 {
		return ""
	}
	return parts[len(parts)-1]
}

func vmUninstall(d wagopaths.Dirs, ver string) {
	if err := removeInstalledVersion(d, ver); err != nil {
		fatal("version uninstall: %v", err)
	}
	fmt.Printf("uninstalled wago %s\n", ver)
}

func removeInstalledVersion(d wagopaths.Dirs, ver string) error {
	lock, err := versionMutationLock(context.Background(), d, ver)
	if err != nil {
		return err
	}
	defer lock.Close()
	dir, err := versionDirectory(d, ver)
	if err != nil {
		return err
	}
	if _, err := os.Stat(dir); err != nil {
		return fmt.Errorf("%s is not installed", ver)
	}
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	return clearActiveInstallation(d, ver)
}

func uninstallVersionPicker(d wagopaths.Dirs, versions []string) *tui.MultiSelect {
	active := activeVersion(d)
	items := make([]tui.SelectItem, 0, len(versions))
	for _, version := range versions {
		desc := strings.Join(installedProfiles(d, version), ", ")
		if version == active {
			if desc != "" {
				desc += " · "
			}
			desc += "current"
		}
		items = append(items, tui.SelectItem{Label: version, Description: desc})
	}
	return &tui.MultiSelect{
		Title:  "Uninstall Wago versions",
		Prompt: "↑/↓ move · space toggle · a toggle all · enter/→ uninstall · esc cancel",
		Items:  items,
	}
}

func vmChooseUninstall(d wagopaths.Dirs) {
	versions := installedVersions(d)
	if len(versions) == 0 {
		fmt.Println(dim("no versions installed"))
		return
	}
	m := uninstallVersionPicker(d, versions)
	submitted, cancelled := tui.Run(m)
	if !submitted || cancelled {
		return
	}
	for _, version := range m.Chosen() {
		vmUninstall(d, version)
	}
}

// rollingChannels are version names whose build moves under a fixed name:
// "canary" tracks the latest main commit, "nightly" the latest nightly release.
// Installing or updating one always re-fetches, unlike an immutable release.
var rollingChannels = map[string]bool{"canary": true, "nightly": true}
