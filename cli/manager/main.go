package manager

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/wago-org/wago/cli/internal/command"
	"github.com/wago-org/wago/cli/internal/handoff"
	versioninstall "github.com/wago-org/wago/cli/manager/commands/version/install"
	managerplugin "github.com/wago-org/wago/cli/manager/internal/plugin"
	managerversion "github.com/wago-org/wago/cli/manager/internal/version"
	"github.com/wago-org/wago/internal/wagopaths"
)

var version string
var installRuntimeForFirstRun = func() {
	fmt.Fprintln(os.Stderr, dim("No runtime selected. Opening `wago version install`…"))
	managerRoot.Child("version").Dispatch("wago version", []string{"install"})
}

var managerRoot = buildCommandRegistry()

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
	managerplugin.ConfigureManagerVersion(versionString())
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
	case "run":
		runWithFirstRunInstall(args)
		return
	}
	if cmd := managerRoot.Child(args[0]); cmd != nil {
		if cmd.Name == "plugin" &&
			handoff.RuntimeOwnsPluginCommand(args[1:]) &&
			!command.InvocationWantsHelp(cmd, args[1:]) {
			runActiveRunner(args)
			return
		}
		cmd.Dispatch("wago "+cmd.Name, args[1:])
		return
	}
	runActiveRunner(args)
}

func runWithFirstRunInstall(args []string) {
	if !versioninstall.EnsureRuntime(hasActiveRunner, installRuntimeForFirstRun) {
		return
	}
	runActiveRunner(args)
}

func hasActiveRunner() bool {
	_, _, _, _, ok := managerversion.ActiveRunner(wagopaths.DirsFor(versionString()))
	return ok
}

func printManagerVersion() {
	d := wagopaths.DirsFor(versionString())
	if path, active, profile, build, ok := managerversion.ActiveRunner(d); ok {
		command := exec.Command(path, "--version")
		command.Env = (handoff.Metadata{
			ManagerVersion: versionString(), ManagerExecutable: executablePath(),
		}).Environment(os.Environ())
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
	printVersionDetail("channel", managerversion.DiagnosticChannel(active, release))
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
	path, active, profile, build, ok := managerversion.ActiveRunner(d)
	if !ok {
		fatal("no active runtime; install one with `wago version install`")
	}
	path, err := managerplugin.Resolve(path, profile, args, commandEnvironment{})
	if err != nil {
		fatal("could not prepare plugins: %v", err)
	}
	cmd := exec.Command(path, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	cmd.Env = (handoff.Metadata{
		ManagerVersion: versionString(), ManagerExecutable: executablePath(),
		RuntimeChannel: active, RuntimeProfile: string(profile), RuntimeBuild: string(build),
	}).Environment(os.Environ())
	err = cmd.Run()
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
	fmt.Fprintf(w, "%s wago %s\n\n", bold("Usage:"), dim("<command> [flags]"))
	fmt.Fprintf(w, "%s\n", bold("Commands:"))
	commands := []struct {
		name, args, summary string
	}{
		{"run", "<file> [args...]", "compile and execute a WebAssembly module (default)"},
		{"status", "", "show the active runtime, project, plugins, and lockfile"},
		{"update", "", "update Wago, the active runtime, and plugins"},
		{"init", "", "initialize a local Wago project"},
		{"add", "<module>[@version]...", "add and enable plugins, then rebuild Wago"},
		{"rm", "<name>", "remove and disable a plugin"},
		{"plugin", "<command>", "install, update, verify, and publish plugins"},
		{"auth", "<command>", "authenticate to the registry (plugins.wago.sh)"},
		{"module", "<command>", "inspect a module's imports and required capabilities"},
		{"self", "<command>", "update or uninstall Wago"},
		{"build", "<file>", "precompile a WebAssembly module to a .wago artifact"},
		{"validate", "<file>", "decode and validate a module"},
		{"version", "<command>", "install, select, update, and remove Wago runtimes"},
		{"cache", "<command>", "inspect and clean regenerable Wago data"},
		{"config", "<command>", "configure Wago"},
	}
	nameWidth, argsWidth := 0, 0
	for _, command := range commands {
		nameWidth = max(nameWidth, len(command.name))
		argsWidth = max(argsWidth, len(command.args))
	}
	for _, command := range commands {
		fmt.Fprintf(w, "  %-*s  %s  %s\n", nameWidth, command.name, dim(fmt.Sprintf("%-*s", argsWidth, command.args)), command.summary)
	}
	fmt.Fprintf(w, "\n%s\n", bold("Flags:"))
	fmt.Fprintf(w, "  %-27s %s\n", "--version, -v", "print version and supported features")
	fmt.Fprintf(w, "  %-27s %s\n", "--help, -h", "show this help")
	fmt.Fprintf(w, "\n%-29s%s\n", "View the repo:", "https://github.com/wago-org/wago")
	fmt.Fprintf(w, "%-29s%s\n", "View the registry:", "https://plugins.wago.sh")
}
