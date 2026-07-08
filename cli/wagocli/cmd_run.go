package wagocli

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/wago-org/wago"
)

// runCommand is `wago run <file> [args...]`: compile and execute an export. It's
// PassThrough so everything after the .wasm file is handed to the guest verbatim.
func runCommand() *Cmd {
	return &Cmd{
		Name:        "run",
		Summary:     "compile and execute an export   (default)",
		Args:        "<file> [args...]",
		PassThrough: true,
		Flags: []Flag{
			{Name: "invoke", Short: "e", Arg: "<name>", Help: "exported function to call"},
			{Name: "plugin", Arg: "<names>", Help: "comma-separated plugins to enable (see: wago plugin list)"},
			{Name: "bounds", Arg: "<mode>", Help: "bounds checks: defer (default) | all"},
		},
		Long: "<file> is raw .wasm or a precompiled .wago. Args after the file are typed by the\n" +
			"signature; override per-arg with a suffix:  42   7:i64   3.5:f64",
		Run: runExec,
	}
}

func runExec(c *Ctx) {
	// Seamlessly hand off to the custom binary that has this project's plugins
	// compiled in (building it once, then cached). No-op without a manifest.
	maybeReexecForPlugins()

	invoke, bounds, plugins := c.Str("invoke"), c.Str("bounds"), c.Str("plugin")
	pos := c.Args
	if len(pos) < 1 {
		fatal("run: need a <file>")
	}
	// Publish the guest argv so host-import plugins (e.g. WASI) can read it.
	wago.SetGuestArgs(pos)
	cfg := wago.NewRuntimeConfig()
	switch bounds {
	case "", "defer": // default: skip a bounds check a prior one already proved safe
	case "all": // bounds-check every access
		cfg = cfg.WithDeferBoundsChecks(false)
	default:
		fatal("run: unknown --bounds %q (want: defer, all)", bounds)
	}
	comp := mustLoad(pos[0], cfg)
	export := mustResolveExport(comp, invoke)

	// Program mode: a _start entry point is a command (e.g. a WASI program). Wire
	// the positional args as guest argv, run _start, and surface proc_exit as the
	// process exit code. Enable a compiled-in plugin with `--plugin <name>` (e.g.
	// --plugin wasi, once WASI is in your wago.json deps and built in).
	if export == "_start" {
		imports := autoHosts(comp, false)
		for k, v := range pluginImports(plugins) {
			imports[k] = v
		}
		in, err := wago.Instantiate(comp, wago.InstantiateOptions{Imports: imports})
		if err != nil {
			fatal("%v", err)
		}
		defer in.Close()
		if _, err := in.Invoke("_start"); err != nil {
			var ex *wago.ExitError
			if errors.As(err, &ex) {
				in.Close()
				os.Exit(int(ex.Code))
			}
			fatal("%s %s", red("trap:"), trapReason(err))
		}
		return
	}

	// Value mode: a normal exported function, with parsed args and a printed result.
	params, results, _ := comp.Signature(export)
	vals := mustParseArgs(pos[1:], params)
	imports := autoHosts(comp, true)
	for k, v := range pluginImports(plugins) {
		imports[k] = v
	}
	in, err := wago.Instantiate(comp, wago.InstantiateOptions{Imports: imports})
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
		c, err = wago.Compile(cfg, src)
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
			hosts[n] = wago.HostFunc(func(_ wago.HostModule, params, _ []uint64) {
				var arg int32
				if len(params) > 0 {
					arg = wago.AsI32(params[0])
				}
				fmt.Printf("  %s %s(%d)\n", dim("host"), n, arg)
			})
		} else {
			hosts[n] = wago.HostFunc(func(wago.HostModule, []uint64, []uint64) {})
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
