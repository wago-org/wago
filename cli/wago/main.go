// Command wago runs, compiles, profiles, and validates wasm modules.
package main

import (
	"fmt"
	"os"
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
	case "compile":
		compileCmd(a[1:])
	case "profile":
		profileCmd(a[1:])
	case "validate":
		validateCmd(a[1:])
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
	fmt.Fprint(w, `wago — a pure-Go (no cgo) WebAssembly engine

usage (options may appear before or after the <file>):
  wago <file> [args...]                      run (alias)
  wago run [-e name] <file> [args...]        JIT-compile and execute
      -e, --invoke <name>    export to call (default: _start, main, or sole export)
  wago compile [opts] <file>                 AOT-compile to a precompiled .wago module
      -o, --output <path>    output (default <file>.wago)
      --target <triple>      target (default linux/amd64)
      --emit <module|asm>    module = loadable blob (default), asm = x86-64 hex
  wago profile [-e name] [--runs N] <file> [args...]   timings + codegen stats
  wago validate <file>                       decode + validate only
  wago version

a <file> may be raw .wasm or a precompiled .wago. args are typed by the
function signature; override with a suffix:  42   7:i64   3.5:f64   1.5:f32
`)
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "wago: "+format+"\n", args...)
	os.Exit(1)
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

func compileCmd(args []string) {
	var out, target, emit string
	pos, err := extractOpts(args, map[string]*string{
		"-o": &out, "--output": &out, "--target": &target, "--emit": &emit,
	})
	if err != nil {
		fatal("compile: %v", err)
	}
	if target == "" {
		target = "linux/amd64"
	}
	if emit == "" {
		emit = "module"
	}
	if len(pos) < 1 {
		fatal("compile: need a <file>")
	}
	file := pos[0]
	if target != "linux/amd64" {
		fatal("unsupported target %q (only linux/amd64)", target)
	}
	src, err := os.ReadFile(file)
	if err != nil {
		fatal("%v", err)
	}
	if wago.IsCompiled(src) {
		fatal("%s is already a precompiled wago module", file)
	}
	c, err := wago.Compile(src)
	if err != nil {
		fatal("%v", err)
	}

	switch emit {
	case "module":
		if out == "" {
			out = strings.TrimSuffix(file, ".wasm") + ".wago"
		}
		data, err := c.MarshalBinary()
		if err != nil {
			fatal("%v", err)
		}
		if err := os.WriteFile(out, data, 0o644); err != nil {
			fatal("%v", err)
		}
		fmt.Printf("wrote %s (precompiled, %d funcs, %d B code)\n", out, len(c.Funcs), len(c.Code))
	case "asm":
		emitAsm(c, out)
	default:
		fatal("--emit must be 'module' or 'asm'")
	}
}

func emitAsm(c *wago.Compiled, out string) {
	w := os.Stdout
	if out != "" {
		f, err := os.Create(out)
		if err != nil {
			fatal("%v", err)
		}
		defer f.Close()
		w = f
	}
	names := map[int]string{}
	for n, gfi := range c.Exports {
		names[gfi-c.NumImports] = n
	}
	for li := range c.Entry {
		start := c.Entry[li]
		end := len(c.Code)
		if li+1 < len(c.Entry) && c.Entry[li+1] > start {
			end = c.Entry[li+1]
		}
		name := names[li]
		if name == "" {
			name = fmt.Sprintf("func%d", li)
		}
		fmt.Fprintf(w, "%s: (offset 0x%x, %d bytes)\n", name, start, end-start)
		code := c.Code[start:end]
		for i := 0; i < len(code); i += 16 {
			j := i + 16
			if j > len(code) {
				j = len(code)
			}
			fmt.Fprintf(w, "  %06x  % x\n", start+i, code[i:j])
		}
		fmt.Fprintln(w)
	}
}

func profileCmd(args []string) {
	var invoke, runsStr string
	pos, err := extractOpts(args, map[string]*string{
		"-e": &invoke, "--invoke": &invoke, "--runs": &runsStr,
	})
	if err != nil {
		fatal("profile: %v", err)
	}
	runs := 1000
	if runsStr != "" {
		if n, e := strconv.Atoi(runsStr); e == nil && n > 0 {
			runs = n
		} else {
			fatal("profile: bad --runs %q", runsStr)
		}
	}
	if len(pos) < 1 {
		fatal("profile: need a <file>")
	}
	file := pos[0]
	src, err := os.ReadFile(file)
	if err != nil {
		fatal("%v", err)
	}
	if wago.IsCompiled(src) {
		fatal("profile needs raw .wasm (it times decode/validate/compile)")
	}
	c, tm, err := wago.CompileTimed(src)
	if err != nil {
		fatal("%v", err)
	}
	export := mustResolveExport(c, invoke)
	params, _, _ := c.Signature(export)
	vals := mustParseArgs(pos[1:], params)

	in, err := wago.Instantiate(c, autoHosts(c, false))
	if err != nil {
		fatal("%v", err)
	}
	defer in.Close()
	best := time.Duration(1) << 62
	for i := 0; i < runs; i++ {
		t0 := time.Now()
		if _, err := in.Invoke(export, vals...); err != nil {
			fatal("%v", err)
		}
		if d := time.Since(t0); d < best {
			best = d
		}
	}

	big, bigIdx := 0, 0
	for li := range c.Entry {
		end := len(c.Code)
		if li+1 < len(c.Entry) && c.Entry[li+1] > c.Entry[li] {
			end = c.Entry[li+1]
		}
		if sz := end - c.Entry[li]; sz > big {
			big, bigIdx = sz, li
		}
	}
	fmt.Printf("module:    %s\n", file)
	fmt.Printf("decode:    %v\n", tm.Decode)
	fmt.Printf("validate:  %v\n", tm.Validate)
	fmt.Printf("compile:   %v   (%d B code, %d funcs)\n", tm.Compile, len(c.Code), len(c.Funcs))
	fmt.Printf("exec:      %v/call   (min of %d, invoking %s)\n", best, runs, export)
	fmt.Printf("largest:   func%d  (%d B)\n", bigIdx, big)
}

func validateCmd(args []string) {
	if len(args) < 1 {
		fatal("validate: need a <file>")
	}
	src, err := os.ReadFile(args[0])
	if err != nil {
		fatal("%v", err)
	}
	m, err := wasm.Decode(src)
	if err != nil {
		fatal("invalid: %v", err)
	}
	if err := wasm.Validate(m); err != nil {
		fatal("invalid: %v", err)
	}
	fmt.Printf("%s: OK (%d funcs, %d exports)\n", args[0], len(m.Functions), len(m.Exports))
}

func mustLoad(file string) *wago.Compiled {
	src, err := os.ReadFile(file)
	if err != nil {
		fatal("%v", err)
	}
	c, err := wago.Load(src)
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
	switch t {
	case wasm.I64:
		if n, err := strconv.ParseInt(s, 0, 64); err == nil {
			return wago.I64(n), nil
		}
		u, err := strconv.ParseUint(s, 0, 64)
		return wago.I64(int64(u)), err
	case wasm.F32:
		f, err := strconv.ParseFloat(s, 32)
		return wago.F32(float32(f)), err
	case wasm.F64:
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
