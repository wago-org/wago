// Package runtime implements the executable WebAssembly runtime CLI. It is
// importable so generated plugin builds can assemble providers and call Main.
package runtime

import (
	"fmt"
	"os"
	"strings"

	"github.com/wago-org/wago"
	"github.com/wago-org/wago/cli/internal/automation"
	"github.com/wago-org/wago/cli/internal/command"
	"github.com/wago-org/wago/cli/internal/handoff"
	"github.com/wago-org/wago/cli/internal/ui"
	runtimeplugin "github.com/wago-org/wago/cli/runtime/internal/plugin"
	"github.com/wago-org/wago/cli/runtime/internal/profile"
	runtimeversion "github.com/wago-org/wago/cli/runtime/internal/version"
)

// version is the build stamp, passed in by the caller of Main (the cli/wago shim
// receives it via -ldflags "-X main.version=<tag>"). An empty value means a plain
// build with no version stamped in.
var version string
var artifactCacheIdentity string

func versionString() string {
	if version == "" {
		return "0.0.0"
	}
	return version
}

// root is the profile-specific runtime command registry. Keep its construction
// lazy so generated plugin binaries can validate their plugin set without
// loading project settings while the manager holds the project mutation lock.
var root *command.Cmd

func runtimeCommandRegistry() *command.Cmd {
	if root == nil {
		root = buildCommandRegistry()
	}
	return root
}

// Main runs the runtime command matching os.Args.
func Main(v string) {
	version = v
	registry := runtimeCommandRegistry()
	args, err := automation.ParseLeading(os.Args[1:])
	if err != nil {
		ui.Usage("%v", err)
	}
	if len(args) == 0 {
		if automation.JSON() {
			ui.Usage("missing command")
		}
		usage(os.Stderr)
		os.Exit(2)
	}
	switch args[0] {
	case "help", "-h", "--help":
		parseTopAutomation(args[1:])
		if automation.JSON() {
			writeRuntimeSchema()
			return
		}
		usage(os.Stdout)
		return
	case "commands":
		parseTopAutomation(args[1:])
		writeRuntimeSchema()
		return
	case "-v", "--version":
		parseTopAutomation(args[1:])
		runtimeversion.Print(versionString(), profile.Name(), profile.Build(), runtimeplugin.Summary())
		return
	}
	// Help describes the invoked CLI, not a generated plugin artifact that may
	// have been compiled by an older Wago. Resolve it before plugin handoff so
	// every command advertises the current interface.
	if cmd := registry.Child(args[0]); cmd != nil && command.InvocationWantsHelp(cmd, args[1:]) {
		cmd.Dispatch("wago "+cmd.Name, args[1:])
		return
	}
	if cmd := registry.Child(args[0]); cmd != nil {
		cmd.Dispatch("wago "+cmd.Name, args[1:])
		return
	}
	// Not a known command. A file path (or a leading flag) is an implicit `run`;
	// anything else is an unknown command rather than a mystery file-open.
	if handoff.LooksLikeRuntimeTarget(args[0]) || strings.HasPrefix(args[0], "-") {
		registry.Child("run").Dispatch("wago run", args)
		return
	}
	if automation.JSON() {
		hint := "wago help --json"
		if suggestion := command.SuggestChild(registry, args[0]); suggestion != "" {
			hint = "wago " + suggestion + " --help"
		}
		ui.UsageHint(hint, "unknown command %q", args[0])
	}
	fmt.Fprintf(os.Stderr, "%s unknown command %q\n\n", red("wago:"), args[0])
	if suggestion := command.SuggestChild(registry, args[0]); suggestion != "" {
		fmt.Fprintf(os.Stderr, "Did you mean %q?\n\n", suggestion)
	}
	usage(os.Stderr)
	os.Exit(2)
}

// MainWithArtifactCacheIdentity runs the runtime CLI with an explicit build
// fingerprint for compiled-module cache keys. Generated plugin runtimes use
// this entry point because their throwaway build module intentionally disables
// VCS stamping. The identity must change whenever generated code, plugin ABI,
// dependencies, or other native-output inputs change.
func MainWithArtifactCacheIdentity(v, identity string) {
	artifactCacheIdentity = identity
	Main(v)
}

// MainWithPluginSet runs a generated runtime with its explicit provider catalog
// and exact reviewed selections. No provider init function mutates global Wago
// state; the set is handed to each runtime that needs plugins.
func MainWithPluginSet(v, identity string, set wago.PluginSet) {
	artifactCacheIdentity = identity
	runtimeplugin.Configure(set)
	if err := runtimeplugin.Verify(); err != nil {
		ui.Fatal("plugins: %v", err)
	}
	Main(v)
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

func writeRuntimeSchema() {
	if err := command.WriteSchema(os.Stdout, runtimeCommandRegistry()); err != nil {
		ui.Fatal("commands: %v", err)
	}
}

// usage prints the top-level help. The layout follows a single house style (see
// Cmd.printHelp for per-command help): a one-line banner with the version, a
// usage line, the command table (rendered from the registry so a new command
// shows up automatically), the global flags, then a documentation/repository footer. Per-command
// flags live in each command's own `--help`. Headings are bold and argument
// syntax is dimmed so command names and descriptions remain easy to scan.
func usage(w *os.File) {
	fmt.Fprintf(w, "%s is a pure-Go (no cgo) WebAssembly engine. (v%s)\n\n", bold("wago"), versionString())
	fmt.Fprintf(w, "%s wago %s\n\n", bold("Usage:"), dim("<command> [flags]"))

	fmt.Fprintf(w, "%s\n", bold("Commands:"))
	writeCommandList(w)

	// Global flags, aligned to the same column as the footer links below.
	fmt.Fprintf(w, "\n%s\n", bold("Flags:"))
	fmt.Fprintf(w, "  %-27s %s\n", "--version, -v", "show version information")
	fmt.Fprintf(w, "  %-27s %s\n", "--help, -h", "show this help")
	fmt.Fprintf(w, "  %-27s %s\n", "--json, -j", "emit machine-readable JSON when supported")
	fmt.Fprintf(w, "  %-27s %s\n", "--no-input", "never prompt; fail when input is missing")
	fmt.Fprintf(w, "  %-27s %s\n", "--dry-run", "show supported mutation plans")
	fmt.Fprintf(w, "  %-27s %s\n", "--locked", "do not change project manifests or lockfiles")
	fmt.Fprintf(w, "  %-27s %s\n", "--offline", "use only installed and cached resources")

	fmt.Fprintf(w, "\n%-29s%s\n", "View the repo:", "https://github.com/wago-org/wago")
	fmt.Fprintf(w, "%-29s%s\n", "View the registry:", "https://plugins.wago.sh")
}

// writeCommandList prints the top-level commands as an aligned name / arg-synopsis
// / description table, sizing the name and arg columns to their widest entries.
func writeCommandList(w *os.File) {
	commands := topLevelHelpCommands()
	nameW, argW := 0, 0
	for _, c := range commands {
		nameW = max(nameW, len(c.Name))
		argW = max(argW, len(command.ArgSynopsis(c)))
	}
	for _, c := range commands {
		args := fmt.Sprintf("%-*s", argW, command.ArgSynopsis(c))
		fmt.Fprintf(w, "  %-*s  %s  %s\n", nameW, c.Name, dim(args), c.Summary)
	}
}

// topLevelHelpCommands reports only commands implemented by this runtime.
// The manager owns the cohesive user-facing help and never asks a runtime to
// advertise manager commands.
func topLevelHelpCommands() []*command.Cmd {
	return append([]*command.Cmd(nil), runtimeCommandRegistry().Children...)
}
