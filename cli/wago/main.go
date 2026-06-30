// Command wago compiles, runs, and tests WebAssembly modules.
package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/wago-org/wago"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

const version = "0.1.0"

func main() {
	a := os.Args[1:]
	if len(a) == 0 {
		usage(os.Stderr)
		os.Exit(2)
	}
	switch a[0] {
	case "run":
		runCmd(a[1:])
	case "test":
		testCmd(a[1:])
	case "build":
		buildCmd(a[1:])
	case "validate":
		validateCmd(a[1:])
	case "version", "--version", "-v":
		versionCmd()
	case "help", "-h", "--help":
		usage(os.Stdout)
	default:
		runCmd(a) // `wago <file> [args...]` shorthand
	}
}

func usage(w *os.File) {
	fmt.Fprintf(w, `%s — a pure-Go (no cgo) WebAssembly engine

%s wago <command> [flags] [args]

%s
  run <file> [args...]      compile and execute an export
  test <file>               run test* exports and report pass/fail
  build <file> [-o out]     ahead-of-time compile to a .wago module
  validate <file>           check that a module compiles
  version                   print version and supported features

%s
  -e, --invoke <name>       (run) export to call
  -o, --out <path>          (build) output path

%s
  wago run add.wasm 2 3
  wago run -e fib fib.wasm 30
  wago test suite.wasm
  wago build app.wasm -o app.wago

A <file> is raw .wasm or a precompiled .wago. run args are typed by the
signature; override per-arg with a suffix:  42   7:i64   3.5:f64
`, bold("wago"), bold("Usage:"), bold("Commands:"), bold("Flags:"), bold("Examples:"))
}

func versionCmd() {
	fmt.Printf("%s %s (linux/amd64)\n", bold("wago"), version)
	fmt.Printf("%s %s\n", dim("features:"), wago.SupportedFeatures())
	if wago.GuardPageSupported() {
		fmt.Printf("%s signals-based bounds checks available\n", dim("guard-page:"))
	}
}

// ---- run ----------------------------------------------------------------

func runCmd(args []string) {
	var invoke string
	pos, err := extractOpts(args, map[string]*string{"-e": &invoke, "--invoke": &invoke})
	if err != nil {
		fatal("run: %v", err)
	}
	if len(pos) < 1 {
		fatal("run: need a <file>")
	}
	c := mustLoad(pos[0])
	export := mustResolveExport(c, invoke)
	params, results, _ := c.Signature(export)
	vals := mustParseArgs(pos[1:], params)

	in, err := wago.Instantiate(c, autoHosts(c, true))
	if err != nil {
		fatal("%v", err)
	}
	defer in.Close()
	res, err := in.Invoke(export, vals...)
	if err != nil {
		fatal("%s %s", red("trap:"), trapReason(err))
	}
	fmt.Println(format(export, vals, res, params, results))
}

// ---- test ---------------------------------------------------------------

type testResult struct {
	name   string
	pass   bool
	reason string
	took   time.Duration
}

func testCmd(args []string) {
	pos, err := extractOpts(args, nil)
	if err != nil {
		fatal("test: %v", err)
	}
	if len(pos) < 1 {
		fatal("test: need a <file>")
	}
	c := mustLoad(pos[0])

	var tests []string
	for _, name := range c.ExportedFunctions() {
		if !isTestName(name) {
			continue
		}
		if p, _, _ := c.Signature(name); len(p) != 0 {
			continue // can't auto-call a test that takes arguments
		}
		tests = append(tests, name)
	}

	fmt.Printf("\n%s %s\n\n", dim("wago test"), filepath.Base(pos[0]))
	if len(tests) == 0 {
		fmt.Printf("  %s no tests found (export functions named test*)\n\n", yellow("!"))
		os.Exit(1)
	}

	results := make([]testResult, 0, len(tests))
	start := time.Now()
	for _, name := range tests {
		results = append(results, runOneTest(c, name))
	}
	total := time.Since(start)

	width := 0
	for _, r := range results {
		if len(r.name) > width {
			width = len(r.name)
		}
	}
	var passed, failed int
	for _, r := range results {
		pad := strings.Repeat(" ", width-len(r.name))
		if r.pass {
			passed++
			fmt.Printf("  %s %s%s  %s\n", green("✓"), r.name, pad, dim(dur(r.took)))
			continue
		}
		failed++
		fmt.Printf("  %s %s%s  %s  %s\n", red("✗"), bold(r.name), pad, red(r.reason), dim(dur(r.took)))
	}

	tally := green(fmt.Sprintf("%d passed", passed))
	if failed > 0 {
		tally += ", " + red(fmt.Sprintf("%d failed", failed))
	}
	fmt.Printf("\n  %s  %s\n\n", tally, dim(fmt.Sprintf("· %d tests in %s", len(tests), dur(total))))
	if failed > 0 {
		os.Exit(1)
	}
}

// runOneTest instantiates a fresh module per test so memory/globals don't leak
// between cases, then scores it: a trap or an i32 zero return is a failure.
func runOneTest(c *wago.Compiled, name string) testResult {
	in, err := wago.Instantiate(c, autoHosts(c, false))
	if err != nil {
		return testResult{name: name, reason: "instantiate: " + err.Error()}
	}
	defer in.Close()
	t0 := time.Now()
	res, err := in.Invoke(name)
	took := time.Since(t0)
	switch {
	case err != nil:
		return testResult{name: name, reason: "trap: " + trapReason(err), took: took}
	case len(res) > 0 && wago.AsI32(res[0]) == 0:
		return testResult{name: name, reason: "returned 0 (false)", took: took}
	default:
		return testResult{name: name, pass: true, took: took}
	}
}

func isTestName(n string) bool { return strings.HasPrefix(strings.ToLower(n), "test") }

// ---- build / validate ---------------------------------------------------

func buildCmd(args []string) {
	var out string
	pos, err := extractOpts(args, map[string]*string{"-o": &out, "--out": &out})
	if err != nil {
		fatal("build: %v", err)
	}
	if len(pos) < 1 {
		fatal("build: need a <file>")
	}
	src := mustRead(pos[0])
	if wago.IsCompiled(src) {
		fatal("build: %s is already a compiled .wago module", pos[0])
	}
	c, err := wago.Compile(src)
	if err != nil {
		fatal("build: %v", err)
	}
	blob, err := c.MarshalBinary()
	if err != nil {
		fatal("build: %v", err)
	}
	if out == "" {
		out = strings.TrimSuffix(filepath.Base(pos[0]), filepath.Ext(pos[0])) + ".wago"
	}
	if err := os.WriteFile(out, blob, 0o644); err != nil {
		fatal("build: %v", err)
	}
	fmt.Printf("%s %s → %s  %s\n", green("✓"), filepath.Base(pos[0]), out, dim(fmt.Sprintf("(%d bytes)", len(blob))))
}

func validateCmd(args []string) {
	pos, err := extractOpts(args, nil)
	if err != nil {
		fatal("validate: %v", err)
	}
	if len(pos) < 1 {
		fatal("validate: need a <file>")
	}
	src := mustRead(pos[0])
	if !wago.IsCompiled(src) {
		if _, err := wago.Compile(src); err != nil {
			fmt.Printf("%s %s — %v\n", red("✗"), filepath.Base(pos[0]), err)
			os.Exit(1)
		}
	} else if _, err := wago.Load(src); err != nil {
		fmt.Printf("%s %s — %v\n", red("✗"), filepath.Base(pos[0]), err)
		os.Exit(1)
	}
	fmt.Printf("%s %s ok\n", green("✓"), filepath.Base(pos[0]))
}

// ---- loading & imports --------------------------------------------------

func mustRead(file string) []byte {
	src, err := os.ReadFile(file)
	if err != nil {
		fatal("%v", err)
	}
	return src
}

func mustLoad(file string) *wago.Compiled {
	src := mustRead(file)
	var c *wago.Compiled
	var err error
	if wago.IsCompiled(src) {
		c, err = wago.Load(src) // precompiled .wago
	} else {
		c, err = wago.Compile(src)
	}
	if err != nil {
		fatal("%v", err)
	}
	return c
}

func mustResolveExport(c *wago.Compiled, invoke string) string {
	names := c.ExportedFunctions()
	if invoke != "" {
		if _, ok := c.Exports[invoke]; !ok {
			fatal("no exported function %q (have: %s)", invoke, strings.Join(names, ", "))
		}
		return invoke
	}
	for _, name := range []string{"_start", "main"} {
		if _, ok := c.Exports[name]; ok {
			return name
		}
	}
	if len(names) == 1 {
		return names[0]
	}
	fatal("multiple exports; pass -e <name> (have: %s)", strings.Join(names, ", "))
	return ""
}

// autoHosts satisfies every function import with a no-op; when trace is set
// (interactive run) it echoes each host call.
func autoHosts(c *wago.Compiled, trace bool) wago.Imports {
	hosts := wago.Imports{}
	for _, name := range c.Imports {
		n := name
		if trace {
			hosts[n] = wago.HostFunc(func(arg int32) { fmt.Printf("  %s %s(%d)\n", dim("host"), n, arg) })
		} else {
			hosts[n] = wago.HostFunc(func(int32) {})
		}
	}
	return hosts
}

// ---- arg parsing & formatting -------------------------------------------

func mustParseArgs(strs []string, params []wasm.ValType) []uint64 {
	if len(strs) != len(params) {
		fatal("expected %d arg(s), got %d", len(params), len(strs))
	}
	vals := make([]uint64, len(strs))
	for i, s := range strs {
		t := params[i]
		valPart := s
		if idx := strings.LastIndexByte(s, ':'); idx >= 0 {
			valPart = s[:idx]
			switch s[idx+1:] {
			case "i32":
				t = wasm.I32
			case "i64":
				t = wasm.I64
			case "f32":
				t = wasm.F32
			case "f64":
				t = wasm.F64
			default:
				fatal("arg %d: bad type suffix in %q", i, s)
			}
		}
		v, err := parseVal(valPart, t)
		if err != nil {
			fatal("arg %d (%q): %v", i, s, err)
		}
		vals[i] = v
	}
	return vals
}

func parseVal(s string, t wasm.ValType) (uint64, error) {
	switch {
	case wasm.EqualValType(t, wasm.I64):
		if n, err := strconv.ParseInt(s, 0, 64); err == nil {
			return wago.I64(n), nil
		}
		u, err := strconv.ParseUint(s, 0, 64)
		return wago.I64(int64(u)), err
	case wasm.EqualValType(t, wasm.F32):
		f, err := strconv.ParseFloat(s, 32)
		return wago.F32(float32(f)), err
	case wasm.EqualValType(t, wasm.F64):
		f, err := strconv.ParseFloat(s, 64)
		return wago.F64(f), err
	default: // i32
		if n, err := strconv.ParseInt(s, 0, 32); err == nil {
			return wago.I32(int32(n)), nil
		}
		u, err := strconv.ParseUint(s, 0, 32)
		return wago.I32(int32(uint32(u))), err
	}
}

func fmtVal(bits uint64, t wasm.ValType) string {
	switch {
	case wasm.EqualValType(t, wasm.I64):
		return strconv.FormatInt(wago.AsI64(bits), 10)
	case wasm.EqualValType(t, wasm.F32):
		return strconv.FormatFloat(float64(wago.AsF32(bits)), 'g', -1, 32)
	case wasm.EqualValType(t, wasm.F64):
		return strconv.FormatFloat(wago.AsF64(bits), 'g', -1, 64)
	default:
		return strconv.FormatInt(int64(wago.AsI32(bits)), 10)
	}
}

func format(export string, args, res []uint64, paramTypes, resultTypes []wasm.ValType) string {
	as := make([]string, len(args))
	for i, v := range args {
		as[i] = fmtVal(v, paramTypes[i])
	}
	call := fmt.Sprintf("%s(%s)", export, strings.Join(as, ", "))
	if len(res) == 0 {
		return fmt.Sprintf("%s = %s", call, dim("()"))
	}
	rs := make([]string, len(res))
	for i, v := range res {
		rs[i] = fmtVal(v, resultTypes[i])
	}
	return fmt.Sprintf("%s = %s", call, cyan(strings.Join(rs, ", ")))
}

// trapReason renders an Invoke error, preferring the typed trap code.
func trapReason(err error) string {
	var te *wago.TrapError
	if errors.As(err, &te) {
		return te.Code.String()
	}
	return err.Error()
}

// ---- output helpers -----------------------------------------------------

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "%s "+format+"\n", append([]any{red("wago:")}, args...)...)
	os.Exit(1)
}

func dur(d time.Duration) string {
	switch {
	case d < time.Microsecond:
		return fmt.Sprintf("%dns", d.Nanoseconds())
	case d < time.Millisecond:
		return fmt.Sprintf("%.1fµs", float64(d.Nanoseconds())/1e3)
	case d < time.Second:
		return fmt.Sprintf("%.2fms", float64(d.Nanoseconds())/1e6)
	default:
		return fmt.Sprintf("%.2fs", d.Seconds())
	}
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

func bold(s string) string   { return paint("1", s) }
func dim(s string) string    { return paint("2", s) }
func red(s string) string    { return paint("31", s) }
func green(s string) string  { return paint("32", s) }
func yellow(s string) string { return paint("33", s) }
func cyan(s string) string   { return paint("36", s) }

// extractOpts accepts "-x val", "--x val", and "-x=val" forms anywhere.
func extractOpts(args []string, opts map[string]*string) ([]string, error) {
	var pos []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		name, inline, hasInline := a, "", false
		if strings.HasPrefix(a, "-") {
			if eq := strings.IndexByte(a, '='); eq >= 0 {
				name, inline, hasInline = a[:eq], a[eq+1:], true
			}
		}
		dst, ok := opts[name]
		if !ok {
			pos = append(pos, a)
			continue
		}
		if hasInline {
			*dst = inline
		} else if i+1 < len(args) {
			*dst = args[i+1]
			i++
		} else {
			return nil, fmt.Errorf("option %s needs a value", name)
		}
	}
	return pos, nil
}
