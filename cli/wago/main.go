// Command wago runs WebAssembly modules.
package main

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/wago-org/wago"
	"github.com/wago-org/wago/src/core/compiler/wasm"
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

func main() {
	a := os.Args[1:]
	if len(a) == 0 {
		usage(os.Stderr)
		os.Exit(2)
	}
	switch a[0] {
	case "run":
		runCmd(a[1:])
	case "build":
		notImplemented("build")
	case "validate":
		validateCmd(a[1:])
	case "version", "--version", "-v":
		versionCmd()
	case "help", "-h", "--help":
		usage(os.Stdout)
	default:
		runCmd(a) // `wago <file> [args...]` defaults to run
	}
}

func usage(w *os.File) {
	fmt.Fprintf(w, `%s — a pure-Go (no cgo) WebAssembly engine

%s wago [run] <file> [args...]

%s
  run <file> [args...]      compile and execute an export   (default)
  build                     not implemented
  validate <file>           decode and validate a module
  version                   print version and supported features

%s
  -e, --invoke <name>       export to call
      --bounds <mode>       bounds checks: defer (skip provably-redundant; default) | all

%s
  wago add.wasm 2 3
  wago run -e fib fib.wasm 30

For run, <file> is raw .wasm or a precompiled .wago. run args are typed by
the signature; override per-arg with a suffix:  42   7:i64   3.5:f64
`, bold("wago"), bold("Usage:"), bold("Commands:"), bold("Flags:"), bold("Examples:"))
}

func versionCmd() {
	fmt.Printf("%s %s (linux/amd64)\n", bold("wago"), versionString())
	fmt.Printf("%s %s\n", dim("features:"), wago.SupportedFeatures())
	if wago.GuardPageSupported() {
		fmt.Printf("%s signals-based bounds checks available\n", dim("guard-page:"))
	}
}

func notImplemented(cmd string) { fatal("%s: not implemented", cmd) }

// ---- validate -----------------------------------------------------------

func validateCmd(args []string) {
	file := singleFileArg("validate", args)
	src, err := os.ReadFile(file)
	if err != nil {
		fatal("%v", err)
	}
	if err := validateModuleBytes(src); err != nil {
		fatal("validate: %v", err)
	}
}

func singleFileArg(cmd string, args []string) string {
	if len(args) != 1 {
		fatal("%s: need exactly one <file>", cmd)
	}
	return args[0]
}

func validateModuleBytes(src []byte) error {
	m, err := wasm.DecodeModule(src)
	if err != nil {
		return fmt.Errorf("decode: %w", err)
	}
	if err := wasm.ValidateModule(m); err != nil {
		return fmt.Errorf("validate: %w", err)
	}
	return nil
}

// ---- run ----------------------------------------------------------------

func runCmd(args []string) {
	var invoke, bounds string
	pos, err := extractOpts(args, map[string]*string{
		"-e": &invoke, "--invoke": &invoke, "--bounds": &bounds,
	})
	if err != nil {
		fatal("run: %v", err)
	}
	if len(pos) < 1 {
		fatal("run: need a <file>")
	}
	cfg := wago.NewRuntimeConfig()
	switch bounds {
	case "", "defer": // default: skip a bounds check a prior one already proved safe
	case "all": // bounds-check every access
		cfg = cfg.WithDeferBoundsChecks(false)
	default:
		fatal("run: unknown --bounds %q (want: defer, all)", bounds)
	}
	c := mustLoad(pos[0], cfg)
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

// ---- loading & imports --------------------------------------------------

func mustLoad(file string, cfg *wago.RuntimeConfig) *wago.Compiled {
	src, err := os.ReadFile(file)
	if err != nil {
		fatal("%v", err)
	}
	var c *wago.Compiled
	if wago.IsCompiled(src) {
		c, err = wago.Load(src) // precompiled .wago — codegen options baked in already
	} else {
		c, err = wago.CompileWithConfig(cfg, src)
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

// autoHosts satisfies every function import with a host that echoes the call.
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

func mustParseArgs(strs []string, params []wago.ValType) []uint64 {
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
				t = wago.ValI32
			case "i64":
				t = wago.ValI64
			case "f32":
				t = wago.ValF32
			case "f64":
				t = wago.ValF64
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

func parseVal(s string, t wago.ValType) (uint64, error) {
	switch t {
	case wago.ValI64:
		if n, err := strconv.ParseInt(s, 0, 64); err == nil {
			return wago.I64(n), nil
		}
		u, err := strconv.ParseUint(s, 0, 64)
		return wago.I64(int64(u)), err
	case wago.ValF32:
		f, err := strconv.ParseFloat(s, 32)
		return wago.F32(float32(f)), err
	case wago.ValF64:
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

func fmtVal(bits uint64, t wago.ValType) string {
	switch t {
	case wago.ValI64:
		return strconv.FormatInt(wago.AsI64(bits), 10)
	case wago.ValF32:
		return strconv.FormatFloat(float64(wago.AsF32(bits)), 'g', -1, 32)
	case wago.ValF64:
		return strconv.FormatFloat(wago.AsF64(bits), 'g', -1, 64)
	default:
		return strconv.FormatInt(int64(wago.AsI32(bits)), 10)
	}
}

func format(export string, args, res []uint64, paramTypes, resultTypes []wago.ValType) string {
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

// extractOpts accepts "-x val", "--x val", and "-x=val" value forms anywhere in
// args; everything else is returned as positional.
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
