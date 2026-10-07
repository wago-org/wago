//go:build linux && !tinygo

package riscv64

import (
	"fmt"
	enc "github.com/wago-org/wago/src/core/encoder/riscv64"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	native "github.com/wago-org/wago/src/core/runtime/riscv64"
)

const TrapMemoryBounds = 5

type globalSpec struct {
	wide, mutable bool
	value         uint64
	typ           wasm.ValType
}
type functionSpec struct {
	params, results []bool
	key             uint64
}
type memorySpec struct {
	min              uint32
	max              uint64
	hasMax, imported bool
	module, name     string
}
type moduleState struct {
	production        bool
	importReferences  bool
	ownedTable        bool
	ownedTableGrowth  bool
	elementCount      int
	memory            *memorySpec
	globals           []globalSpec
	data              []native.MemoryInitializer
	passiveData       [][]byte // all Wasm data indices; active entries are dropped/nil
	functions         []functionSpec
	importedFunctions int
	types             []functionSpec
	resultCapacity    int
	capacity          int
	memoryExports     map[string]bool
	globalExports     map[string]int
}

func plainConst(expr wasm.Expr, wide bool) (uint64, error) {
	r := wasm.ReaderFrom(expr.BodyBytes)
	op, err := r.Byte()
	if err != nil {
		return 0, unsupported("empty constant expression")
	}
	var v uint64
	if wide && op == 0x42 {
		x, e := r.I64()
		v, err = uint64(x), e
	} else if !wide && op == 0x41 {
		x, e := r.I32()
		v, err = uint64(uint32(x)), e
	} else {
		return 0, unsupported("nonliteral initializer")
	}
	if err != nil {
		return 0, err
	}
	end, err := r.Byte()
	if err != nil || end != 0x0b || r.HasNext() {
		return 0, unsupported("extended initializer")
	}
	return v, nil
}
func admitStateMode(m *wasm.Module, production bool) (moduleState, error) {
	return admitStatePolicy(m, production, false, false, false)
}
func admitStatePolicy(m *wasm.Module, production, hostImports, publicExports, importedTables bool) (moduleState, error) {
	s := moduleState{production: production, memoryExports: make(map[string]bool), globalExports: make(map[string]int)}
	s.importReferences = production && importedTables
	var tableErr error
	s.ownedTable, tableErr = admitTableProfile(m, production, importedTables)
	if s.ownedTable {
		s.elementCount = len(m.Elements)
		// The frontend reserves max(1024,2*min) for observable no-max growth.
		t, _ := m.TableType(0)
		s.ownedTableGrowth = importedTables && m.ImportedTableCount() != 0 || t.Limits.HasMax || t.Limits.Min <= MaxOwnedTableEntries/2
	}
	if tableErr != nil {
		return s, tableErr
	}
	maxImports := 1
	if production {
		maxImports = MaxProductionImports
	} // 256 globals and one memory
	if len(m.Imports) > maxImports || len(m.Memories) > 1 {
		return s, unsupported("import/memory count bound")
	}
	var mt *wasm.MemType
	if len(m.Memories) == 1 {
		mt = &m.Memories[0]
		s.memory = &memorySpec{}
	}
	for _, im := range m.Imports {
		switch im.Type.Kind {
		case wasm.ExternFunc:
			if !production || !hostImports || s.ownedTable && !importedTables {
				return s, unsupported("function imports require qualified dynamic bindings and retained table contexts")
			}
			s.importedFunctions++
		case wasm.ExternTable:
			if !production || !importedTables || !s.ownedTable {
				return s, unsupported("imported table requires the retained public table ABI")
			}
		case wasm.ExternMem:
			if mt != nil {
				return s, unsupported("multiple memories")
			}
			t := im.Type.MemType()
			mt = &t
			s.memory = &memorySpec{imported: true, module: im.Module, name: im.Name}
		case wasm.ExternGlobal:
			if !production {
				return s, unsupported("isolated global import")
			}
			g := im.Type.GlobalType()
			if !scalarType(g.Type, true) {
				return s, unsupported("nonscalar global import")
			}
			s.globals = append(s.globals, globalSpec{wide: scalarWide(g.Type), mutable: g.Mutable, typ: g.Type})
		default:
			return s, unsupported("nonmemory/nonglobal import")
		}
	}
	if mt != nil {
		maxPages := uint64(native.MaxMemoryPages)
		if production {
			maxPages = 65536
		}
		if mt.Shared || mt.Limits.Addr64 || mt.Limits.Min > maxPages {
			return s, unsupported("shared/memory64/initial memory bound")
		}
		s.memory.min = uint32(mt.Limits.Min)
		s.memory.max = mt.Limits.Max
		s.memory.hasMax = mt.Limits.HasMax
	}
	if len(s.globals)+len(m.Globals) > 256 || len(m.Data) > native.MaxDataSegments {
		return s, unsupported("state count bound")
	}
	importedGlobals := len(s.globals)
	for _, g := range m.Globals {
		w := scalarWide(g.Type.Type)
		if !scalarType(g.Type.Type, production) {
			return s, unsupported("noninteger global")
		}
		v, err := admittedScalarConst(g.Init, g.Type.Type, production, s.globals[:importedGlobals])
		if err != nil {
			return s, err
		}
		s.globals = append(s.globals, globalSpec{wide: w, mutable: g.Type.Mutable, value: v, typ: g.Type.Type})
	}
	for _, d := range m.Data {
		if d.Mode.Kind == wasm.DataPassive {
			s.passiveData = append(s.passiveData, append([]byte(nil), d.Init...))
			continue
		}
		if s.memory == nil || d.Mode.Kind != wasm.DataActive || d.Mode.Mem != 0 {
			return s, unsupported("indexed/unsupported active data")
		}
		off, err := admittedScalarConst(d.Mode.Offset, wasm.I32, production, s.globals[:importedGlobals])
		if err != nil {
			return s, err
		}
		s.data = append(s.data, native.MemoryInitializer{Offset: uint32(off), Bytes: append([]byte(nil), d.Init...)})
		s.passiveData = append(s.passiveData, nil)
	}
	tableExports := 0
	for _, ex := range m.Exports {
		switch ex.Index.Kind {
		case wasm.ExternFunc:
			if ex.Index.Index < uint32(s.importedFunctions) && !(production && hostImports) {
				return s, unsupported("imported-function reexport lowering is pending")
			}
		case wasm.ExternMem:
			s.memoryExports[ex.Name] = true
		case wasm.ExternGlobal:
			s.globalExports[ex.Name] = int(ex.Index.Index)
		case wasm.ExternTable:
			if !production || !publicExports || !s.ownedTable || ex.Index.Index != 0 {
				return s, unsupported("table export requires the public owned table profile")
			}
			tableExports++
			if tableExports > MaxFunctions {
				return s, unsupported("owned table export count bound")
			}
		default:
			return s, unsupported("noninteger state export")
		}
	}
	for i, rec := range m.Types {
		ft := &rec.SubTypes[0].Comp
		fn := functionSpec{params: make([]bool, len(ft.Params)), results: make([]bool, len(ft.Results))}
		if s.ownedTable {
			var ok bool
			fn.key, ok = m.StructuralTypeKeyChecked(uint32(i))
			if !ok {
				return s, unsupported("bounded table signature identity")
			}
		}
		for k, t := range ft.Params {
			fn.params[k] = scalarWide(t)
		}
		for k, t := range ft.Results {
			fn.results[k] = scalarWide(t)
		}
		s.types = append(s.types, fn)
	}
	if s.importedFunctions > MaxHostFunctionImports || s.importedFunctions+len(m.Code) > MaxFunctions {
		return s, unsupported("aggregate function count bound")
	}
	for _, im := range m.Imports {
		if im.Type.Kind == wasm.ExternFunc {
			fn := s.types[im.Type.FuncType().Index]
			s.capacity = max(s.capacity, len(fn.params))
			s.resultCapacity = max(s.resultCapacity, len(fn.results))
			s.functions = append(s.functions, fn)
		}
	}
	for j := range m.Code {
		ft, _ := m.LocalFuncType(j)
		s.capacity = max(s.capacity, len(ft.Params))
		s.resultCapacity = max(s.resultCapacity, len(ft.Results))
		s.functions = append(s.functions, s.types[m.FuncTypes[j].Index])
	}
	return s, nil
}

// InstantiateOptions binds the single explicitly named nonshared memory import.
// All other imports stay unsupported; binding does not transfer creator ownership.
type InstantiateOptions struct {
	ImportedMemory           *native.LinearMemory
	ImportModule, ImportName string
}

func (s *moduleState) bindMemory(opts InstantiateOptions) (*native.LinearMemory, *native.MemoryLease, bool, error) {
	if s.memory == nil || !s.memory.imported {
		if opts.ImportedMemory != nil || opts.ImportModule != "" || opts.ImportName != "" {
			return nil, nil, false, fmt.Errorf("rv64: unexpected memory import")
		}
		if s.memory == nil {
			return nil, nil, false, nil
		}
		m, err := native.NewLinearMemoryWithLimits(s.memory.min, s.memory.max, s.memory.hasMax)
		if err != nil {
			return m, nil, true, err
		}
		l, err := m.Lease()
		return m, l, true, err
	}
	ms := s.memory
	if opts.ImportModule != ms.module || opts.ImportName != ms.name || opts.ImportedMemory == nil {
		return nil, nil, false, fmt.Errorf("rv64: missing named memory import %q.%q", ms.module, ms.name)
	}
	l, err := opts.ImportedMemory.LeaseFor(ms.min, ms.max, ms.hasMax)
	return opts.ImportedMemory, l, false, err
}
func (i *Instance) ReadMemory(name string, off uint32, dst []byte) error {
	if i == nil {
		return native.ErrClosed
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.closed || i.compiled == nil {
		return native.ErrClosed
	}
	if !i.compiled.state.memoryExports[name] {
		return fmt.Errorf("rv64: unknown memory export %q", name)
	}
	return i.lease.Read(off, dst)
}
func (i *Instance) WriteMemory(name string, off uint32, src []byte) error {
	if i == nil {
		return native.ErrClosed
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.closed || i.compiled == nil {
		return native.ErrClosed
	}
	if !i.compiled.state.memoryExports[name] {
		return fmt.Errorf("rv64: unknown memory export %q", name)
	}
	return i.lease.Write(off, src)
}
func (i *Instance) Global(name string) (uint64, error) {
	if i == nil {
		return 0, native.ErrClosed
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.closed || i.compiled == nil {
		return 0, native.ErrClosed
	}
	idx, ok := i.compiled.state.globalExports[name]
	if !ok {
		return 0, fmt.Errorf("rv64: unknown global export %q", name)
	}
	v, err := i.context.Global(idx)
	if !i.compiled.state.globals[idx].wide {
		v = uint64(uint32(v))
	}
	return v, err
}

func memoryOpcode(op byte) (size int, wide, signed, store, ok bool) {
	switch op {
	case 0x28, 0x2a:
		return 4, false, false, false, true
	case 0x29, 0x2b:
		return 8, true, false, false, true
	case 0x2c:
		return 1, false, true, false, true
	case 0x2d:
		return 1, false, false, false, true
	case 0x2e:
		return 2, false, true, false, true
	case 0x2f:
		return 2, false, false, false, true
	case 0x30:
		return 1, true, true, false, true
	case 0x31:
		return 1, true, false, false, true
	case 0x32:
		return 2, true, true, false, true
	case 0x33:
		return 2, true, false, false, true
	case 0x34:
		return 4, true, true, false, true
	case 0x35:
		return 4, true, false, false, true
	case 0x36, 0x38:
		return 4, false, false, true, true
	case 0x37, 0x39:
		return 8, true, false, true, true
	case 0x3a:
		return 1, false, false, true, true
	case 0x3b:
		return 2, false, false, true, true
	case 0x3c:
		return 1, true, false, true, true
	case 0x3d:
		return 2, true, false, true, true
	case 0x3e:
		return 4, true, false, true, true
	}
	return 0, false, false, false, false
}
func (f *integerEmitter) contextAddress(offset int32) (enc.Reg, int32) {
	if offset <= 2047 {
		return enc.X24, offset
	}
	f.a.Constant64(enc.Scratch1, int64(offset))
	f.a.Add(enc.Scratch1, enc.X24, enc.Scratch1)
	return enc.Scratch1, 0
}
func (f *integerEmitter) global(index uint32, wide, store bool) {
	if f.mode.production {
		f.productionGlobal(index, wide, store)
		return
	}
	base, off := f.contextAddress(int32(8 + index*8))
	if store {
		if wide {
			f.a.Sd(enc.X5, base, off)
		} else {
			f.a.Sw(enc.X5, base, off)
		}
	} else {
		if wide {
			f.a.Ld(enc.X5, base, off)
		} else {
			f.a.Lwu(enc.X5, base, off)
		}
	}
}

// Before the first byte access, use unsigned 64-bit EA and subtraction guards.
// The unaligned path uses only byte instructions and little-endian assembly.
func (f *integerEmitter) memoryAccess(offset uint64, size int, wide, signed, store bool) {
	f.a.Constant64(enc.X7, int64(offset))
	f.a.Add(enc.X5, enc.X5, enc.X7)
	if f.mode.production {
		f.productionMemorySize(enc.X29)
	} else {
		f.a.Ld(enc.X28, enc.X24, 0)
		f.a.Ld(enc.X29, enc.X28, 8)
	}
	f.a.Addi(enc.X7, enc.Zero, int32(size))
	f.trap(TrapMemoryBounds, f.a.BranchLong(enc.LTU, enc.X29, enc.X7))
	f.a.Sub(enc.X29, enc.X29, enc.X7)
	f.trap(TrapMemoryBounds, f.a.BranchLong(enc.LTU, enc.X29, enc.X5))
	if f.mode.production {
		f.a.Addi(enc.X28, enc.X25, 0)
	} else {
		f.a.Ld(enc.X28, enc.X28, 0)
	}
	f.a.Add(enc.X5, enc.X28, enc.X5)
	if store {
		for j := 0; j < size; j++ {
			f.a.Sb(enc.X6, enc.X5, int32(j))
			if j+1 < size {
				f.a.Srli(enc.X6, enc.X6, 8)
			}
		}
		return
	}
	f.Constant(6, 0, true)
	for j := 0; j < size; j++ {
		f.a.Lbu(enc.X7, enc.X5, int32(j))
		if j != 0 {
			f.a.Slli(enc.X7, enc.X7, uint8(j*8))
		}
		f.a.Or(enc.X6, enc.X6, enc.X7)
	}
	f.Move(5, 6, true)
	if signed {
		shift := uint8(64 - size*8)
		f.a.Slli(enc.X5, enc.X5, shift)
		f.a.Srai(enc.X5, enc.X5, shift)
	}
	if !wide {
		f.NormalizeI32(5)
	}
}
func (f *integerEmitter) memorySizeGrow(grow bool) {
	if f.mode.production {
		f.productionMemorySizeGrow(grow)
		return
	}
	f.a.Ld(enc.X28, enc.X24, 0)
	f.a.Ld(enc.X6, enc.X28, 8)
	f.a.Srli(enc.X6, enc.X6, 16)
	if !grow {
		f.Move(5, 6, false)
		return
	}
	f.a.Ld(enc.X7, enc.X28, 16)
	f.a.Srli(enc.X7, enc.X7, 16)
	f.a.Sub(enc.X7, enc.X7, enc.X6)
	fail := f.a.BranchLong(enc.LTU, enc.X7, enc.X5)
	f.a.Add(enc.X5, enc.X5, enc.X6)
	f.a.Slli(enc.X5, enc.X5, 16)
	f.a.Sd(enc.X5, enc.X28, 8)
	f.Move(5, 6, false)
	end := f.Jump()
	if err := f.Patch(fail, f.Position()); err != nil {
		f.err = err
	}
	f.Constant(5, -1, false)
	if err := f.Patch(end, f.Position()); err != nil {
		f.err = err
	}
}
