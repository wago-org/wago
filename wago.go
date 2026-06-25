// Package wago exposes compile, instantiate, and run helpers for WebAssembly modules.
package wago

import (
	"bytes"
	"encoding/binary"
	"encoding/gob"
	"fmt"
	"math"
	"time"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/backend/amd64"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime"
)

// Value is a typed wasm call argument or result.
type Value struct {
	Type wasm.ValType
	Bits uint64
}

func I32(v int32) Value   { return Value{wasm.I32, uint64(uint32(v))} }
func I64(v int64) Value   { return Value{wasm.I64, uint64(v)} }
func F32(v float32) Value { return Value{wasm.F32, uint64(math.Float32bits(v))} }
func F64(v float64) Value { return Value{wasm.F64, math.Float64bits(v)} }

func (v Value) AsI32() int32   { return int32(uint32(v.Bits)) }
func (v Value) AsI64() int64   { return int64(v.Bits) }
func (v Value) AsF32() float32 { return math.Float32frombits(uint32(v.Bits)) }
func (v Value) AsF64() float64 { return math.Float64frombits(v.Bits) }

func (v Value) String() string {
	switch v.Type {
	case wasm.I64:
		return fmt.Sprintf("%d", v.AsI64())
	case wasm.F32:
		return fmt.Sprintf("%g", v.AsF32())
	case wasm.F64:
		return fmt.Sprintf("%g", v.AsF64())
	default:
		return fmt.Sprintf("%d", v.AsI32())
	}
}

// HostFunc handles a void host import with one i32 argument.
type HostFunc func(arg int32)

type FuncSig struct{ Params, Results []wasm.ValType }

type ElemInit struct {
	Base  uint32
	Funcs []uint32
}

type DataInit struct {
	Offset uint32
	Bytes  []byte
}

// Compiled is emitted machine code plus instantiate-time metadata.
type Compiled struct {
	Code       []byte
	Entry      []int          // entry offset per local function
	Funcs      []FuncSig      // signature per local function
	Imports    []string       // "module.name" per imported function
	Exports    map[string]int // exported function name -> global function index
	NumImports int

	TableSize  int        // initial table length
	FuncTypeID []uint32   // canonical signature id per global function index
	Elems      []ElemInit // active element segments

	Data []DataInit // active data segments (copied into linear memory at instantiate)
}

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
	cm, err := amd64.CompileModule(m)
	if err != nil {
		return nil, t, fmt.Errorf("compile: %w", err)
	}
	if timed {
		t = Timings{t1.Sub(t0), t2.Sub(t1), time.Since(t2)}
	}

	c := &Compiled{Code: cm.Code, Entry: cm.Entry, NumImports: m.ImportedFuncCount(), Exports: map[string]int{}}
	for i := range m.Imports {
		if m.Imports[i].Kind == wasm.ExternFunc {
			c.Imports = append(c.Imports, m.Imports[i].Module+"."+m.Imports[i].Name)
		}
	}
	for li := range m.Functions {
		ft := &m.Types[m.Functions[li]]
		c.Funcs = append(c.Funcs, FuncSig{ft.Params, ft.Results})
	}
	for i := range m.Exports {
		if m.Exports[i].Kind == wasm.ExternFunc {
			c.Exports[m.Exports[i].Name] = int(m.Exports[i].Index)
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
		if base, ok := evalI32ConstExpr(e.Offset); ok {
			c.Elems = append(c.Elems, ElemInit{Base: base, Funcs: e.FuncIdx})
		}
	}
	for i := range m.Data {
		d := &m.Data[i]
		if d.Passive {
			continue
		}
		if off, ok := evalI32ConstExpr(d.Offset); ok {
			c.Data = append(c.Data, DataInit{Offset: off, Bytes: d.Init})
		}
	}
	return c, t, nil
}

func evalI32ConstExpr(b []byte) (uint32, bool) {
	r := wasm.NewReader(b)
	op, err := r.Byte()
	if err != nil || op != 0x41 { // i32.const
		return 0, false
	}
	v, err := r.I32()
	if err != nil {
		return 0, false
	}
	return uint32(v), true
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
const wagoVersion = 1

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

// Instance is ready for repeated Invoke calls.
type Instance struct {
	c                      *Compiled
	eng                    *runtime.Engine
	jm                     *runtime.JobMemory
	ar                     *runtime.Arena
	base                   uintptr
	mem                    []byte
	hosts                  map[string]HostFunc
	hostLog                []byte
	serArgs, results, trap []byte
}

// Instantiate maps code, initializes memory/table state, and allocates call buffers.
func Instantiate(c *Compiled, hosts map[string]HostFunc) (*Instance, error) {
	eng, err := runtime.NewEngine()
	if err != nil {
		return nil, err
	}
	jm, err := runtime.NewJobMemory(1 << 16)
	if err != nil {
		eng.Close()
		return nil, err
	}
	ar, err := runtime.NewArena(1 << 20)
	if err != nil {
		jm.Close()
		eng.Close()
		return nil, err
	}
	mem, base, err := runtime.MapCode(c.Code)
	if err != nil {
		ar.Close()
		jm.Close()
		eng.Close()
		return nil, err
	}
	const maxEntries = (1 << 16) / 8
	hostLog := ar.Alloc(8 + maxEntries*8)
	jm.SetCustomCtx(uintptr(unsafe.Pointer(&hostLog[0])))

	// Table descriptor: [len u32][pad][entry...], entry {codePtr u64, sigID u32, pad u32}.
	if c.TableSize > 0 || len(c.Elems) > 0 {
		size := c.TableSize
		desc := ar.Alloc(8 + size*16)
		binary.LittleEndian.PutUint32(desc, uint32(size))
		for _, el := range c.Elems {
			for k, fidx := range el.Funcs {
				slot := int(el.Base) + k
				if slot < 0 || slot >= size {
					continue
				}
				off := 8 + slot*16
				if li := int(fidx) - c.NumImports; li >= 0 && li < len(c.Entry) {
					binary.LittleEndian.PutUint64(desc[off:], uint64(base)+uint64(c.Entry[li]))
				}
				if int(fidx) < len(c.FuncTypeID) {
					binary.LittleEndian.PutUint32(desc[off+8:], c.FuncTypeID[fidx])
				}
			}
		}
		jm.SetTablePtr(uintptr(unsafe.Pointer(&desc[0])))
	}

	if len(c.Data) > 0 {
		lin := jm.LinearMemory()
		for _, d := range c.Data {
			if int(d.Offset) <= len(lin) && int(d.Offset)+len(d.Bytes) <= len(lin) {
				copy(lin[d.Offset:], d.Bytes)
			}
		}
	}

	return &Instance{
		c: c, eng: eng, jm: jm, ar: ar, base: base, mem: mem, hosts: hosts, hostLog: hostLog,
		serArgs: ar.Alloc(512), results: ar.Alloc(512), trap: ar.Alloc(8),
	}, nil
}

// Close releases the instance's mapped code and memory.
func (in *Instance) Close() {
	runtime.Unmap(in.mem)
	in.ar.Close()
	in.jm.Close()
	in.eng.Close()
}

// LinearMemory exposes the instance's linear memory for zero-copy access.
func (in *Instance) LinearMemory() []byte { return in.jm.LinearMemory() }

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

// RunValuesWithHost compiles (or loads) and invokes an export in one shot.
func RunValuesWithHost(wasmBytes []byte, hosts map[string]HostFunc, export string, args ...Value) ([]Value, error) {
	c, err := Load(wasmBytes)
	if err != nil {
		return nil, err
	}
	in, err := Instantiate(c, hosts)
	if err != nil {
		return nil, err
	}
	defer in.Close()
	return in.Invoke(export, args...)
}

// RunValues is RunValuesWithHost with no host imports.
func RunValues(wasmBytes []byte, export string, args ...Value) ([]Value, error) {
	return RunValuesWithHost(wasmBytes, nil, export, args...)
}

// Run is a convenience wrapper for i32 arguments and int64 results.
func Run(wasmBytes []byte, export string, args ...int32) ([]int64, error) {
	return RunWithHost(wasmBytes, nil, export, args...)
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
