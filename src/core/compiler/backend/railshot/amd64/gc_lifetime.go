//go:build amd64

package amd64

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"strings"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// Analysis is deliberately not a lowering permission. No plan is consumed by
// codegen; allocation charges, traps and exact roots use the existing path.
var gcLifetimeAnalyzeEnabled bool

const (
	gcLifetimeMaxBody      = 4096
	gcLifetimeMaxRecords   = 256
	gcLifetimeMaxSlots     = 64
	gcLifetimeMaxFields    = 32
	gcLifetimeMaxFunctions = 4096
)

// GCLifetimeResult is an immutable-by-convention diagnostic value. Sites is a
// lower bound unless DenominatorKnown: a rejected prefix is never coverage.
type GCLifetimeResult struct {
	ConstructorOffset                          int // byte offset, -1 until the first constructor
	Function                                   int
	Reason                                     string
	Candidate                                  bool
	DenominatorKnown                           bool
	Sites, Records, Reads, Aliases, Overwrites int
	LastAliasUse                               int
	Type                                       uint32
	// Identity binds the complete body, signature, locals and selected layout to
	// the Analyze-v1 contract. It is not a replay certificate for a future consumer.
	Identity [32]byte
}

type GCLifetimeReport struct {
	Functions []GCLifetimeResult
	Omitted   int // module report budget; denominator unknown for omitted functions
}

func (r *GCLifetimeReport) String() string {
	if r == nil {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "gc-lifetime: analysis-only (no allocation elimination), omitted=%d\n", r.Omitted)
	for _, f := range r.Functions {
		fmt.Fprintf(&b, "  func=%d candidate=%t reason=%s sites=%d denominator-known=%t records=%d reads=%d alias-reads=%d overwrites=%d last-alias-use=%d constructor-offset=%d identity=%x\n", f.Function, f.Candidate, f.Reason, f.Sites, f.DenominatorKnown, f.Records, f.Reads, f.Aliases, f.Overwrites, f.LastAliasUse, f.ConstructorOffset, f.Identity)
	}
	return b.String()
}

// One serial diagnostic pass before worker launch gives deterministic reports,
// bounded aggregate storage and no shared mutable worker/retry state. Off never
// calls this function. At most 4096 functions * 256 records are interpreted;
// each function has fixed local/operand arrays and no persistent instruction IR.
func analyzeGCLifetimes(m *wasm.Module) *GCLifetimeReport {
	n := min(len(m.Code), gcLifetimeMaxFunctions)
	r := &GCLifetimeReport{Functions: make([]GCLifetimeResult, n), Omitted: len(m.Code) - n}
	for i := range r.Functions {
		r.Functions[i] = analyzeGCLifetime(m, i, gcLifetimeMaxRecords)
	}
	return r
}

type lifetimeValue struct {
	typ         wasm.ValType
	object      bool
	initializer bool // constant or never-written scalar parameter, including copies
}

func lifetimeScalar(t wasm.ValType) bool {
	return t == wasm.I32 || t == wasm.I64 || t == wasm.F32 || t == wasm.F64
}

func analyzeGCLifetime(m *wasm.Module, i, recordBudget int) (out GCLifetimeResult) {
	out.Function, out.LastAliasUse, out.Reason = i, -1, "malformed"
	out.ConstructorOffset = -1
	if i < 0 || i >= len(m.Code) {
		return
	}
	c := &m.Code[i]
	if len(c.BodyBytes) > gcLifetimeMaxBody {
		out.Reason = "body-budget"
		return
	}
	ft, ok := m.LocalFuncType(i)
	if !ok {
		return
	}
	if len(ft.Params) > gcLifetimeMaxSlots || len(ft.Results) > 1 {
		out.Reason = "signature"
		return
	}
	for _, t := range ft.Params {
		if !lifetimeScalar(t) {
			out.Reason = "signature"
			return
		}
	}
	for _, t := range ft.Results {
		if !lifetimeScalar(t) {
			out.Reason = "object-result"
			return
		}
	}
	// Safe negative gate over the complete bounded bytecode. Immediate 0xfb bytes
	// may cause false positives only. No module summaries or GC layouts required.
	if !bytes.Contains(c.BodyBytes, []byte{0xfb}) {
		out.Reason = "no-gc"
		out.DenominatorKnown = true
		return
	}
	var locals, stack [gcLifetimeMaxSlots]lifetimeValue
	var localTypes [gcLifetimeMaxSlots]wasm.ValType
	var written [gcLifetimeMaxSlots]bool
	if len(c.Locals.Runs) > gcLifetimeMaxSlots {
		out.Reason = "local-budget"
		return
	}
	n := len(ft.Params)
	for j, t := range ft.Params {
		locals[j] = lifetimeValue{typ: t, initializer: true}
		localTypes[j] = t
	}
	for _, run := range c.Locals.Runs {
		if run.Count > uint32(gcLifetimeMaxSlots-n) {
			out.Reason = "local-budget"
			return
		}
		for j := uint32(0); j < run.Count; j++ {
			localTypes[n] = run.Type
			locals[n].typ = run.Type
			n++
		}
	}
	sp := 0
	push := func(v lifetimeValue) bool {
		if sp == len(stack) {
			return false
		}
		stack[sp] = v
		sp++
		return true
	}
	pop := func() (lifetimeValue, bool) {
		if sp == 0 {
			return lifetimeValue{}, false
		}
		sp--
		return stack[sp], true
	}
	r := wasm.ReaderFrom(c.BodyBytes)
	var fields []wasm.FieldType
	for r.HasNext() {
		if out.Records >= min(recordBudget, gcLifetimeMaxRecords) {
			out.Reason = "record-budget"
			return
		}
		pc := r.Offset()
		out.Records++
		op, err := r.Byte()
		if err != nil {
			return
		}
		switch op {
		case 0x01: // nop
		case 0x0b:
			if r.HasNext() || sp != len(ft.Results) {
				return
			}
			if sp == 1 && (stack[0].object || stack[0].typ != ft.Results[0]) {
				return
			}
			out.DenominatorKnown = true
			if out.Sites != 1 {
				out.Reason = "no-constructor"
				return
			}
			// Locals die at function exit. All uses have been checked; aliases need
			// not be explicitly nulled/dropped to close their function-local lifetime.
			h := sha256.New()
			h.Write(c.BodyBytes)
			fmt.Fprintf(h, "Analyze-v1/%d/%v/%v/%v/%d/%v", i, ft.Params, ft.Results, c.Locals, out.Type, fields)
			copy(out.Identity[:], h.Sum(nil))
			out.Candidate = true
			out.Reason = "closed-scalar-struct"
			return
		case 0x41, 0x42, 0x43, 0x44:
			t := wasm.I32
			switch op {
			case 0x41:
				_, err = r.I32()
			case 0x42:
				t = wasm.I64
				_, err = r.I64()
			case 0x43:
				t = wasm.F32
				_, err = r.LEU32()
			case 0x44:
				t = wasm.F64
				_, err = r.LEU64()
			}
			if err != nil {
				return
			}
			if !push(lifetimeValue{typ: t, initializer: true}) {
				out.Reason = "stack-budget"
				return
			}
		case 0x20, 0x21, 0x22:
			idx, e := r.U32()
			if e != nil || idx >= uint32(n) {
				return
			}
			if op == 0x20 {
				v := locals[idx]
				if int(idx) < len(ft.Params) && written[idx] {
					v.initializer = false
				}
				if !push(v) {
					out.Reason = "stack-budget"
					return
				}
				if v.object {
					out.Aliases++
					out.LastAliasUse = pc
				}
			} else {
				v, yes := pop()
				if !yes {
					return
				}
				if int(idx) < len(ft.Params) {
					out.Reason = "parameter-write"
					return
				}
				if v.object {
					if localTypes[idx].Kind() != wasm.ValRef {
						return
					}
				} else if v.typ != localTypes[idx] {
					return
				}
				if written[idx] {
					out.Overwrites++
				}
				written[idx] = true
				locals[idx] = v
				if op == 0x22 && !push(v) {
					out.Reason = "stack-budget"
					return
				}
			}
		case 0x1a:
			v, yes := pop()
			if !yes {
				return
			}
			if v.object {
				out.LastAliasUse = pc
			}
		case 0xfb:
			sub, e := r.U32()
			if e != nil {
				return
			}
			if sub != 0 && sub != 1 && sub != 2 {
				out.Reason = "unsupported-gc"
				return
			}
			ti, e := r.U32()
			if e != nil {
				return
			}
			st, yes := m.TypeSubType(ti)
			if !yes || st.Comp.Kind != wasm.CompStruct {
				out.Reason = "layout"
				return
			}
			if !st.Final || len(st.Supers) != 0 || st.Metadata.Describes.Present() || st.Metadata.Descriptor.Present() || len(st.Comp.Fields) > gcLifetimeMaxFields {
				out.Reason = "layout"
				return
			}
			for _, f := range st.Comp.Fields {
				if f.Storage().Packed() || !lifetimeScalar(f.Storage().Val()) {
					out.Reason = "layout"
					return
				}
			}
			if sub == 0 || sub == 1 {
				out.Sites++
				if out.Sites != 1 {
					out.Reason = "multiple-constructors"
					return
				}
				out.ConstructorOffset = pc
				fields = st.Comp.Fields
				out.Type = ti
				if sub == 0 {
					for j := len(fields) - 1; j >= 0; j-- {
						v, yes := pop()
						if !yes {
							return
						}
						if v.object || !v.initializer || v.typ != fields[j].Storage().Val() {
							out.Reason = "initializer"
							return
						}
					}
				}
				if !push(lifetimeValue{object: true}) {
					out.Reason = "stack-budget"
					return
				}
			} else {
				fi, e := r.U32()
				if e != nil || fi >= uint32(len(st.Comp.Fields)) {
					return
				}
				v, yes := pop()
				if !yes || !v.object || ti != out.Type {
					out.Reason = "unknown-object"
					return
				}
				out.Reads++
				out.LastAliasUse = pc
				if !push(lifetimeValue{typ: st.Comp.Fields[fi].Storage().Val()}) {
					out.Reason = "stack-budget"
					return
				}
			}
		default:
			out.Reason = fmt.Sprintf("unsupported-opcode-0x%02x", op)
			return
		}
	}
	return // missing end, never accept a prefix
}
