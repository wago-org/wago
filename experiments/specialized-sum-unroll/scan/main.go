// This research tool decodes instruction boundaries and structured loop ranges.
// Its broad reduction screen produces hypotheses for manual inspection, not
// proofs of recurrence, reachability, trip count, or compiler admission.
package main

import (
	"crypto/sha256"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

type token struct {
	PC       int    `json:"pc"`
	Op       byte   `json:"op"`
	Index    uint32 `json:"index,omitempty"`
	Constant int64  `json:"constant,omitempty"`
	Memory   uint32 `json:"memory,omitempty"`
	Offset   uint64 `json:"offset,omitempty"`
}
type candidate struct {
	Function        int    `json:"function"` // absolute Wasm function index
	Name            string `json:"name,omitempty"`
	LoopPC, WritePC int
	Accumulator     uint32
	Type            string
	Reasons         []string
	Instructions    []token
}
type result struct {
	Path, SHA256, Error                                                                       string
	Bytes, Functions, Loops, I64Loads, Exact, BodyMatches, IntegerCandidates, FloatCandidates int
	Candidates                                                                                []candidate
}
type loop struct{ start, end int }
type value struct {
	locals      map[uint32]bool
	memory, add bool
}

func merge(a, b value, add bool) value {
	v := value{locals: map[uint32]bool{}, memory: a.memory || b.memory, add: a.add || b.add || add}
	for x := range a.locals {
		v.locals[x] = true
	}
	for x := range b.locals {
		v.locals[x] = true
	}
	return v
}
func decode(m *wasm.Module, body []byte) ([]token, []loop, error) {
	r := wasm.ReaderFrom(body)
	classifier := wasm.NewModuleInstructionClassifier(m, true)
	var ts []token
	var stack []int
	var loops []loop
	for r.HasNext() {
		pc := r.Offset()
		op, e := r.Byte()
		if e != nil {
			return nil, nil, e
		}
		copy := r
		var imm wasm.InstructionImmediate
		if e = classifier.ClassifyInto(&r, op, &imm); e != nil {
			return nil, nil, fmt.Errorf("instruction pc=%d op=%02x: %w", pc, op, e)
		}
		t := token{PC: pc, Op: op, Index: imm.Index, Memory: imm.MemIndex, Offset: imm.MemOffset}
		if op == 0x41 {
			n, _ := copy.I32()
			t.Constant = int64(n)
		}
		if op == 0x42 {
			t.Constant, _ = copy.I64()
		}
		ts = append(ts, t)
		if op == 2 || op == 3 || op == 4 || op == 6 || op == 0x1f {
			stack = append(stack, len(ts)-1)
		}
		if op == 0x0b && len(stack) > 0 {
			i := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if ts[i].Op == 3 {
				loops = append(loops, loop{i, len(ts) - 1})
			}
		}
	}
	if len(stack) != 0 {
		return nil, nil, fmt.Errorf("unclosed control frames")
	}
	return ts, loops, nil
}

// Exact mirrors inspectLinearSumLoop and its top-tested zero-counter entry.
// It does not claim pinned-register, feature, or range-proof admission.
func bodyMatch(ts []token, l loop, params []wasm.ValType, runs []wasm.LocalRun) bool {
	start := l.end - 14
	if start <= l.start || l.end+1 >= len(ts) {
		return false
	}
	t := ts[start : l.end+2]
	ops := []byte{0x20, 0x20, 0x29, 0x7c, 0x21, 0x20, 0x41, 0x6a, 0x21, 0x20, 0x41, 0x6b, 0x21, 0x0c, 0x0b, 0x0b}
	for i, o := range ops {
		if t[i].Op != o {
			return false
		}
	}
	a, p, c := t[0].Index, t[1].Index, t[9].Index
	ct, cok := wasm.LocalType(params, runs, c)
	at, aok := wasm.LocalType(params, runs, a)
	pt, pok := wasm.LocalType(params, runs, p)
	return cok && aok && pok && ct == wasm.I32 && at == wasm.I64 && pt == wasm.I32 && c != p && a < 65535 && p < 65535 && t[2].Memory == 0 && t[2].Offset == 0 && t[4].Index == a && t[5].Index == p && t[6].Constant == 8 && t[8].Index == p && t[10].Constant == 1 && t[12].Index == c && t[13].Index == 0
}
func exact(ts []token, l loop, params []wasm.ValType, runs []wasm.LocalRun) bool {
	if !bodyMatch(ts, l, params, runs) {
		return false
	}
	start := l.end - 14
	var header []token
	for _, t := range ts[l.start+1 : start] {
		if t.Op != 1 {
			header = append(header, t)
		}
	}
	// The body reader begins immediately after br_if. A nop there is not
	// admissible even though nops before/between header instructions are.
	return len(header) == 3 && header[0].Op == 0x20 && header[1].Op == 0x45 && header[2].Op == 0x0d && header[2].Index == 1 && header[0].Index == ts[start+9].Index && ts[start-1].Op == 0x0d
}
func screen(ts []token, l loop, params []wasm.ValType, runs []wasm.LocalRun) []candidate {
	var stack []value
	var out []candidate
	pop := func() value {
		if len(stack) == 0 {
			return value{}
		}
		v := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		return v
	}
	for i := l.start + 1; i < l.end; i++ {
		t := ts[i]
		if t.Op == 3 {
			depth := 1
			for i++; i < l.end && depth > 0; i++ {
				switch ts[i].Op {
				case 2, 3, 4, 6, 0x1f:
					depth++
				case 0x0b:
					depth--
				}
			}
			i--
			stack = nil
			continue
		}
		switch {
		case t.Op == 0x20:
			stack = append(stack, value{locals: map[uint32]bool{t.Index: true}})
		case t.Op >= 0x41 && t.Op <= 0x44:
			stack = append(stack, value{})
		case t.Op >= 0x28 && t.Op <= 0x35:
			v := pop()
			v.memory = true
			stack = append(stack, v)
		case t.Op == 0x21 || t.Op == 0x22:
			v := pop()
			vt, ok := wasm.LocalType(params, runs, t.Index)
			if ok && v.memory && v.add && v.locals[t.Index] && i > 0 && (ts[i-1].Op == 0x7c && vt == wasm.I64 || ts[i-1].Op == 0x6a && vt == wasm.I32 || ts[i-1].Op == 0x92 && vt == wasm.F32 || ts[i-1].Op == 0xa0 && vt == wasm.F64) {
				lo := i - 24
				if lo < l.start+1 {
					lo = l.start + 1
				}
				hi := i + 9
				if hi > l.end+1 {
					hi = l.end + 1
				}
				out = append(out, candidate{LoopPC: ts[l.start].PC, WritePC: t.PC, Accumulator: t.Index, Type: vt.String(), Instructions: append([]token(nil), ts[lo:hi]...)})
			}
			if t.Op == 0x22 {
				stack = append(stack, v)
			}
		case t.Op == 0x1a:
			pop()
		case t.Op >= 0x6a && t.Op <= 0x78 || t.Op >= 0x7c && t.Op <= 0x8a || t.Op >= 0x92 && t.Op <= 0x98 || t.Op >= 0xa0 && t.Op <= 0xa6:
			b, a := pop(), pop()
			stack = append(stack, merge(a, b, t.Op == 0x6a || t.Op == 0x7c || t.Op == 0x92 || t.Op == 0xa0))
		case t.Op >= 0xa7 && t.Op <= 0xc4: // conversions preserve provenance; reviewed separately
		case t.Op >= 0x45 && t.Op <= 0x66:
			stack = nil // comparisons terminate this straight-line expression
		default:
			stack = nil // never infer across control, calls, stores, SIMD, unknown stack effects
		}
	}
	return out
}
func scan(path string) result {
	r := result{Path: path}
	data, e := os.ReadFile(path)
	if e != nil {
		r.Error = e.Error()
		return r
	}
	r.Bytes = len(data)
	r.SHA256 = fmt.Sprintf("%x", sha256.Sum256(data))
	m, e := wasm.DecodeModule(data)
	if e != nil {
		r.Error = e.Error()
		return r
	}
	r.Functions = len(m.Code)
	for fi, f := range m.Code {
		ts, ls, e := decode(m, f.BodyBytes)
		if e != nil {
			r.Error = fmt.Sprintf("function %d: %v", fi, e)
			return r
		}
		r.Loops += len(ls)
		for _, t := range ts {
			if t.Op == 0x29 {
				r.I64Loads++
			}
		}
		var ft wasm.CompType
		if !m.ResolveLocalFuncType(fi, &ft) {
			r.Error = "function type unresolved"
			return r
		}
		name := ""
		absolute := fi + m.ImportedFuncCount()
		if m.NameSec != nil {
			for _, n := range m.NameSec.FunctionNames {
				if int(n.Index) == absolute {
					name = n.Name
					break
				}
			}
		}
		for _, l := range ls {
			if bodyMatch(ts, l, ft.Params, f.Locals.Runs) {
				r.BodyMatches++
			}
			if exact(ts, l, ft.Params, f.Locals.Runs) {
				r.Exact++
				r.Candidates = append(r.Candidates, candidate{Function: absolute, Name: name, LoopPC: ts[l.start].PC, Type: "i64", Reasons: []string{"exact structural match; actual compiler admission must be checked"}, Instructions: ts[l.start : l.end+2]})
			}
			for _, c := range screen(ts, l, ft.Params, f.Locals.Runs) {
				c.Function = absolute
				c.Name = name
				if c.Type == "i64" || c.Type == "i32" {
					r.IntegerCandidates++
				} else {
					r.FloatCandidates++
				}
				if c.Type != "i64" {
					c.Reasons = append(c.Reasons, "accumulator type is not i64")
				}
				if !exact(ts, l, ft.Params, f.Locals.Runs) {
					c.Reasons = append(c.Reasons, "loop does not have the exact top-tested countdown, one load, stride-8, local.set, br-0 body")
				}
				if c.Type == "i64" {
					r.Candidates = append(r.Candidates, c)
				}
			}
		}
	}
	return r
}
func typeNames(v []wasm.ValType) []string {
	out := make([]string, len(v))
	for i, t := range v {
		out[i] = t.String()
	}
	return out
}
func localNames(v []wasm.LocalRun) []map[string]any {
	out := make([]map[string]any, len(v))
	for i, r := range v {
		out[i] = map[string]any{"count": r.Count, "type": r.Type.String()}
	}
	return out
}
func main() {
	manifest := flag.String("manifest", "", "JSON array with local_path entries")
	dir := flag.String("dir", "", "recursive local Wasm directory")
	out := flag.String("out", "", "JSON output")
	inspect := flag.String("inspect", "", "decode one function or loop from this module")
	function := flag.Int("function", -1, "absolute Wasm function index for inspection")
	loopPC := flag.Int("loop-pc", -1, "expression-relative loop opcode offset; -1 means whole function")
	flag.Parse()
	if *inspect != "" {
		data, err := os.ReadFile(*inspect)
		if err != nil {
			panic(err)
		}
		m, err := wasm.DecodeModule(data)
		if err != nil {
			panic(err)
		}
		local := *function - m.ImportedFuncCount()
		if local < 0 || local >= len(m.Code) {
			panic("bad function index")
		}
		f := m.Code[local]
		ts, loops, err := decode(m, f.BodyBytes)
		if err != nil {
			panic(err)
		}
		if *loopPC >= 0 {
			found := false
			for _, l := range loops {
				if ts[l.start].PC == *loopPC {
					ts = ts[l.start : l.end+1]
					found = true
					break
				}
			}
			if !found {
				panic("loop not found")
			}
		}
		var ft wasm.CompType
		m.ResolveLocalFuncType(local, &ft)
		r := map[string]any{"path": *inspect, "sha256": fmt.Sprintf("%x", sha256.Sum256(data)), "function": *function, "local_function": local, "signature": map[string]any{"params": typeNames(ft.Params), "results": typeNames(ft.Results)}, "locals": localNames(f.Locals.Runs), "memories": m.Memories, "instructions": ts}
		b, err := json.MarshalIndent(r, "", "  ")
		if err != nil {
			panic(err)
		}
		if err = os.WriteFile(*out, append(b, '\n'), 0644); err != nil {
			panic(err)
		}
		return
	}
	var paths []string
	if *manifest != "" {
		b, e := os.ReadFile(*manifest)
		if e != nil {
			panic(e)
		}
		var rows []struct {
			Path string `json:"local_path"`
		}
		if e = json.Unmarshal(b, &rows); e != nil {
			panic(e)
		}
		for _, r := range rows {
			if r.Path != "" {
				paths = append(paths, r.Path)
			}
		}
	}
	if *dir != "" {
		e := filepath.WalkDir(*dir, func(p string, d os.DirEntry, e error) error {
			if e != nil {
				return e
			}
			if !d.IsDir() && filepath.Ext(p) == ".wasm" {
				paths = append(paths, p)
			}
			return nil
		})
		if e != nil {
			panic(e)
		}
	}
	var rows []result
	for _, p := range paths {
		r := scan(p)
		rows = append(rows, r)
		fmt.Printf("%s: %d loops, exact=%d integer=%d float=%d error=%s\n", p, r.Loops, r.Exact, r.IntegerCandidates, r.FloatCandidates, r.Error)
	}
	b, e := json.MarshalIndent(rows, "", "  ")
	if e != nil {
		panic(e)
	}
	if e = os.WriteFile(*out, append(b, '\n'), 0644); e != nil {
		panic(e)
	}
}
