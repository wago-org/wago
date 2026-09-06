package manager

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/wago-org/wago/cli/internal/automation"
	"github.com/wago-org/wago/cli/internal/command"
	"github.com/wago-org/wago/cli/internal/handoff"
	"github.com/wago-org/wago/cli/internal/ui"
	"github.com/wago-org/wago/cli/internal/watchsupervisor"
	versioninstall "github.com/wago-org/wago/cli/manager/commands/version/install"
	managerplugin "github.com/wago-org/wago/cli/manager/internal/plugin"
	managerversion "github.com/wago-org/wago/cli/manager/internal/version"
	"github.com/wago-org/wago/internal/managedrelease"
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
	if dispatched, err := managedrelease.Dispatch(); dispatched || err != nil {
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
		}
		os.Exit(managedrelease.ExitCode(err))
	}
	watchsupervisor.Enter()
	version = v
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	// NotifyContext suppresses the default interrupt action while registered.
	// Restore it after the first signal so context-aware work can unwind, while
	// a second Ctrl-C still terminates commands that have not reached a context
	// cancellation point.
	go func() {
		<-ctx.Done()
		stop()
	}()
	managerRoot = buildCommandRegistryContext(ctx)
	managerplugin.ConfigureManagerVersion(versionString())
	args, err := automation.ParseLeading(os.Args[1:])
	if err != nil {
		ui.Usage("%v", err)
	}
	if len(args) == 0 {
		if automation.JSON() {
			ui.Usage("missing command")
		}
		managerUsage(os.Stderr)
		os.Exit(2)
	}
	configureInvocationAutomation(args)
	if dispatchRuntimeDiscovery(args) {
		return
	}
	switch args[0] {
	case "__complete":
		for _, candidate := range command.Complete(managerCommandRoot(), args[1:]) {
			fmt.Println(candidate)
		}
		return
	case "help", "-h", "--help":
		parseTopAutomation(args[1:])
		if automation.JSON() {
			writeManagerSchema()
			return
		}
		managerUsage(os.Stdout)
		return
	case "commands":
		parseTopAutomation(args[1:])
		writeManagerSchema()
		return
	case "-v", "--version":
		parseTopAutomation(args[1:])
		printManagerVersion()
		return
	case "run":
		runWithFirstRunInstall(args)
		return
	}
	if cmd := managerRoot.Child(args[0]); cmd != nil {
		childArgs := args[1:]
		if cmd.Name == "plugin" {
			var err error
			childArgs, err = automation.ParseLeading(childArgs)
			if err != nil {
				ui.Usage("plugin: %v", err)
			}
		}
		if cmd.Name == "plugin" &&
			handoff.RuntimeOwnsPluginCommand(childArgs) &&
			!command.InvocationWantsHelp(cmd, childArgs) {
			runActiveRunner(append([]string{args[0]}, childArgs...))
			return
		}
		cmd.Dispatch("wago "+cmd.Name, childArgs)
		return
	}
	if shouldForwardToRuntime(args[0]) {
		runActiveRunner(args)
		return
	}
	managerUnknownCommand(args[0])
}

func shouldForwardToRuntime(name string) bool {
	return managerCommandRoot().Child(name) != nil || handoff.LooksLikeRuntimeTarget(name) || strings.HasPrefix(name, "-")
}

func managerUnknownCommand(name string) {
	root := managerCommandRoot()
	if automation.JSON() {
		hint := "wago help --json"
		if suggestion := command.SuggestChild(root, name); suggestion != "" {
			hint = "wago " + suggestion + " --help"
		}
		ui.UsageHint(hint, "unknown command %q", name)
	}
	fmt.Fprintf(os.Stderr, "%s unknown command %q\n\n", ui.Red("wago:"), name)
	if suggestion := command.SuggestChild(root, name); suggestion != "" {
		fmt.Fprintf(os.Stderr, "Did you mean %q?\n\n", suggestion)
	}
	managerUsage(os.Stderr)
	os.Exit(2)
}

// dispatchRuntimeDiscovery keeps command help and nested typo diagnostics
// available before a runtime has been installed. The manager carries the
// runtime command schema specifically so newcomers can learn how to install and
// use Wago without first completing an installation or reaching the network.
func dispatchRuntimeDiscovery(args []string) bool {
	root := &command.Cmd{Name: "wago", Children: runtimeSchemaCommands()}
	cmd := root.Child(args[0])
	if cmd == nil {
		return false
	}
	childArgs := args[1:]
	if command.InvocationWantsHelp(cmd, childArgs) {
		cmd.Dispatch("wago "+cmd.Name, childArgs)
		return true
	}
	if len(cmd.Children) == 0 {
		return false
	}
	remaining, err := automation.ParseLeading(childArgs)
	if err != nil || len(remaining) == 0 || strings.HasPrefix(remaining[0], "-") || cmd.Child(remaining[0]) != nil {
		return false
	}
	cmd.Dispatch("wago "+cmd.Name, childArgs)
	return true
}

// configureInvocationAutomation observes flags on runtime-owned commands before
// the manager decides whether it can hand them to an active runtime. This keeps
// pre-handoff errors and first-run behavior consistent with the runtime parser.
func configureInvocationAutomation(args []string) {
	root := &command.Cmd{
		Name:     "wago",
		Children: append(append([]*command.Cmd(nil), managerRoot.Children...), fallbackRuntimeSchemaCommands()...),
	}
	current, remaining := root, args
	for len(current.Children) != 0 {
		var err error
		remaining, err = automation.ParseLeading(remaining)
		if err != nil || len(remaining) == 0 {
			return
		}
		child := current.Child(remaining[0])
		if child == nil {
			if current == root && (handoff.LooksLikeRuntimeTarget(remaining[0]) || strings.HasPrefix(remaining[0], "-")) {
				command.ConfigureAutomation(root.Child("run"), remaining)
			}
			return
		}
		current, remaining = child, remaining[1:]
	}
	command.ConfigureAutomation(current, remaining)
}

func parseTopAutomation(args []string) {
	remaining, err := automation.ParseLeading(args)
	if err != nil {
		ui.Usage("%v", err)
	}
	if len(remaining) != 0 {
		ui.Usage("unexpected argument %q", remaining[0])
	}
}

func writeManagerSchema() {
	if err := command.WriteSchema(os.Stdout, managerCommandRoot()); err != nil {
		ui.Fatal("commands: %v", err)
	}
}

func managerCommandRoot() *command.Cmd {
	root := &command.Cmd{Name: "wago", Children: append([]*command.Cmd(nil), managerRoot.Children...)}
	root.Children = append(root.Children, runtimeSchemaCommands()...)
	return root
}

func runtimeSchemaCommands() []*command.Cmd {
	if commands := activeRuntimeSchemaCommands(); len(commands) != 0 {
		return commands
	}
	return fallbackRuntimeSchemaCommands()
}

func fallbackRuntimeSchemaCommands() []*command.Cmd {
	return handoff.RuntimeCommands()
}

func activeRuntimeSchemaCommands() []*command.Cmd {
	d := wagopaths.DirsFor(versionString())
	path, active, profile, build, ok := managerversion.ActiveRunner(d)
	if !ok {
		return nil
	}
	cmd := exec.Command(path, "commands", "--json")
	cmd.Env = automation.Environment((handoff.Metadata{
		ManagerVersion: versionString(), ManagerExecutable: executablePath(),
		RuntimeChannel: active, RuntimeProfile: string(profile), RuntimeBuild: string(build),
	}).Environment(os.Environ()))
	output, err := cmd.Output()
	if err != nil {
		return nil
	}
	var schema command.CommandSchema
	if json.Unmarshal(output, &schema) != nil {
		return nil
	}
	wanted := map[string]bool{"run": true, "module": true, "build": true, "validate": true}
	var commands []*command.Cmd
	for _, spec := range schema.Commands {
		if wanted[spec.Name] {
			commands = append(commands, commandFromSpec(spec))
		}
	}
	return commands
}

func commandFromSpec(spec command.CommandSpec) *command.Cmd {
	result := &command.Cmd{
		Name: spec.Name, Aliases: append([]string(nil), spec.Aliases...), Summary: spec.Summary,
		Args: spec.Arguments, PassThrough: spec.PassThrough,
	}
	for _, flag := range spec.Flags {
		if flag.Name == "help" || flag.Name == "help-optimizations" || flag.Name == "json" || flag.Name == "dry-run" || flag.Name == "no-input" || flag.Name == "locked" || flag.Name == "offline" {
			continue
		}
		value := command.Flag{Name: flag.Name, Short: flag.Short, Bool: flag.Type == "boolean", Arg: flag.Value, Help: flag.Summary}
		if flag.Category == "optimization" {
			result.Knobs = append(result.Knobs, value)
		} else {
			result.Flags = append(result.Flags, value)
		}
	}
	for _, child := range spec.Commands {
		result.Children = append(result.Children, commandFromSpec(child))
	}
	for _, flag := range spec.Flags {
		switch flag.Name {
		case "json":
			if !strings.Contains(flag.Summary, "--dry-run") {
				result.Automation |= command.JSONOutput
			}
		case "dry-run":
			result.Automation |= command.DryRun
		}
	}
	return result
}

func runWithFirstRunInstall(args []string) {
	if !hasActiveRunner() && automation.NoInput() {
		ui.FailHint(1, "runtime_not_installed", "wago version install --latest --use", "no active runtime is selected")
	}
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
	if automation.JSON() {
		path, active, profile, build, ok := managerversion.ActiveRunner(d)
		ui.PrintJSON(map[string]any{
			"managerVersion": versionString(), "managerPath": executablePath(),
			"runtimeSelected": ok, "runtimeVersion": active, "runtimeProfile": string(profile), "runtimeBuild": string(build), "runtimePath": path,
			"platform": runtime.GOOS + "/" + runtime.GOARCH, "toolchain": runtime.Compiler + " " + runtime.Version(),
		})
		return
	}
	if path, active, profile, build, ok := managerversion.ActiveRunner(d); ok {
		command := exec.Command(path, "--version")
		command.Env = (handoff.Metadata{
			ManagerVersion: versionString(), ManagerExecutable: executablePath(),
		}).Environment(os.Environ())
		output, err := command.Output()
		if err == nil && strings.HasPrefix(string(output), "Wago\n") {
			_, _ = os.Stdout.Write(versionWithoutFeatures(output))
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

func versionWithoutFeatures(output []byte) []byte {
	lines := strings.SplitAfter(string(output), "\n")
	filtered := lines[:0]
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) > 0 && strings.TrimSuffix(fields[0], ":") == "features" {
			continue
		}
		filtered = append(filtered, line)
	}
	return []byte(strings.Join(filtered, ""))
}

func printLegacyManagerVersion(active string, profile wagopaths.Profile, build wagopaths.Build, path string, output []byte) {
	release, platform := legacyRunnerVersionFields(string(output))
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
}

func legacyRunnerVersionFields(output string) (release, platform string) {
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
	}
	return release, platform
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
		ui.FatalHint("wago version install --latest --use", "no active runtime is selected")
	}
	path, err := managerplugin.Resolve(path, profile, args, commandEnvironment{})
	if err != nil {
		fatal("could not prepare plugins: %v", err)
	}
	cmd := exec.Command(path, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	cmd.Env = automation.Environment((handoff.Metadata{
		ManagerVersion: versionString(), ManagerExecutable: executablePath(),
		RuntimeChannel: active, RuntimeProfile: string(profile), RuntimeBuild: string(build),
	}).Environment(os.Environ()))
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
	fmt.Fprintf(w, "%s is a wonderfully quick, compact, and extensible WebAssembly runtime for Go.\n\n", bold("Wago"))
	fmt.Fprintf(w, "%s wago %s\n\n", bold("Usage:"), dim("<command> [flags]"))
	fmt.Fprintf(w, "%s\n", bold("Commands:"))
	commands := []struct {
		name, args, summary string
	}{
		{"run", "<file> [args...]", "run a WebAssembly module (default)"},
		{"status", "", "show the runtime, project, plugins, and lockfile"},
		{"update", "", "update Wago, the runtime, and plugins"},
		{"init", "", "create a Wago project"},
		{"add", "<module>[@version]...", "add plugins and rebuild Wago"},
		{"rm", "<name>", "remove a plugin"},
		{"plugin", "<command>", "manage and publish plugins"},
		{"auth", "<command>", "sign in to plugins.wago.sh"},
		{"module", "<command>", "inspect module imports and capabilities"},
		{"self", "<command>", "update or uninstall Wago"},
		{"compile", "<file>", "build a standalone executable"},
		{"build", "<file>", "precompile a module to .wago"},
		{"validate", "<file>", "validate a WebAssembly module"},
		{"version", "<command>", "manage Wago runtimes"},
		{"cache", "<command>", "inspect and clean cached data"},
		{"config", "<command>", "configure Wago"},
		{"commands", "", "describe commands as JSON"},
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
	fmt.Fprintf(w, "  %-27s %s\n", "--version, -v", "show version information")
	fmt.Fprintf(w, "  %-27s %s\n", "--help, -h", "show this help")
	fmt.Fprintf(w, "  %-27s %s\n", "--json, -j", "emit JSON when supported")
	fmt.Fprintf(w, "\n%-13s%s\n", "Repository:", "https://github.com/wago-org/wago")
	fmt.Fprintf(w, "%-13s%s\n", "Plugins:", "https://plugins.wago.sh")
}
