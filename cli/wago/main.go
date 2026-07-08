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

// usage prints the top-level help: the command list (rendered from the registry
// so a new command shows up automatically), then the run flags and examples. The
// command list uses a fixed 26-column left field.
func usage(w *os.File) {
	fmt.Fprintf(w, "%s — a pure-Go (no cgo) WebAssembly engine\n\n", bold("wago"))
	fmt.Fprintf(w, "%s wago [run] <file> [args...]\n\n", bold("Usage:"))

	fmt.Fprintf(w, "%s\n", bold("Commands:"))
	writeCommandList(w)

	fmt.Fprintf(w, `
%s
  -v, --version             print version and supported features
  -e, --invoke <name>       export to call
      --plugin <names>      comma-separated plugins to enable (see: wago plugin list)
                            a module exporting _start runs as a command; add
                            --plugin wasi (or wasi/p1, wasi/unstable) for a WASI program
      --bounds <mode>       bounds checks: defer (skip provably-redundant; default) | all

%s
  wago add.wasm 2 3
  wago run -e fib fib.wasm 30
  wago run --plugin wasi app.wasm

For run, <file> is raw .wasm or a precompiled .wago. run args are typed by
the signature; override per-arg with a suffix:  42   7:i64   3.5:f64
`, bold("Flags:"), bold("Examples:"))
}

// writeCommandList prints the top-level commands as aligned "name  summary" rows.
// The 26-column left field reproduces the historical layout.
func writeCommandList(w *os.File) {
	for _, c := range root.Children {
		left := c.Name
		if c.Args != "" {
			left += " " + c.Args
		}
		fmt.Fprintf(w, "  %-26s%s\n", left, c.Summary)
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
