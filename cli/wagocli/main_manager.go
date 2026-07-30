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
		if hasActiveRunner() {
			runActiveRunner(args)
			return
		}
		managerUsage(os.Stderr)
		os.Exit(2)
	}
	switch args[0] {
	case "help", "-h", "--help":
		if hasActiveRunner() {
			runActiveRunner(args)
			return
		}
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
	case "init":
		initCommand().Dispatch("wago init", args[1:])
		return
	}
	runActiveRunner(args)
}

func hasActiveRunner() bool {
	_, _, _, _, ok := activeRunner(wagopaths.DirsFor(versionString()))
	return ok
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
	cmd.Env = append(os.Environ(),
		"WAGO_MANAGER_VERSION="+versionString(),
		"WAGO_MANAGER_EXECUTABLE="+executablePath(),
		"WAGO_RUNTIME_PROFILE="+string(profile),
		"WAGO_RUNTIME_BUILD="+string(build),
	)
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
	fmt.Fprintf(w, "%s is a pure-Go (no cgo) WebAssembly engine. (v%s)\n\n", bold("wago"), versionString())
	fmt.Fprintf(w, "%s wago <command> [flags]\n", bold("Usage:"))
	fmt.Fprintf(w, "       wago [run] [flags] <file> [args...]\n\n")
	fmt.Fprintf(w, "%s\n", bold("Commands:"))
	commands := []struct {
		name, args, summary string
	}{
		{"run", "<file> [args...]", "compile and execute a WebAssembly module (default)"},
		{"init", "", "initialize a local Wago project"},
		{"add", "<module>[@version]", "add and enable a plugin, then rebuild Wago"},
		{"rm", "<name>", "remove and disable a plugin"},
		{"plugin", "<command>", "add, remove, inspect, update, and publish plugins"},
		{"auth", "<command>", "authenticate to the registry (plugins.wago.sh)"},
		{"module", "<command>", "inspect a module's imports and required capabilities"},
		{"build", "<file>", "precompile a WebAssembly module to a .wago artifact"},
		{"validate", "<file>", "decode and validate a module"},
		{"version", "<command>", "install, select, update, and remove Wago runtimes"},
	}
	for _, command := range commands {
		fmt.Fprintf(w, "  %-8s  %-19s  %s\n", command.name, command.args, command.summary)
	}
	fmt.Fprintf(w, "\n%s\n", bold("Flags:"))
	fmt.Fprintf(w, "  %-27s %s\n", "--version, -v", "print version and supported features")
	fmt.Fprintf(w, "  %-27s %s\n", "--help, -h", "show this help")
	fmt.Fprintf(w, "\n%-29s%s\n", "View the repo:", "https://github.com/wago-org/wago")
	fmt.Fprintf(w, "%-29s%s\n", "View the registry:", "https://plugins.wago.sh")
}
