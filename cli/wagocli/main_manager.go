//go:build wago_manager

package wagocli

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/wago-org/wago/internal/wagopaths"
)

var version string

func versionString() string {
	if version == "" {
		return "0.0.0"
	}
	return version
}

// Main runs the standard-Go manager. Runtime commands are forwarded to
// the active host runner; the manager itself owns version selection and network
// installation so every profile retains the same management commands.
func Main(v string) {
	version = v
	args := os.Args[1:]
	if len(args) == 0 {
		managerUsage(os.Stderr)
		os.Exit(2)
	}
	switch args[0] {
	case "help", "-h", "--help":
		managerUsage(os.Stdout)
		return
	case "-v", "--version":
		printManagerVersion()
		return
	case "version":
		versionCommand().Dispatch("wago version", args[1:])
		return
	case "auth":
		authCommand().Dispatch("wago auth", args[1:])
		return
	}
	runActiveRunner(args)
}

func printManagerVersion() {
	d := wagopaths.DirsFor(versionString())
	if path, active, profile, build, ok := activeRunner(d); ok {
		command := exec.Command(path, "--version")
		command.Env = append(os.Environ(),
			"WAGO_MANAGER_VERSION="+versionString(),
			"WAGO_MANAGER_EXECUTABLE="+executablePath(),
		)
		output, err := command.Output()
		if err == nil && strings.HasPrefix(string(output), "Wago\n") {
			_, _ = os.Stdout.Write(output)
			return
		}
		printLegacyManagerVersion(active, profile, build, path, output)
		return
	}
	fmt.Printf("%s\n", bold("Wago"))
	printVersionDetail("channel", "none")
	printVersionDetail("manager", versionString())
	printVersionDetail("location", executablePath())
	printVersionDetail("platform", runtime.GOOS+"/"+runtime.GOARCH)
	printVersionDetail("toolchain", runtime.Compiler+" "+runtime.Version())
	printVersionDetail("runtime", "none selected")
}

func printLegacyManagerVersion(active string, profile wagopaths.Profile, build wagopaths.Build, path string, output []byte) {
	release, platform, features := legacyRunnerVersionFields(string(output))
	if release == "" {
		release = active
	}
	if platform == "" {
		platform = runtime.GOOS + "/" + runtime.GOARCH
	}
	fmt.Printf("%s\n", bold("Wago"))
	printVersionDetail("channel", diagnosticChannel(active, release))
	printVersionDetail("release", release)
	printVersionDetail("profile", string(profile))
	printVersionDetail("build", string(build))
	printVersionDetail("platform", platform)
	printVersionDetail("toolchain", runtime.Compiler+" "+runtime.Version())
	printVersionDetail("manager", versionString()+"  "+executablePath())
	printVersionDetail("runtime", path)
	printVersionDetail("plugins", "unavailable (update runtime for details)")
	if features != "" {
		printVersionDetail("features", features)
	}
}

func legacyRunnerVersionFields(output string) (release, platform, features string) {
	for index, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if index == 0 {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				release = fields[1]
			}
			if open, close := strings.LastIndex(line, "("), strings.LastIndex(line, ")"); open >= 0 && close > open {
				platform = line[open+1 : close]
			}
		}
		if strings.HasPrefix(line, "features:") {
			features = strings.TrimSpace(strings.TrimPrefix(line, "features:"))
		}
	}
	return release, platform, features
}

func executablePath() string {
	path, err := os.Executable()
	if err != nil {
		return os.Args[0]
	}
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved
	}
	return path
}

func runActiveRunner(args []string) {
	d := wagopaths.DirsFor(versionString())
	path, active, profile, build, ok := activeRunner(d)
	if !ok {
		fatal("no active runtime; install one with `wago version install`")
	}
	cmd := exec.Command(path, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	cmd.Env = os.Environ()
	err := cmd.Run()
	if err == nil {
		return
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		os.Exit(exitErr.ExitCode())
	}
	fatal("could not launch %s %s/%s runtime: %v", active, profile, build, err)
}

func managerUsage(w *os.File) {
	fmt.Fprintf(w, "%s manages Wago versions and launches the selected runtime. (manager v%s)\n\n", bold("wago"), versionString())
	fmt.Fprintf(w, "%s wago [run] [...flags] <file> [...args]\n\n", bold("Usage:"))
	fmt.Fprintf(w, "%s\n", bold("Manager commands:"))
	fmt.Fprintf(w, "  %-12s %s\n", "version", "list, install, switch, update, and remove runtimes")
	fmt.Fprintf(w, "  %-12s %s\n", "auth", "log in to the plugin registry and manage credentials")
	fmt.Fprintf(w, "\n%s\n", bold("Runtime profiles:"))
	for _, profile := range wagopaths.Profiles {
		fmt.Fprintf(w, "  %-12s %s\n", titleProfile(profile), profile.Description())
	}
	fmt.Fprintf(w, "\n%s\n", bold("Runtime builds:"))
	for _, build := range wagopaths.Builds {
		fmt.Fprintf(w, "  %-12s %s\n", titleBuild(build), build.Description())
	}
	fmt.Fprintf(w, "\nAll other commands are handled by the selected runtime.\n")
}
