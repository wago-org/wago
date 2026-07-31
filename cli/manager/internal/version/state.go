package version

import (
	"fmt"
	"os"
	"sort"
	"strings"

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
	sort.Slice(vers, func(i, j int) bool { return Compare(vers[i], vers[j]) < 0 })
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
	vmUse(d, ver, profile, build)
}
