// Package wago exposes compile, instantiate, and run helpers for WebAssembly modules.
package wago

import (
	"bytes"
	"encoding/binary"
	"encoding/gob"
	"fmt"
	"time"

	"github.com/wago-org/wago/src/core/compiler/backend/amd64"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

type Timings struct{ Decode, Validate, Compile time.Duration }

// Compile decodes, validates, and compiles a wasm module to native code.
func Compile(wasmBytes []byte) (*Compiled, error) {
	c, _, err := compile(wasmBytes, false)
	return c, err
}

// CompileTimed is Compile with per-phase timings.
func CompileTimed(wasmBytes []byte) (*Compiled, Timings, error) {
	return compile(wasmBytes, true)
}

// compile builds the serialized metadata needed to instantiate without re-decoding.
func compile(wasmBytes []byte, timed bool) (*Compiled, Timings, error) {
	var t Timings
	t0 := time.Now()
	m, err := wasm.Decode(wasmBytes)
	if err != nil {
		return nil, t, fmt.Errorf("decode: %w", err)
	}
	t1 := time.Now()
	if err := wasm.Validate(m); err != nil {
		return nil, t, fmt.Errorf("validate: %w", err)
	}
	t2 := time.Now()
	if err := rejectUnsupportedGlobalTypes(m); err != nil {
		return nil, t, err
	}
	cm, err := amd64.CompileModule(m)
	if err != nil {
		return nil, t, fmt.Errorf("compile: %w", err)
	}
	if timed {
		t = Timings{t1.Sub(t0), t2.Sub(t1), time.Since(t2)}
	}

	c := &Compiled{Code: cm.Code, Entry: cm.Entry, NumImports: m.ImportedFuncCount(), Exports: map[string]int{}, GlobalExports: map[string]int{}}
	for i := range m.Imports {
		switch m.Imports[i].Kind {
		case wasm.ExternFunc:
			c.Imports = append(c.Imports, m.Imports[i].Module+"."+m.Imports[i].Name)
		case wasm.ExternGlobal:
			imp := GlobalImportDef{Module: m.Imports[i].Module, Name: m.Imports[i].Name, Type: m.Imports[i].Global.Val, Mutable: m.Imports[i].Global.Mutable}
			c.GlobalImports = append(c.GlobalImports, imp)
			c.Globals = append(c.Globals, GlobalDef{Type: imp.Type, Mutable: imp.Mutable})
		}
	}
	for li := range m.Functions {
		ft := &m.Types[m.Functions[li]]
		c.Funcs = append(c.Funcs, FuncSig{ft.Params, ft.Results})
	}
	for i := range m.Globals {
		v, err := evalConstExprWithModule(m.Globals[i].Init, m.Globals[i].Type.Val, m)
		if err != nil {
			return nil, t, fmt.Errorf("global %d initializer: %w", i, err)
		}
		g := GlobalDef{Type: m.Globals[i].Type.Val, Mutable: m.Globals[i].Type.Mutable}
		applyGlobalInit(&g, v.Init())
		c.Globals = append(c.Globals, g)
	}
	for i := range m.Exports {
		switch m.Exports[i].Kind {
		case wasm.ExternFunc:
			c.Exports[m.Exports[i].Name] = int(m.Exports[i].Index)
		case wasm.ExternGlobal:
			c.GlobalExports[m.Exports[i].Name] = int(m.Exports[i].Index)
		}
	}

	if len(m.Tables) > 0 {
		c.TableSize = int(m.Tables[0].Limits.Min)
	}
	// Table 0 is the only table wired through the current runtime ABI.
	for i := range m.Imports {
		if m.Imports[i].Kind == wasm.ExternFunc {
			c.FuncTypeID = append(c.FuncTypeID, m.CanonicalTypeID(m.Imports[i].TypeIndex))
		}
	}
	for li := range m.Functions {
		c.FuncTypeID = append(c.FuncTypeID, m.CanonicalTypeID(m.Functions[li]))
	}
	for i := range m.Elements {
		e := &m.Elements[i]
		if e.Passive || e.Declared || len(e.FuncIdx) == 0 {
			continue // only active func-index segments
		}
		base, err := evalConstExprWithModule(e.Offset, wasm.I32, m)
		if err != nil {
			return nil, t, fmt.Errorf("element %d offset: %w", i, err)
		}
		init := ElemInit{Funcs: e.FuncIdx}
		applyElemOffset(&init, base.Init())
		c.Elems = append(c.Elems, init)
	}
	for i := range m.Data {
		d := &m.Data[i]
		if d.Passive {
			continue
		}
		off, err := evalConstExprWithModule(d.Offset, wasm.I32, m)
		if err != nil {
			return nil, t, fmt.Errorf("data %d offset: %w", i, err)
		}
		init := DataInit{Bytes: d.Init}
		applyDataOffset(&init, off.Init())
		c.Data = append(c.Data, init)
	}
	return c, t, nil
}

func rejectUnsupportedGlobalTypes(m *wasm.Module) error {
	for i := range m.Imports {
		if m.Imports[i].Kind == wasm.ExternGlobal && !wasm.IsNumericGlobalType(m.Imports[i].Global.Val) {
			return fmt.Errorf("unsupported global type %s for import %q.%q", m.Imports[i].Global.Val, m.Imports[i].Module, m.Imports[i].Name)
		}
	}
	for i := range m.Globals {
		if !wasm.IsNumericGlobalType(m.Globals[i].Type.Val) {
			return fmt.Errorf("unsupported global type %s for global %d", m.Globals[i].Type.Val, i)
		}
	}
	return nil
}

// Signature returns the parameter and result types of an exported function.
func (c *Compiled) Signature(export string) (params, results []wasm.ValType, err error) {
	li, err := c.localIndex(export)
	if err != nil {
		return nil, nil, err
	}
	return c.Funcs[li].Params, c.Funcs[li].Results, nil
}

func (c *Compiled) localIndex(export string) (int, error) {
	gfi, ok := c.Exports[export]
	if !ok {
		return 0, fmt.Errorf("no exported function %q", export)
	}
	li := gfi - c.NumImports
	if li < 0 || li >= len(c.Funcs) {
		return 0, fmt.Errorf("export %q is an imported function", export)
	}
	return li, nil
}

const wagoMagic = "WAGO"
const wagoVersion = 3

// plain avoids recursive gob encoding through MarshalBinary.
type plain Compiled

// MarshalBinary serializes the precompiled module to a ".wago" blob.
func (c *Compiled) MarshalBinary() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteString(wagoMagic)
	buf.WriteByte(wagoVersion)
	if err := gob.NewEncoder(&buf).Encode((*plain)(c)); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// UnmarshalBinary loads a ".wago" blob produced by MarshalBinary.
func (c *Compiled) UnmarshalBinary(data []byte) error {
	if !IsCompiled(data) {
		return fmt.Errorf("not a wago module")
	}
	if data[4] != wagoVersion {
		return fmt.Errorf("wago module version %d unsupported (want %d)", data[4], wagoVersion)
	}
	return gob.NewDecoder(bytes.NewReader(data[5:])).Decode((*plain)(c))
}

// IsCompiled reports whether b is a precompiled wago module (vs raw wasm).
func IsCompiled(b []byte) bool { return len(b) >= 5 && string(b[:4]) == wagoMagic }

// Load returns a *Compiled from either a precompiled ".wago" blob or raw wasm
// (which it compiles).
func Load(b []byte) (*Compiled, error) {
	if IsCompiled(b) {
		c := &Compiled{}
		return c, c.UnmarshalBinary(b)
	}
	return Compile(b)
}

// Invoke marshals slot-based arguments/results around one native WasmWrapper call.
func (in *Instance) Invoke(export string, args ...Value) ([]Value, error) {
	li, err := in.c.localIndex(export)
	if err != nil {
		return nil, err
	}
	sig := in.c.Funcs[li]
	if len(args) != len(sig.Params) {
		return nil, fmt.Errorf("%s expects %d arg(s), got %d", export, len(sig.Params), len(args))
	}
	for i, a := range args {
		binary.LittleEndian.PutUint64(in.serArgs[i*8:], a.Bits)
	}
	binary.LittleEndian.PutUint32(in.hostLog, 0) // reset host-call log
	entry := in.base + uintptr(in.c.Entry[li])
	if err := in.eng.Call(entry, in.serArgs, in.jm.LinearMemory(), in.trap, in.results); err != nil {
		return nil, err
	}
	n := binary.LittleEndian.Uint32(in.hostLog)
	for i := uint32(0); i < n; i++ {
		off := 8 + i*8
		imp := binary.LittleEndian.Uint32(in.hostLog[off:])
		arg := int32(binary.LittleEndian.Uint32(in.hostLog[off+4:]))
		if int(imp) < len(in.c.Imports) {
			if fn := in.hosts[in.c.Imports[imp]]; fn != nil {
				fn(arg)
			}
		}
	}
	out := make([]Value, len(sig.Results))
	for i, rt := range sig.Results {
		switch rt {
		case wasm.I64, wasm.F64:
			out[i] = Value{rt, binary.LittleEndian.Uint64(in.results[i*8:])}
		default: // i32 / f32 (4-byte)
			out[i] = Value{rt, uint64(binary.LittleEndian.Uint32(in.results[i*8:]))}
		}
	}
	return out, nil
}

// RunValuesWithImports compiles (or loads), instantiates with imports, and invokes an export in one shot.
func RunValuesWithImports(wasmBytes []byte, imports Imports, export string, args ...Value) ([]Value, error) {
	c, err := Load(wasmBytes)
	if err != nil {
		return nil, err
	}
	in, err := InstantiateWithImports(c, imports)
	if err != nil {
		return nil, err
	}
	defer in.Close()
	return in.Invoke(export, args...)
}

// RunValuesWithHost compiles (or loads) and invokes an export in one shot.
func RunValuesWithHost(wasmBytes []byte, hosts map[string]HostFunc, export string, args ...Value) ([]Value, error) {
	return RunValuesWithImports(wasmBytes, Imports{Funcs: hosts}, export, args...)
}

// RunValues is RunValuesWithHost with no host imports.
func RunValues(wasmBytes []byte, export string, args ...Value) ([]Value, error) {
	return RunValuesWithHost(wasmBytes, nil, export, args...)
}

// Run is a convenience wrapper for i32 arguments and int64 results.
func Run(wasmBytes []byte, export string, args ...int32) ([]int64, error) {
	return RunWithHost(wasmBytes, nil, export, args...)
}

// RunWithImports is Run with host functions and globals wired by "module.name".
func RunWithImports(wasmBytes []byte, imports Imports, export string, args ...int32) ([]int64, error) {
	vals := make([]Value, len(args))
	for i, a := range args {
		vals[i] = I32(a)
	}
	res, err := RunValuesWithImports(wasmBytes, imports, export, vals...)
	if err != nil {
		return nil, err
	}
	out := make([]int64, len(res))
	for i, v := range res {
		switch v.Type {
		case wasm.I64, wasm.F64:
			out[i] = int64(v.Bits)
		case wasm.F32:
			out[i] = int64(uint32(v.Bits))
		default:
			out[i] = int64(int32(uint32(v.Bits)))
		}
	}
	return out, nil
}

// RunWithHost is Run with host imports wired by "module.name".
func RunWithHost(wasmBytes []byte, hosts map[string]HostFunc, export string, args ...int32) ([]int64, error) {
	vals := make([]Value, len(args))
	for i, a := range args {
		vals[i] = I32(a)
	}
	res, err := RunValuesWithHost(wasmBytes, hosts, export, vals...)
	if err != nil {
		return nil, err
	}
	out := make([]int64, len(res))
	for i, v := range res {
		switch v.Type {
		case wasm.I64, wasm.F64:
			out[i] = int64(v.Bits)
		case wasm.F32:
			out[i] = int64(uint32(v.Bits))
		default:
			out[i] = int64(int32(uint32(v.Bits)))
		}
	}
	return out, nil
}
