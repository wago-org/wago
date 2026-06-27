// Package wago exposes compile, instantiate, and run helpers for WebAssembly modules.
package wago

import (
	"bytes"
	"encoding/binary"
	"encoding/gob"
	"fmt"
	"time"

	"github.com/wago-org/wago/src/core/compiler/backend/amd64"
	"github.com/wago-org/wago/src/core/compiler/frontend"
	wasm "github.com/wago-org/wago/src/core/compiler/wasm3"
	wruntime "github.com/wago-org/wago/src/core/runtime"
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
	m3, err := wasm.DecodeModule(wasmBytes)
	if err != nil {
		return nil, t, fmt.Errorf("decode: %w", err)
	}
	t1 := time.Now()
	if err := wasm.ValidateModule(m3); err != nil {
		return nil, t, fmt.Errorf("validate: %w", err)
	}
	if err := frontend.RejectUnsupported(m3); err != nil {
		return nil, t, fmt.Errorf("compile: %w", err)
	}
	t2 := time.Now()
	m := m3
	cm, err := amd64.CompileModule(m)
	if err != nil {
		return nil, t, fmt.Errorf("compile: %w", err)
	}
	if timed {
		t = Timings{t1.Sub(t0), t2.Sub(t1), time.Since(t2)}
	}

	c := &Compiled{Code: cm.Code, Entry: cm.Entry, NumImports: m.ImportedFuncCount(), Exports: map[string]int{}, GlobalExports: map[string]int{}}
	for i := range m.Imports {
		im := &m.Imports[i]
		switch im.Type.Kind {
		case wasm.ExternFunc:
			c.Imports = append(c.Imports, im.Module+"."+im.Name)
		case wasm.ExternGlobal:
			imp := GlobalImportDef{Module: im.Module, Name: im.Name, Type: im.Type.Global.Type, Mutable: im.Type.Global.Mutable}
			c.GlobalImports = append(c.GlobalImports, imp)
			c.Globals = append(c.Globals, GlobalDef{Type: imp.Type, Mutable: imp.Mutable})
		}
	}
	for li := range m.FuncTypes {
		ft, ok := m.LocalFuncType(li)
		if !ok {
			return nil, t, fmt.Errorf("function %d: unknown type", li)
		}
		c.Funcs = append(c.Funcs, FuncSig{ft.Params, ft.Results})
	}
	for i := range m.Globals {
		v, err := evalConstExprWithModule(m.Globals[i].Init, m.Globals[i].Type.Type, m)
		if err != nil {
			return nil, t, fmt.Errorf("global %d initializer: %w", i, err)
		}
		g := GlobalDef{Type: m.Globals[i].Type.Type, Mutable: m.Globals[i].Type.Mutable}
		applyGlobalInit(&g, v.Init())
		c.Globals = append(c.Globals, g)
	}
	for i := range m.Exports {
		switch m.Exports[i].Index.Kind {
		case wasm.ExternFunc:
			c.Exports[m.Exports[i].Name] = int(m.Exports[i].Index.Index)
		case wasm.ExternGlobal:
			c.GlobalExports[m.Exports[i].Name] = int(m.Exports[i].Index.Index)
		}
	}

	hasTable, tableSize, err := frontend.SupportedTableRuntimeShape(m)
	if err != nil {
		return nil, t, fmt.Errorf("compile: %w", err)
	}
	c.HasTable = hasTable
	c.TableSize = tableSize
	// Table 0 is the only table wired through the current runtime ABI.
	for i := range m.Imports {
		if m.Imports[i].Type.Kind == wasm.ExternFunc {
			c.FuncTypeID = append(c.FuncTypeID, m.CanonicalTypeID(m.Imports[i].Type.Type.Index))
		}
	}
	for li := range m.FuncTypes {
		c.FuncTypeID = append(c.FuncTypeID, m.CanonicalTypeID(m.FuncTypes[li].Index))
	}
	for i := range m.Elements {
		e := &m.Elements[i]
		if e.Mode.Kind != wasm.ElemActive || e.Kind.Kind != wasm.ElemFuncs || len(e.Kind.Funcs) == 0 {
			continue // only active func-index segments
		}
		base, err := evalConstExprWithModule(e.Mode.Offset, wasm.I32, m)
		if err != nil {
			return nil, t, fmt.Errorf("element %d offset: %w", i, err)
		}
		init := ElemInit{Funcs: make([]uint32, len(e.Kind.Funcs))}
		for j, fidx := range e.Kind.Funcs {
			init.Funcs[j] = uint32(fidx)
		}
		applyElemOffset(&init, base.Init())
		c.Elems = append(c.Elems, init)
	}
	for i := range m.Data {
		d := &m.Data[i]
		if d.Mode.Kind != wasm.DataActive {
			continue
		}
		off, err := evalConstExprWithModule(d.Mode.Offset, wasm.I32, m)
		if err != nil {
			return nil, t, fmt.Errorf("data %d offset: %w", i, err)
		}
		init := DataInit{Bytes: d.Init}
		applyDataOffset(&init, off.Init())
		c.Data = append(c.Data, init)
	}
	return c, t, nil
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

func (c *Compiled) validate() error {
	if c == nil {
		return fmt.Errorf("compiled module is nil")
	}
	if c.NumImports < 0 {
		return fmt.Errorf("compiled metadata invalid: negative NumImports %d", c.NumImports)
	}
	if len(c.Imports) != c.NumImports {
		return fmt.Errorf("compiled metadata invalid: Imports length %d != NumImports %d", len(c.Imports), c.NumImports)
	}
	if c.NumImports > maxInt()-len(c.Funcs) {
		return fmt.Errorf("compiled metadata invalid: function count overflows int")
	}
	if c.TableSize < 0 {
		return fmt.Errorf("compiled metadata invalid: negative TableSize %d", c.TableSize)
	}
	if !c.HasTable && c.TableSize != 0 {
		return fmt.Errorf("compiled metadata invalid: TableSize %d without table", c.TableSize)
	}
	if len(c.Elems) > 0 && !c.HasTable {
		return fmt.Errorf("compiled metadata invalid: %d element segment(s) without table", len(c.Elems))
	}
	if len(c.Entry) != len(c.Funcs) {
		return fmt.Errorf("compiled metadata invalid: Entry length %d != Funcs length %d", len(c.Entry), len(c.Funcs))
	}
	for i, off := range c.Entry {
		if off < 0 || off >= len(c.Code) {
			return fmt.Errorf("compiled metadata invalid: Entry[%d] offset %d out of code range %d", i, off, len(c.Code))
		}
	}
	totalFuncs := c.NumImports + len(c.Funcs)
	if len(c.FuncTypeID) != totalFuncs {
		return fmt.Errorf("compiled metadata invalid: FuncTypeID length %d != function count %d", len(c.FuncTypeID), totalFuncs)
	}
	for name, gfi := range c.Exports {
		if gfi < 0 || gfi >= totalFuncs {
			return fmt.Errorf("compiled metadata invalid: function export %q index %d out of range", name, gfi)
		}
	}
	if len(c.GlobalImports) > len(c.Globals) {
		return fmt.Errorf("compiled metadata invalid: GlobalImports length %d > Globals length %d", len(c.GlobalImports), len(c.Globals))
	}
	for i, imp := range c.GlobalImports {
		if !wasm.IsNumericGlobalType(imp.Type) {
			return fmt.Errorf("compiled metadata invalid: imported global %d has unsupported type %s", i, imp.Type)
		}
		g := c.Globals[i]
		if !valTypeEqual(g.Type, imp.Type) || g.Mutable != imp.Mutable {
			return fmt.Errorf("compiled metadata invalid: imported global %d metadata mismatch", i)
		}
	}
	for name, idx := range c.GlobalExports {
		if idx < 0 || idx >= len(c.Globals) {
			return fmt.Errorf("compiled metadata invalid: global export %q index %d out of range", name, idx)
		}
	}
	for i, g := range c.Globals {
		if !wasm.IsNumericGlobalType(g.Type) {
			return fmt.Errorf("compiled metadata invalid: global %d has unsupported type %s", i, g.Type)
		}
		if g.HasInitGlobal {
			if g.InitGlobal < 0 || g.InitGlobal >= i || g.InitGlobal >= len(c.Globals) {
				return fmt.Errorf("compiled metadata invalid: global %d initializer references unavailable global %d", i, g.InitGlobal)
			}
			src := c.Globals[g.InitGlobal]
			if g.InitGlobal >= len(c.GlobalImports) || src.Mutable {
				return fmt.Errorf("compiled metadata invalid: global %d initializer references non-imported or mutable global %d", i, g.InitGlobal)
			}
			if !valTypeEqual(src.Type, g.Type) {
				return fmt.Errorf("compiled metadata invalid: global %d initializer type %s != source global %d type %s", i, g.Type, g.InitGlobal, src.Type)
			}
		}
	}
	for seg, el := range c.Elems {
		if el.Offset.HasGlobal {
			if err := c.validateDeferredOffsetGlobal("element", seg, el.Offset.Global); err != nil {
				return err
			}
		}
		for k, fidx := range el.Funcs {
			if int(fidx) >= totalFuncs {
				return fmt.Errorf("compiled metadata invalid: element %d function %d index %d out of range", seg, k, fidx)
			}
		}
	}
	for seg, d := range c.Data {
		if d.Offset.HasGlobal {
			if err := c.validateDeferredOffsetGlobal("data", seg, d.Offset.Global); err != nil {
				return err
			}
		}
	}
	if err := c.validateArenaFootprint(); err != nil {
		return err
	}
	return nil
}

func maxInt() int { return int(^uint(0) >> 1) }

func (c *Compiled) validateArenaFootprint() error {
	maxParams, maxResults, err := c.maxCallSlots()
	if err != nil {
		return fmt.Errorf("compiled metadata invalid: %w", err)
	}
	need, err := wruntime.InstantiateArenaNeed(wruntime.InstantiateFootprint{
		GlobalCount:    len(c.Globals),
		HasTable:       c.HasTable,
		TableSize:      c.TableSize,
		ElemCount:      len(c.Elems),
		MaxParamSlots:  maxParams,
		MaxResultSlots: maxResults,
	})
	if err != nil {
		return fmt.Errorf("compiled metadata invalid: %w", err)
	}
	if need > wruntime.InstantiateArenaSize {
		return fmt.Errorf("compiled metadata invalid: instantiate arena need %d > limit %d", need, wruntime.InstantiateArenaSize)
	}
	return nil
}

func (c *Compiled) maxCallSlots() (params, results int, err error) {
	for i, fn := range c.Funcs {
		if len(fn.Params) > maxInt()/8 {
			return 0, 0, fmt.Errorf("function %d parameter count %d overflows call buffer", i, len(fn.Params))
		}
		if len(fn.Results) > maxInt()/8 {
			return 0, 0, fmt.Errorf("function %d result count %d overflows call buffer", i, len(fn.Results))
		}
		if len(fn.Params) > params {
			params = len(fn.Params)
		}
		if len(fn.Results) > results {
			results = len(fn.Results)
		}
	}
	return params, results, nil
}

func (c *Compiled) validateDeferredOffsetGlobal(kind string, seg, idx int) error {
	if idx < 0 || idx >= len(c.Globals) {
		return fmt.Errorf("compiled metadata invalid: %s %d offset global %d out of range", kind, seg, idx)
	}
	g := c.Globals[idx]
	if idx >= len(c.GlobalImports) || g.Mutable || !valTypeEqual(g.Type, wasm.I32) {
		return fmt.Errorf("compiled metadata invalid: %s %d offset global %d must be imported immutable i32", kind, seg, idx)
	}
	return nil
}

const wagoMagic = "WAGO"
const wagoVersion = 5

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
	if err := gob.NewDecoder(bytes.NewReader(data[5:])).Decode((*plain)(c)); err != nil {
		return err
	}
	return c.validate()
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
	if len(args) > len(in.serArgs)/8 {
		return nil, fmt.Errorf("%s requires %d arg slot(s), instance buffer has %d", export, len(args), len(in.serArgs)/8)
	}
	if len(sig.Results) > len(in.results)/8 {
		return nil, fmt.Errorf("%s requires %d result slot(s), instance buffer has %d", export, len(sig.Results), len(in.results)/8)
	}
	for i, a := range args {
		if !valTypeEqual(a.Type, sig.Params[i]) {
			return nil, fmt.Errorf("%s arg %d has type %s, want %s", export, i, a.Type, sig.Params[i])
		}
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
		off := i * 8
		if off+8 > len(in.results) {
			return nil, fmt.Errorf("%s result %d exceeds instance result buffer", export, i)
		}
		if valTypeEqual(rt, wasm.I64) || valTypeEqual(rt, wasm.F64) {
			out[i] = Value{rt, binary.LittleEndian.Uint64(in.results[off:])}
		} else { // i32 / f32 (4-byte)
			out[i] = Value{rt, uint64(binary.LittleEndian.Uint32(in.results[off:]))}
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

// Run is a convenience wrapper for int32 CLI-style arguments and int64 results.
// Arguments are coerced to the exported function's parameter types before Invoke.
func Run(wasmBytes []byte, export string, args ...int32) ([]int64, error) {
	return RunWithHost(wasmBytes, nil, export, args...)
}

// RunWithImports is Run with host functions and globals wired by "module.name".
func RunWithImports(wasmBytes []byte, imports Imports, export string, args ...int32) ([]int64, error) {
	c, err := Load(wasmBytes)
	if err != nil {
		return nil, err
	}
	li, err := c.localIndex(export)
	if err != nil {
		return nil, err
	}
	vals := valuesForIntArgs(c.Funcs[li].Params, args)
	in, err := InstantiateWithImports(c, imports)
	if err != nil {
		return nil, err
	}
	defer in.Close()
	res, err := in.Invoke(export, vals...)
	if err != nil {
		return nil, err
	}
	return valuesToInt64s(res), nil
}

func valuesForIntArgs(params []wasm.ValType, args []int32) []Value {
	vals := make([]Value, len(args))
	for i, a := range args {
		t := wasm.I32
		if i < len(params) {
			t = params[i]
		}
		switch valTypeCode(t) {
		case 0x7e: // i64
			vals[i] = I64(int64(a))
		case 0x7d: // f32
			vals[i] = F32(float32(a))
		case 0x7c: // f64
			vals[i] = F64(float64(a))
		default:
			vals[i] = I32(a)
		}
	}
	return vals
}

func valuesToInt64s(res []Value) []int64 {
	out := make([]int64, len(res))
	for i, v := range res {
		switch valTypeCode(v.Type) {
		case 0x7e, 0x7c: // i64 / f64
			out[i] = int64(v.Bits)
		case 0x7d: // f32
			out[i] = int64(uint32(v.Bits))
		default:
			out[i] = int64(int32(uint32(v.Bits)))
		}
	}
	return out
}

// RunWithHost is Run with host imports wired by "module.name".
func RunWithHost(wasmBytes []byte, hosts map[string]HostFunc, export string, args ...int32) ([]int64, error) {
	return RunWithImports(wasmBytes, Imports{Funcs: hosts}, export, args...)
}
