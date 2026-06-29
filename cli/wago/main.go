// Command wago runs wasm modules.
package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"

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
	case "compile":
		notImplemented("compile")
	case "profile":
		notImplemented("profile")
	case "validate":
		notImplemented("validate")
	case "version", "--version", "-v":
		fmt.Printf("wago %s (linux/amd64)\n", version)
		fmt.Println("wasm: i32 i64 f32 f64, control flow, memory, calls + host imports")
	case "help", "-h", "--help":
		usage(os.Stdout)
	default:
		runCmd(a)
	}
}

func usage(w *os.File) {
	fmt.Fprint(w, `wago - a pure-Go (no cgo) WebAssembly engine

usage (options may appear before or after the <file>):
  wago <file> [args...]                      run (alias)
  wago run [-e name] <file> [args...]        JIT-compile and execute
      -e, --invoke <name>    export to call (default: _start, main, or sole export)
  wago compile                               not implemented
  wago profile                               not implemented
  wago validate                              not implemented
  wago version

a <file> must be raw .wasm. args are typed by the function signature; override
with a suffix:  42   7:i64   3.5:f64   1.5:f32
`)
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "wago: "+format+"\n", args...)
	os.Exit(1)
}

func notImplemented(cmd string) {
	fatal("%s: not implemented", cmd)
}

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
	params, _, _ := c.Signature(export)
	vals := mustParseArgs(pos[1:], params)

	in, err := wago.Instantiate(c, autoHosts(c, true))
	if err != nil {
		fatal("%v", err)
	}
	defer in.Close()
	res, err := in.Invoke(export, vals...)
	if err != nil {
		fatal("%v", err)
	}
	fmt.Println(format(export, vals, res))
}

func mustLoad(file string) *wago.Compiled {
	src, err := os.ReadFile(file)
	if err != nil {
		fatal("%v", err)
	}
	if wago.IsCompiled(src) {
		fatal("precompiled .wago modules are not implemented")
	}
	c, err := wago.Compile(src)
	if err != nil {
		fatal("%v", err)
	}
	return c
}

func mustResolveExport(c *wago.Compiled, invoke string) string {
	if invoke != "" {
		if _, ok := c.Exports[invoke]; !ok {
			fatal("no exported function %q (have: %s)", invoke, strings.Join(exportNames(c), ", "))
		}
		return invoke
	}
	for _, name := range []string{"_start", "main"} {
		if _, ok := c.Exports[name]; ok {
			return name
		}
	}
	if len(c.Exports) == 1 {
		for n := range c.Exports {
			return n
		}
	}
	fatal("multiple exports; pass --invoke <name> (have: %s)", strings.Join(exportNames(c), ", "))
	return ""
}

func exportNames(c *wago.Compiled) []string {
	ns := make([]string, 0, len(c.Exports))
	for n := range c.Exports {
		ns = append(ns, n)
	}
	return ns
}

func mustParseArgs(strs []string, params []wasm.ValType) []wago.Value {
	if len(strs) != len(params) {
		fatal("expected %d arg(s), got %d", len(params), len(strs))
	}
	vals := make([]wago.Value, len(strs))
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

func parseVal(s string, t wasm.ValType) (wago.Value, error) {
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

func autoHosts(c *wago.Compiled, print bool) map[string]wago.HostFunc {
	hosts := map[string]wago.HostFunc{}
	for _, name := range c.Imports {
		n := name
		if print {
			hosts[n] = func(arg int32) { fmt.Printf("  %s(%d)\n", n, arg) }
		} else {
			hosts[n] = func(int32) {}
		}
	}
	return hosts
}

func format(export string, args, res []wago.Value) string {
	as := make([]string, len(args))
	for i, v := range args {
		as[i] = v.String()
	}
	if len(res) == 0 {
		return fmt.Sprintf("%s(%s) = ()", export, strings.Join(as, ", "))
	}
	rs := make([]string, len(res))
	for i, v := range res {
		rs[i] = v.String()
	}
	return fmt.Sprintf("%s(%s) = %s", export, strings.Join(as, ", "), strings.Join(rs, ", "))
}
