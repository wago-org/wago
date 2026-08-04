package manager

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/wago-org/wago/cli/internal/automation"
	"github.com/wago-org/wago/cli/internal/command"
	"github.com/wago-org/wago/cli/internal/handoff"
	"github.com/wago-org/wago/cli/internal/ui"
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
	runActiveRunner(args)
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
	parallel := command.Flag{Name: "parallel", Short: "p", Arg: "[workers]", Help: "parallel function validation and compilation"}
	pluginFlags := []command.Flag{
		{Name: "plugin", Arg: "<names>", Help: "comma-separated extra plugins to enable"},
		{Name: "local", Bool: true, Help: "use this project's plugins"},
		{Name: "global", Bool: true, Help: "use shared user-wide plugins"},
		{Name: "bare", Bool: true, Help: "run without plugins"},
	}
	runFlags := []command.Flag{
		{Name: "invoke", Short: "e", Arg: "<name>", Help: "exported function to call"},
		{Name: "watch", Short: "w", Bool: true, Help: "rerun when the module changes"},
		{Name: "watch-interval", Arg: "<duration>", Help: "watch polling interval"},
		parallel,
	}
	runFlags = append(runFlags, pluginFlags...)
	buildFlags := append([]command.Flag{{Name: "output", Short: "o", Arg: "<file>", Help: "output path"}, parallel}, pluginFlags...)
	return []*command.Cmd{
		{Name: "run", Summary: "compile and execute a WebAssembly module (default)", Args: "<file> [args...]", Flags: runFlags, PassThrough: true},
		{Name: "module", Summary: "inspect a module's imports and required capabilities", Children: []*command.Cmd{
			{Name: "imports", Summary: "list a module's imports", Args: "<file>", Automation: command.JSONOutput},
			{Name: "capabilities", Aliases: []string{"caps"}, Summary: "list required capabilities", Args: "<file>", Automation: command.JSONOutput},
		}},
		{Name: "build", Summary: "precompile a WebAssembly module", Args: "<file>", Flags: buildFlags, Automation: command.DryRun},
		{Name: "validate", Aliases: []string{"check"}, Summary: "decode and validate a module", Args: "<file>", Flags: []command.Flag{parallel}, Automation: command.JSONOutput},
	}
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
		if flag.Name == "help" || flag.Name == "json" || flag.Name == "dry-run" || flag.Name == "no-input" || flag.Name == "locked" || flag.Name == "offline" {
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
		ui.FailHint(1, "runtime_not_installed", "wago version install --canary --profile standard --build normal --use", "no active runtime is selected")
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
		ui.FatalHint("wago version install", "no active runtime is selected")
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
	fmt.Fprintf(w, "  %-27s %s\n", "--version, -v", "show version and supported features")
	fmt.Fprintf(w, "  %-27s %s\n", "--help, -h", "show this help")
	fmt.Fprintf(w, "  %-27s %s\n", "--json, -j", "emit JSON when supported")
	fmt.Fprintf(w, "\n%-13s%s\n", "Repository:", "https://github.com/wago-org/wago")
	fmt.Fprintf(w, "%-13s%s\n", "Plugins:", "https://plugins.wago.sh")
}
