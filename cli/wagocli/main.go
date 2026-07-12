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
	r.Children = append(r.Children,
		runCommand(),
		addCommand(),
		rmCommand(),
		pluginCommand(),
		authCommand(),
		moduleCommand(),
		envCommand(),
		optsCommand(),
		buildCommand(),
		validateCommand(),
		versionCommand(),
	)
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
	// In a project (a wago.json declaring packages), transparently hand off to the
	// project's own wago — .wago/bin/wago, built on demand — so every command runs
	// with the local package set compiled in. With no project it stays on this
	// (global) wago. Build-management and toolchain/meta commands are skipped so
	// they don't rebuild circularly or need a project to run.
	if usesProjectBuild(args) {
		maybeReexecLocal()
	}
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

// usesProjectBuild reports whether an invocation should hand off to the project's
// local wago build. Most commands do (run, module, validate, and the pkg
// introspection commands, so they see the local package set). Commands that
// build/manage the package set — or are toolchain/meta and don't need packages —
// stay on the invoked wago, so they neither rebuild circularly nor require a
// project to run.
func usesProjectBuild(args []string) bool {
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
	nameW, argW := 0, 0
	for _, c := range root.Children {
		nameW = max(nameW, len(c.Name))
		argW = max(argW, len(cmdArg(c)))
	}
	for _, c := range root.Children {
		fmt.Fprintf(w, "  %-*s  %-*s  %s\n", nameW, c.Name, argW, cmdArg(c), c.Summary)
	}
}

// ---- output helpers -----------------------------------------------------

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "%s "+format+"\n", append([]any{red("wago:")}, args...)...)
	os.Exit(1)
}

var useColor = colorEnabled()

func colorEnabled() bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	fi, err := os.Stdout.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

func paint(code, s string) string {
	if !useColor {
		return s
	}
	return "\x1b[" + code + "m" + s + "\x1b[0m"
}

func bold(s string) string { return paint("1", s) }
func dim(s string) string  { return paint("2", s) }
func red(s string) string  { return paint("31", s) }
func cyan(s string) string { return paint("36", s) }
