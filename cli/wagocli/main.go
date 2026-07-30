//go:build !wago_manager

// Package wagocli is the wago command implementation. It lives in an importable
// package (rather than package main) so a generated .wago build module can link
// wago together with plugins — the cli/wago binary is a thin shim that calls Main.
package wagocli

import (
	"fmt"
	"os"
	"strings"
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

// root is the command tree, assembled once at startup. usage() and main() both
// read it, and it's built at package init so it's populated even when a test
// calls usage() directly.
var root = buildRoot()

// buildRoot wires every command onto the root. The order here is the order shown
// by `wago --help`.
func buildRoot() *Cmd {
	r := &Cmd{Name: "wago"}
	r.Children = append(r.Children, runnerCommands()...)
	return r
}

// Main is the wago entry point. version is the build stamp (see the cli/wago
// shim). It runs the command matching os.Args and exits the process itself.
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
		printVersion()
		return
	}
	// Transparently hand off to the active plugin-aware wago build: a project's
	// local build when its wago.json declares packages, otherwise the global build.
	// Build-management and toolchain/meta commands are skipped so they don't
	// rebuild circularly or need a plugin runtime to run.
	prepareRunnerInvocation(args)
	if cmd := root.child(args[0]); cmd != nil {
		cmd.Dispatch("wago "+cmd.Name, args[1:])
		return
	}
	// Not a known command. A file path (or a leading flag) is an implicit `run`;
	// anything else is an unknown command rather than a mystery file-open.
	if looksLikeRunTarget(args[0]) || strings.HasPrefix(args[0], "-") {
		root.child("run").Dispatch("wago run", args)
		return
	}
	fmt.Fprintf(os.Stderr, "%s unknown command %q\n\n", red("wago:"), args[0])
	usage(os.Stderr)
	os.Exit(2)
}

// usesPluginRuntime reports whether an invocation should hand off to the active
// plugin-aware wago build. Most commands do (run, module, validate, and the
// plugin introspection commands, so they see the compiled plugin set). Commands
// that build/manage the package set — or are toolchain/meta and don't need
// plugins — stay on the invoked wago, so they neither rebuild circularly nor
// require a plugin runtime to run.
func usesPluginRuntime(args []string) bool {
	if len(args) == 0 {
		return false
	}
	switch args[0] {
	case "version", "auth", "env", "opts",
		"add", "install", "i", "rm", "remove", "uninstall": // build-management / meta: run on base
		return false
	case "plugin", "plugins":
		if len(args) >= 2 {
			switch args[1] {
			case "update", "up", "upgrade", "grant", "publish", "unpublish", "deprecate":
				return false
			}
		}
	}
	return true
}

// looksLikeRunTarget reports whether s is plausibly a module to run: a .wasm/.wago
// name, or an existing file. It keeps `wago app.wasm 2 3` working without letting
// a mistyped command silently become a failed file open.
func looksLikeRunTarget(s string) bool {
	if strings.HasSuffix(s, ".wasm") || strings.HasSuffix(s, ".wago") {
		return true
	}
	fi, err := os.Stat(s)
	return err == nil && !fi.IsDir()
}

// usage prints the top-level help. The layout follows a single house style (see
// Cmd.printHelp for per-command help): a one-line banner with the version, a
// usage line, the command table (rendered from the registry so a new command
// shows up automatically), the global flags, then a docs/repo footer. Per-command
// flags live in each command's own `--help`. Output is monochrome (bold only).
func usage(w *os.File) {
	fmt.Fprintf(w, "%s is a pure-Go (no cgo) WebAssembly engine. (v%s)\n\n", bold("wago"), versionString())
	fmt.Fprintf(w, "%s wago [run] [...flags] <file> [...args]\n\n", bold("Usage:"))

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
		argW = max(argW, len(cmdArg(c)))
	}
	for _, c := range commands {
		fmt.Fprintf(w, "  %-*s  %-*s  %s\n", nameW, c.Name, argW, cmdArg(c), c.Summary)
	}
}

// topLevelHelpCommands presents the manager and selected runtime as one CLI.
// The manager injects its executable path when it launches a runtime, so
// stripped profiles can advertise manager-owned commands without implementing
// them or carrying their networking dependencies.
func topLevelHelpCommands() []*Cmd {
	commands := append([]*Cmd(nil), root.Children...)
	if os.Getenv("WAGO_MANAGER_EXECUTABLE") == "" {
		return commands
	}
	for _, command := range []*Cmd{authCommand(), versionCommand()} {
		if root.child(command.Name) == nil {
			commands = append(commands, command)
		}
	}
	return commands
}
