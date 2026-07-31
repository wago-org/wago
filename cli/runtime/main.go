// Package runtime implements the executable WebAssembly runtime CLI. It is
// importable so generated plugin builds can register extensions and call Main.
package runtime

import (
	"fmt"
	"os"
	"strings"

	"github.com/wago-org/wago/cli/internal/command"
	runcommand "github.com/wago-org/wago/cli/runtime/commands/run"
	runtimeplugin "github.com/wago-org/wago/cli/runtime/internal/plugin"
	"github.com/wago-org/wago/cli/runtime/internal/profile"
	runtimeversion "github.com/wago-org/wago/cli/runtime/internal/version"
)

// version is the build stamp, passed in by the caller of Main (the cli/wago shim
// receives it via -ldflags "-X main.version=<tag>"). An empty value means a plain
// build with no version stamped in.
var version string

func versionString() string {
	if version == "" {
		return "0.0.0"
	}
	return version
}

// root is the profile-specific runtime command registry.
var root = buildCommandRegistry()

// Main runs the runtime command matching os.Args.
func Main(v string) {
	version = v
	args := os.Args[1:]
	if len(args) == 0 {
		usage(os.Stderr)
		os.Exit(2)
	}
	switch args[0] {
	case "help", "-h", "--help":
		usage(os.Stdout)
		return
	case "-v", "--version":
		runtimeversion.Print(versionString(), profile.Name(), profile.Build(), runtimeplugin.Summary())
		return
	}
	// Help describes the invoked CLI, not a generated plugin artifact that may
	// have been compiled by an older Wago. Resolve it before plugin handoff so
	// every command advertises the current interface.
	if cmd := root.Child(args[0]); cmd != nil && command.InvocationWantsHelp(cmd, args[1:]) {
		cmd.Dispatch("wago "+cmd.Name, args[1:])
		return
	}
	if cmd := root.Child(args[0]); cmd != nil {
		cmd.Dispatch("wago "+cmd.Name, args[1:])
		return
	}
	// Not a known command. A file path (or a leading flag) is an implicit `run`;
	// anything else is an unknown command rather than a mystery file-open.
	if runcommand.LooksLikeTarget(args[0]) || strings.HasPrefix(args[0], "-") {
		root.Child("run").Dispatch("wago run", args)
		return
	}
	fmt.Fprintf(os.Stderr, "%s unknown command %q\n\n", red("wago:"), args[0])
	usage(os.Stderr)
	os.Exit(2)
}

// usage prints the top-level help. The layout follows a single house style (see
// Cmd.printHelp for per-command help): a one-line banner with the version, a
// usage line, the command table (rendered from the registry so a new command
// shows up automatically), the global flags, then a docs/repo footer. Per-command
// flags live in each command's own `--help`. Headings are bold and argument
// syntax is dimmed so command names and descriptions remain easy to scan.
func usage(w *os.File) {
	fmt.Fprintf(w, "%s is a pure-Go (no cgo) WebAssembly engine. (v%s)\n\n", bold("wago"), versionString())
	fmt.Fprintf(w, "%s wago %s\n\n", bold("Usage:"), dim("<command> [flags]"))

	fmt.Fprintf(w, "%s\n", bold("Commands:"))
	writeCommandList(w)

	// Global flags, aligned to the same column as the footer links below.
	fmt.Fprintf(w, "\n%s\n", bold("Flags:"))
	fmt.Fprintf(w, "  %-27s %s\n", "--version, -v", "print version and supported features")
	fmt.Fprintf(w, "  %-27s %s\n", "--help, -h", "show this help")

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
	return append([]*command.Cmd(nil), root.Children...)
}
