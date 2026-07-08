// Command wago runs WebAssembly modules.
package main

import (
	"fmt"
	"os"
	"strings"
)

// version is overridden at build time via -ldflags "-X main.version=<tag>" (see
// `make build` / `make build-release`). It must be an uninitialized var: TinyGo
// only honors -X for variables declared without an initializer. An empty value
// means a plain `go build` with no version stamped in.
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
		authCommand(),
		pkgCommand(),
		pluginCommand(),
		moduleCommand(),
		envCommand(),
		buildCommand(),
		validateCommand(),
		versionCommand(),
	)
	return r
}

func main() {
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
	fmt.Fprintf(w, "%-29s%s\n", "View the registry:", "https://pkg.wago.sh")
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
