//go:build linux && amd64

package wago

// Execution-level conformance against the official WebAssembly spec testsuite
// (vendored at tests/spec, pinned to a pre-reference-types commit so the file
// set represents wasm 1.0 / MVP). The harness converts each .wast to JSON +
// .wasm with wast2json, then drives module / assert_return / assert_trap /
// assert_exhaustion / action commands through wago's compile→instantiate→invoke
// pipeline and scores each file.
//
// It is gated on the submodule being checked out and wast2json on PATH; it skips
// otherwise. Set WAGO_SPECTEST_WRITE=<path> to also write a Markdown scoreboard.

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

// mvpFiles is the wasm 1.0 / MVP core test set. A few are omitted because the
// pinned wabt cannot convert them (elem: named-segment redefinition quirk).
var mvpFiles = []string{
	"address", "align", "block", "br", "br_if", "br_table",
	"call", "call_indirect", "comments", "const", "conversions", "custom",
	"data", "endianness", "exports", "f32", "f32_bitwise", "f32_cmp",
	"f64", "f64_bitwise", "f64_cmp", "fac", "float_exprs", "float_literals",
	"float_memory", "float_misc", "forward", "func", "func_ptrs", "global",
	"i32", "i64", "if", "imports", "inline-module", "int_exprs", "int_literals",
	"labels", "left-to-right", "linking", "load", "local_get", "local_set",
	"local_tee", "loop", "memory", "memory_grow", "memory_redundancy",
	"memory_size", "memory_trap", "names", "nop", "return", "select", "stack",
	"start", "store", "switch", "token", "traps", "type",
	"unreachable", "unreached-invalid", "unwind",
}

type specCmd struct {
	Type     string     `json:"type"`
	Line     int        `json:"line"`
	Filename string     `json:"filename"`
	Name     string     `json:"name"`
	Action   specAction `json:"action"`
	Expected []specVal  `json:"expected"`
	Text     string     `json:"text"`
	ModType  string     `json:"module_type"`
	As       string     `json:"as"`
}

type specAction struct {
	Type   string    `json:"type"`
	Field  string    `json:"field"`
	Args   []specVal `json:"args"`
	Module string    `json:"module"`
}

// As is the (register "as" $mod) name a module's exports are published under, so
// a later module can import them (cross-instance linking).

type specVal struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

type fileScore struct {
	name             string
	pass, fail, skip int
	blocked          bool
	reason           string
}

// specResult is the per-file score, marshaled across the subprocess boundary.
type specResult struct {
	Name    string `json:"name"`
	Pass    int    `json:"pass"`
	Fail    int    `json:"fail"`
	Skip    int    `json:"skip"`
	Blocked bool   `json:"blocked"`
	Reason  string `json:"reason"`
	Status  string `json:"-"` // filled by the parent (adds CRASH/TIMEOUT)
}

func statusOf(r specResult) string {
	switch {
	case r.Reason == "crash" || r.Reason == "timeout":
		return strings.ToUpper(r.Reason)
	case r.Fail > 0:
		return "FAIL"
	case r.Blocked && r.Pass > 0:
		return "PARTIAL" // some assertions passed, but a later module couldn't load
	case r.Blocked:
		return "BLOCKED" // first module never loaded; nothing ran
	case r.Pass > 0:
		return "PASS"
	case r.Skip > 0:
		return "SKIP" // validation-only file (assert_invalid/malformed), no execution
	default:
		return "EMPTY"
	}
}

// applicable reports whether a file actually exercised (or tried to exercise)
// execution, so SKIP/EMPTY files don't dilute the conformance denominator.
func (r specResult) applicable() bool {
	switch statusOf(r) {
	case "SKIP", "EMPTY":
		return false
	default:
		return true
	}
}

// TestSpecExec runs each MVP file in an isolated subprocess (re-exec of this test
// binary with WAGO_SPECFILE set), so a JIT segfault on one file marks that file
// CRASH instead of taking down the whole run.
func TestSpecExec(t *testing.T) {
	if base := os.Getenv("WAGO_SPECFILE"); base != "" {
		wast2json, err := exec.LookPath("wast2json")
		if err != nil {
			b, _ := json.Marshal(specResult{Name: base, Blocked: true, Reason: "wast2json (wabt) not on PATH"})
			fmt.Printf("SPECRESULT\t%s\n", b)
			return
		}
		s := runSpecFile(t, wast2json, "tests/spec", base)
		b, _ := json.Marshal(specResult{Name: s.name, Pass: s.pass, Fail: s.fail, Skip: s.skip, Blocked: s.blocked, Reason: s.reason})
		fmt.Printf("SPECRESULT\t%s\n", b)
		return
	}

	if _, err := os.Stat("tests/spec/i32.wast"); err != nil {
		t.Skip("spec submodule not checked out (git submodule update --init tests/spec)")
	}
	if _, err := exec.LookPath("wast2json"); err != nil {
		t.Skip("wast2json (wabt) not on PATH")
	}

	results := make([]specResult, 0, len(mvpFiles))
	for _, base := range mvpFiles {
		results = append(results, runChild(base))
	}

	sort.Slice(results, func(i, j int) bool { return results[i].Name < results[j].Name })
	var fullPass, applicable, totPass, totFail, totSkip int
	var table strings.Builder
	table.WriteString("| file | status | pass | fail | skip | note |\n|---|---|---:|---:|---:|---|\n")
	for _, r := range results {
		st := statusOf(r)
		if r.applicable() {
			applicable++
		}
		if st == "PASS" {
			fullPass++
		}
		totPass += r.Pass
		totFail += r.Fail
		totSkip += r.Skip
		fmt.Fprintf(&table, "| %s | %s | %d | %d | %d | %s |\n", r.Name, st, r.Pass, r.Fail, r.Skip, r.Reason)
		t.Logf("%-20s %-7s pass=%-4d fail=%-3d skip=%-3d %s", r.Name, st, r.Pass, r.Fail, r.Skip, r.Reason)
	}
	summary := fmt.Sprintf("MVP execution: %d/%d applicable files fully passing | assertions pass=%d fail=%d skip=%d",
		fullPass, applicable, totPass, totFail, totSkip)
	t.Log(summary)

	if out := os.Getenv("WAGO_SPECTEST_WRITE"); out != "" {
		md := "# wasm 1.0 (MVP) spec conformance\n\n" +
			"Generated by `go test -run TestSpecExec` against `tests/spec` (pinned pre-reference-types).\n" +
			"Each file runs in an isolated subprocess; CRASH means the JIT faulted.\n\n" +
			"**" + summary + "**\n\n" + table.String()
		if err := os.WriteFile(out, []byte(md), 0o644); err != nil {
			t.Fatalf("write scoreboard: %v", err)
		}
		t.Logf("wrote scoreboard to %s", out)
	}
}

// runChild re-executes this test binary to score a single file in isolation.
func runChild(base string) specResult {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestSpecExec$")
	cmd.Env = append(os.Environ(), "WAGO_SPECFILE="+base)
	out, _ := cmd.CombinedOutput()
	for _, line := range strings.Split(string(out), "\n") {
		if rest, ok := strings.CutPrefix(line, "SPECRESULT\t"); ok {
			var r specResult
			if json.Unmarshal([]byte(rest), &r) == nil {
				return r
			}
		}
	}
	r := specResult{Name: base, Reason: "crash"}
	if ctx.Err() == context.DeadlineExceeded {
		r.Reason = "timeout"
	}
	return r
}

func runSpecFile(t *testing.T, wast2json, dir, base string) (score fileScore) {
	score.name = base
	defer func() {
		if r := recover(); r != nil {
			score.reason = fmt.Sprintf("panic: %v", r)
			score.fail++
		}
	}()

	wast := filepath.Join(dir, base+".wast")
	if _, err := os.Stat(wast); err != nil {
		score.blocked = true
		score.reason = "missing .wast"
		return
	}
	tmp := t.TempDir()
	jsonPath := filepath.Join(tmp, base+".json")
	if out, err := exec.Command(wast2json, "--enable-all", wast, "-o", jsonPath).CombinedOutput(); err != nil {
		score.blocked = true
		score.reason = "wast2json failed"
		t.Logf("%s: wast2json failed: %s", base, firstLine(out))
		return
	}
	raw, err := os.ReadFile(jsonPath)
	if err != nil {
		score.reason = err.Error()
		return
	}
	var sf struct {
		Commands []specCmd `json:"commands"`
	}
	if err := json.Unmarshal(raw, &sf); err != nil {
		score.reason = err.Error()
		return
	}

	st := &specState{tmp: tmp, named: map[string]*Instance{}, registered: map[string]*Instance{}}
	defer st.closeAll()

	for _, c := range sf.Commands {
		switch c.Type {
		case "module":
			in, err := st.instantiate(c.Filename)
			if err != nil {
				st.cur = nil
				score.blocked = true
				if score.reason == "" {
					score.reason = "module: " + condense(err.Error())
				}
				continue
			}
			st.cur = in
			if c.Name != "" {
				st.named[c.Name] = in
			}
		case "assert_return":
			ok, skip, why := st.assertReturn(c)
			tally(&score, ok, skip, why)
		case "assert_trap", "assert_exhaustion":
			ok, skip, why := st.assertTrap(c)
			tally(&score, ok, skip, why)
		case "assert_uninstantiable":
			// The module must fail to instantiate (e.g. a trapping start), but its
			// data/element writes into shared imported memory/table persist. instantiate
			// retains the compiled module so a funcref it stored in a shared table stays
			// backed by mapped code.
			if _, err := st.instantiate(c.Filename); err == nil {
				score.fail++
				if score.reason == "" {
					score.reason = fmt.Sprintf("line %d: expected uninstantiable module to fail", c.Line)
				}
			}
		case "action":
			if _, _, err := st.doAction(c.Action); err != nil {
				// A bare action that errors is only a problem if the module was live.
				if st.cur != nil {
					score.fail++
					score.reason = "action: " + condense(err.Error())
				}
			}
		case "register":
			// Publish a module's exports under c.As so later modules can import
			// them (cross-instance linking). c.Name selects the module; empty means
			// the current one.
			in := st.cur
			if c.Name != "" {
				in = st.named[c.Name]
			}
			if in != nil && c.As != "" {
				st.registered[c.As] = in
			}
		default:
			// assert_invalid / assert_malformed / assert_unlinkable /
			// assert_uninstantiable are covered by the wasm-package validation
			// harness; the execution harness skips them.
			score.skip++
		}
	}
	return
}

func tally(s *fileScore, ok, skip bool, why string) {
	switch {
	case skip:
		s.skip++
	case ok:
		s.pass++
	default:
		s.fail++
		if s.reason == "" {
			s.reason = why
		}
	}
}

type specState struct {
	tmp        string
	cur        *Instance
	named      map[string]*Instance
	registered map[string]*Instance // (register "as") name -> instance, for cross-instance imports
	all        []*Instance
	mems       []*Memory   // host-provided memories (e.g. spectest.memory), closed with the state
	compiled   []*Compiled // retained so funcref code (incl. from uninstantiable modules) stays mapped
}

func (st *specState) closeAll() {
	for _, in := range st.all {
		in.Close()
	}
	for _, m := range st.mems {
		m.Close()
	}
}

func (st *specState) instantiate(filename string) (*Instance, error) {
	data, err := os.ReadFile(filepath.Join(st.tmp, filename))
	if err != nil {
		return nil, err
	}
	c, err := Compile(data)
	if err != nil {
		return nil, err
	}
	// Retain the compiled module so any funcref it writes into a shared table stays
	// backed by mapped code — even if the module itself fails to instantiate.
	st.compiled = append(st.compiled, c)
	// Satisfy imports best-effort: a no-op host for every function import and a
	// spectest-style value for every global import. Cross-module memory/table
	// imports are unsupported and will surface as an instantiate error.
	// Function imports come from the standard "spectest" host module (no-op host
	// funcs) or from a (register ...)'d instance (cross-instance linking). Anything
	// else is unresolvable, so the module is reported blocked.
	imports := Imports{}
	for _, key := range c.Imports {
		mod, field, _ := strings.Cut(key, ".")
		switch {
		case mod == "spectest":
			imports[key] = HostFunc(func(int32) {})
		case st.registered[mod] != nil:
			ex, err := st.registered[mod].ExportedFunc(field)
			if err != nil {
				return nil, fmt.Errorf("cross-instance function import %q: %w", key, err)
			}
			imports[key] = ex
		default:
			return nil, fmt.Errorf("cross-instance linking unsupported: function import %q", key)
		}
	}
	for _, gi := range c.GlobalImports {
		key := gi.Module + "." + gi.Name
		switch {
		case gi.Module == "spectest":
			imports[key] = GlobalImport{Type: gi.Type, Mutable: gi.Mutable, Bits: spectestGlobalBits(gi.Type)}
		case st.registered[gi.Module] != nil:
			g, err := st.registered[gi.Module].ExportedGlobalObject(gi.Name)
			if err != nil {
				return nil, fmt.Errorf("cross-instance global import %q: %w", key, err)
			}
			imports[key] = g
		default:
			return nil, fmt.Errorf("cross-instance linking unsupported: global import %q", key)
		}
	}
	// A memory import comes from spectest (a fresh host memory) or from a
	// (register ...)'d instance (cross-instance shared memory).
	if key, ok := c.MemoryImport(); ok {
		mod, field, _ := strings.Cut(key, ".")
		switch {
		case mod == "spectest":
			mem, err := NewMemory(1, 2) // the testsuite's standard spectest.memory
			if err != nil {
				return nil, err
			}
			imports[key] = mem
			st.mems = append(st.mems, mem)
		case st.registered[mod] != nil:
			mem, err := st.registered[mod].ExportedMemory(field)
			if err != nil {
				return nil, fmt.Errorf("cross-instance memory import %q: %w", key, err)
			}
			imports[key] = mem // owned by the registered instance; not tracked in st.mems
		default:
			return nil, fmt.Errorf("cross-instance linking unsupported: memory import %q", key)
		}
	}
	// A table import comes from a (register ...)'d instance (cross-instance shared
	// table). spectest.table is not provided, so such modules stay blocked.
	if key, ok := c.TableImport(); ok {
		mod, field, _ := strings.Cut(key, ".")
		if st.registered[mod] == nil {
			return nil, fmt.Errorf("cross-instance linking unsupported: table import %q", key)
		}
		tbl, err := st.registered[mod].ExportedTable(field)
		if err != nil {
			return nil, fmt.Errorf("cross-instance table import %q: %w", key, err)
		}
		imports[key] = tbl
	}
	in, err := Instantiate(c, imports)
	if err != nil {
		return nil, err
	}
	st.all = append(st.all, in)
	return in, nil
}

func (st *specState) target(module string) *Instance {
	if module != "" {
		return st.named[module]
	}
	return st.cur
}

func (st *specState) doAction(a specAction) ([]uint64, bool, error) {
	in := st.target(a.Module)
	if in == nil {
		return nil, true, nil // blocked: no live module
	}
	switch a.Type {
	case "invoke":
		args := make([]uint64, len(a.Args))
		for i, av := range a.Args {
			v, unsup := toValue(av)
			if unsup {
				return nil, true, nil
			}
			args[i] = v
		}
		res, err := in.Invoke(a.Field, args...)
		return res, false, err
	case "get":
		v, err := in.Global(a.Field)
		if err != nil {
			return nil, false, err
		}
		return []uint64{v}, false, nil
	default:
		return nil, true, nil
	}
}

func (st *specState) assertReturn(c specCmd) (ok, skip bool, why string) {
	res, skip, err := st.doAction(c.Action)
	if skip {
		return false, true, ""
	}
	if err != nil {
		return false, false, "unexpected error: " + condense(err.Error())
	}
	if len(res) != len(c.Expected) {
		return false, false, fmt.Sprintf("result count %d != expected %d", len(res), len(c.Expected))
	}
	for i, exp := range c.Expected {
		match, unsup := valueMatches(res[i], exp)
		if unsup {
			return false, true, ""
		}
		if !match {
			return false, false, fmt.Sprintf("line %d: got %s, want %s/%s", c.Line, fmtVal(res[i]), exp.Type, exp.Value)
		}
	}
	return true, false, ""
}

func (st *specState) assertTrap(c specCmd) (ok, skip bool, why string) {
	_, skip, err := st.doAction(c.Action)
	if skip {
		return false, true, ""
	}
	if err == nil {
		return false, false, fmt.Sprintf("line %d: expected trap %q, returned normally", c.Line, c.Text)
	}

	msg := strings.TrimPrefix(err.Error(), "wasm trap: ")
	want := c.Text
	matches := false
	switch want {
	case "unreachable":
		matches = strings.Contains(msg, "unreachable")
	case "builtin trap":
		matches = strings.Contains(msg, "builtin") && strings.Contains(msg, "trap")
	case "runtime interrupt request":
		matches = strings.Contains(msg, "interrupt")
	case "out of bounds memory access", "out of bounds linear memory access":
		matches = strings.Contains(msg, "linear memory") && strings.Contains(msg, "out of bounds")
	case "indirect call type mismatch":
		matches = strings.Contains(msg, "indirect call") && strings.Contains(msg, "wrong signature")
	case "undefined element", "undefined", "uninitialized element", "uninitialized":
		// wago reports both a null table entry (spec: "uninitialized") and an
		// out-of-range table index (spec: "undefined") as one indirect-call
		// out-of-bounds trap.
		matches = strings.Contains(msg, "indirect call") && strings.Contains(msg, "out of bounds")
	case "integer overflow":
		// wasm names both integer-division overflow and out-of-range float→int
		// truncation "integer overflow"; wago reports them distinctly.
		matches = strings.Contains(msg, "division overflow") || strings.Contains(msg, "conversion overflow")
	case "integer divide by zero":
		matches = strings.Contains(msg, "division by zero")
	case "invalid conversion to integer":
		matches = strings.Contains(msg, "conversion") && strings.Contains(msg, "overflow")
	case "unknown import", "called function not linked", "indirect call not linked":
		matches = strings.Contains(msg, "not linked")
	case "call stack exhausted":
		matches = strings.Contains(msg, "stack fence")
	default:
		matches = strings.Contains(msg, want)
	}
	if !matches {
		return false, false, fmt.Sprintf("line %d: expected trap %q, got %q", c.Line, want, msg)
	}
	return true, false, ""
}

// toValue converts a spec arg to a wago Value; unsupported types (v128/ref)
// report unsup=true so the caller can skip.
func toValue(sv specVal) (v uint64, unsup bool) {
	switch sv.Type {
	case "i32":
		u, err := strconv.ParseUint(sv.Value, 10, 64)
		if err != nil {
			return 0, true
		}
		return I32(int32(uint32(u))), false
	case "i64":
		u, err := strconv.ParseUint(sv.Value, 10, 64)
		if err != nil {
			return 0, true
		}
		return I64(int64(u)), false
	case "f32":
		bits, ok := floatBits(sv.Value, 32)
		if !ok {
			return 0, true
		}
		return F32(math.Float32frombits(uint32(bits))), false
	case "f64":
		bits, ok := floatBits(sv.Value, 64)
		if !ok {
			return 0, true
		}
		return F64(math.Float64frombits(bits)), false
	default:
		return 0, true
	}
}

// valueMatches compares an actual result against an expected spec value,
// including canonical/arithmetic NaN handling. unsup=true for ref/v128 types.
func valueMatches(got uint64, exp specVal) (match, unsup bool) {
	switch exp.Type {
	case "i32":
		want, err := strconv.ParseUint(exp.Value, 10, 64)
		if err != nil {
			return false, true
		}
		return uint32(got) == uint32(want), false
	case "i64":
		want, err := strconv.ParseUint(exp.Value, 10, 64)
		if err != nil {
			return false, true
		}
		return got == want, false
	case "f32":
		return floatMatches(uint64(uint32(got)), exp.Value, 32), false
	case "f64":
		return floatMatches(got, exp.Value, 64), false
	default:
		return false, true
	}
}

func floatBits(s string, width int) (uint64, bool) {
	switch s {
	case "nan:canonical", "nan:arithmetic":
		return 0, false // only valid as an expected result, not an argument
	}
	u, err := strconv.ParseUint(s, 10, width)
	if err != nil {
		return 0, false
	}
	return u, true
}

func floatMatches(got uint64, want string, width int) bool {
	var quiet, signMask uint64
	if width == 32 {
		quiet, signMask = 0x7fc00000, 0x7fffffff
		got &= 0xffffffff
	} else {
		quiet, signMask = 0x7ff8000000000000, 0x7fffffffffffffff
	}
	switch want {
	case "nan:canonical":
		return got&signMask == quiet
	case "nan:arithmetic":
		return got&quiet == quiet // quiet bit (and exponent) set => some NaN
	}
	w, err := strconv.ParseUint(want, 10, width)
	if err != nil {
		return false
	}
	return got == w
}

func spectestGlobalBits(t ValType) uint64 {
	switch t {
	case ValF32:
		return uint64(math.Float32bits(666.6))
	case ValF64:
		return math.Float64bits(666.6)
	default:
		return 666
	}
}

func fmtVal(v uint64) string { return fmt.Sprintf("%#x", v) }

func firstLine(b []byte) string { return condense(string(b)) }

func condense(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > 120 {
		s = s[:120]
	}
	return strings.TrimSpace(s)
}
