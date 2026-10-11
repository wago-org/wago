//go:build linux && !tinygo

package riscv64

import (
	"bytes"
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
	signature       *wasm.CompType
}
type memorySpec struct {
	min              uint32
	max              uint64
	hasMax, imported bool
	module, name     string
}
type moduleState struct {
	production        bool
	localTailCalls    bool
	foreignTableCalls bool
	importTailCalls   bool
	dynamicTailCalls  bool
	referenceCalls    bool
	// The exact public two-token boundary may pass one authenticated funcref
	// argument through call_ref in addition to the callable target reference.
	publicReferenceCarriers            bool
	publicDynamicHostReferenceCarriers bool
	// Exact GC/i31 qualification profiles.
	i31Core               bool
	i31RefTest            bool
	i31RefEq              bool
	gcRefEq               bool
	gcRefTestTable        bool
	gcRefTestConcrete     bool
	gcRefTestAbstract     bool
	gcBrOnCastNullability bool
	gcStructGet           bool
	gcStructMutation      bool
	gcStructNull          bool
	gcStructPacked        bool
	gcStructBasic         bool
	gcStructGlobals       bool
	gcArrayLocal          bool
	gcArrayUniform        bool
	gcArrayFixedLocal     bool
	gcArrayFixedOfficial  bool
	gcArrayPackedData     bool
	gcArrayReference      bool
	gcArrayInitElem       bool
	gcArrayInitData       bool
	gcArrayInitDataWide   bool
	gcArrayFill           bool
	gcArrayCopy           bool
	// Private reference signatures use same-image direct calls. Descriptor calls
	// stay numeric; the flag alone never grants source/artifact admission.
	referenceFunctions      bool
	core3RefIsNullExecution bool
	declarationExternRefs   bool
	declarationNonnullRefs  bool
	importReferences        bool
	localMutableReferences  bool
	ownedTable              bool
	ownedTableGrowth        bool
	table64                 bool
	tableAddr64             []bool
	// Compact reference tables store one authenticated opaque carrier per
	// eight-byte row. Funcref tables retain their 32-byte callable descriptor
	// rows. Keep physical layout independent from the table index width.
	tableCompactRef   []bool
	elementCount      int
	memory            *memorySpec
	memoryCount       int
	memories          []wasm.MemType
	indexedMemories   bool
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

func (s *moduleState) publicReferenceCarrierType(index uint32) bool {
	return s != nil && s.publicReferenceCarriers && (index == 0 || s.publicDynamicHostReferenceCarriers && index == 2)
}

func (s *moduleState) tableWidth(index uint32) (wide bool, ok bool) {
	if s == nil || !s.ownedTable {
		return false, false
	}
	if len(s.tableAddr64) == 0 {
		return s.table64, index == 0
	}
	if uint64(index) >= uint64(len(s.tableAddr64)) {
		return false, false
	}
	return s.tableAddr64[index], true
}

func (s *moduleState) tableCompactReference(index uint32) (compact bool, ok bool) {
	if s == nil || !s.ownedTable {
		return false, false
	}
	// Legacy private fixtures and single-table pilots predate the layout
	// directory; their only admitted representation is the funcref descriptor.
	if len(s.tableCompactRef) == 0 {
		_, ok = s.tableWidth(index)
		return false, ok
	}
	if uint64(index) >= uint64(len(s.tableCompactRef)) {
		return false, false
	}
	return s.tableCompactRef[index], true
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

// admitStateOnlyExternrefGlobals recognizes a bounded directory containing at
// least one immutable null externref global plus optional immutable literal
// scalar globals in an otherwise empty public module. This declaration-only
// profile does not enable executable externref carriers, imports, mutation, or
// non-null reference ownership. Externref initializers may only copy null from
// an earlier immutable externref global; scalar initializer chains remain out
// of scope.
func admitStateOnlyExternrefGlobals(m *wasm.Module, production, public bool) bool {
	if !production || !public || len(m.Imports) != 0 || len(m.Types) != 0 || len(m.FuncTypes) != 0 || len(m.Code) != 0 || len(m.Globals) == 0 || len(m.Globals) > 256 || len(m.Exports) > 256 || m.Start != nil {
		return false
	}
	if len(m.Tables) != 0 || len(m.Memories) != 0 || len(m.Elements) != 0 || len(m.Tags) != 0 || len(m.StringRefs) != 0 || len(m.Data) != 0 || m.DataCount != nil || m.BranchHints != nil || m.UsesCompactImports {
		return false
	}
	for _, export := range m.Exports {
		if export.Index.Kind != wasm.ExternGlobal || uint64(export.Index.Index) >= uint64(len(m.Globals)) {
			return false
		}
	}
	hasExternref := false
	for i, g := range m.Globals {
		if g.Type.Mutable || len(g.Init.Instrs) != 0 {
			return false
		}
		if wasm.EqualValType(g.Type.Type, wasm.ExternRef) {
			hasExternref = true
			if bytes.Equal(g.Init.BodyBytes, []byte{0xd0, 0x6f, 0x0b}) {
				continue
			}
			if admitGlobalExpression(g.Init, m, wasm.ExternRef, i) == nil {
				continue
			}
			return false
		}
		if !scalarType(g.Type.Type, true) {
			return false
		}
		if _, err := plainScalarConst(g.Init, g.Type.Type, true); err != nil {
			return false
		}
	}
	return hasExternref
}

func admitStateMode(m *wasm.Module, production bool) (moduleState, error) {
	return admitStatePolicy(m, production, false, false, false)
}
func admitStatePolicy(m *wasm.Module, production, hostImports, publicExports, importedTables bool) (moduleState, error) {
	s := moduleState{production: production, memoryExports: make(map[string]bool), globalExports: make(map[string]int)}
	gcTypeSubtypingDeclarations := admitRV64GCTypeSubtypingDeclarations(m)
	gcRefEq := admitRV64GCRefEq(m)
	gcRefTestTable := admitRV64GCRefTestTable(m)
	gcBrOnCastNullability := admitRV64GCBrOnCastNullability(m) || admitRV64GCBrOnCastFailNullability(m)
	gcRefTestConcrete := admitRV64GCRefTestConcrete(m) || admitRV64GCRefCastConcrete(m) || admitRV64GCBrOnCastConcrete(m) || admitRV64GCBrOnCastFailConcrete(m) || gcBrOnCastNullability
	gcExtern := admitRV64GCExtern(m)
	gcRefCastAbstract := admitRV64GCRefCastAbstract(m)
	gcBrOnCastAbstract := admitRV64GCBrOnCastAbstract(m) || admitRV64GCBrOnCastFailAbstract(m)
	gcRefTestAbstract := admitRV64GCRefTestAbstract(m) || gcExtern || gcRefCastAbstract || gcBrOnCastAbstract
	gcStructGet := admitRV64GCStructGet(m)
	gcStructMutation := admitRV64GCStructMutation(m)
	gcStructNull := admitRV64GCStructNull(m)
	gcStructPacked := admitRV64GCStructPacked(m)
	gcStructBasic := admitRV64GCStructBasic(m)
	gcStructNumericGlobals := admitRV64GCStructNumericGlobals(m)
	gcArrayLocal := admitRV64GCArrayLocal(m)
	gcArrayUniform := admitRV64GCArrayUniform(m)
	gcArrayFixedLocal := admitRV64GCArrayFixedLocal(m)
	gcArrayFixedOfficial := admitRV64GCArrayOfficial(m)
	gcArrayPackedData := admitRV64GCArrayPackedData(m)
	gcArrayReference := admitRV64GCArrayReference(m)
	gcArrayInitElem := admitRV64GCArrayInitElem(m)
	gcArrayInitData := admitRV64GCArrayInitData(m)
	gcArrayFill := admitRV64GCArrayFill(m)
	gcArrayCopy := admitRV64GCArrayCopy(m)
	i31Core := admitRV64I31Core(m) || gcRefEq || gcRefTestTable || gcRefTestConcrete || gcRefTestAbstract || gcStructGet || gcStructMutation || gcStructNull || gcStructPacked || gcStructBasic || gcStructNumericGlobals || gcArrayLocal || gcArrayUniform || gcArrayFixedLocal || gcArrayFixedOfficial || gcArrayPackedData || gcArrayReference || gcArrayInitElem || gcArrayInitData || gcArrayFill || gcArrayCopy
	i31RefEq := i31Core && (admitRV64I31RefEqPilot(m) || gcRefEq)
	s.importReferences = production && importedTables
	if production && publicExports && importedTables {
		var err error
		s.localMutableReferences, err = admitLocalMutableFunctionGlobals(m)
		if err != nil {
			return s, err
		}
	}
	s.indexedMemories = production && publicExports
	var tableErr error
	s.ownedTable, tableErr = admitTableProfile(m, production, importedTables)
	s.elementCount = len(m.Elements)
	if s.ownedTable {
		s.tableAddr64 = make([]bool, m.TableCount())
		s.tableCompactRef = make([]bool, m.TableCount())
		for index := range s.tableAddr64 {
			t, _ := m.TableType(uint32(index))
			s.tableAddr64[index] = t.Limits.Addr64
			elem := wasm.RefVal(t.Ref)
			s.tableCompactRef[index] = wasm.EqualValType(elem, wasm.ExternRef) || (gcRefTestTable || gcRefTestConcrete) && wasm.EqualValType(elem, wasm.RefVal(wasm.Ref(true, wasm.AbsHeap(wasm.HeapStruct), false))) || i31Core && (wasm.EqualValType(elem, wasm.I31Ref) || isI31CoreRef(elem) || wasm.EqualValType(elem, wasm.AnyRef) || i31RefEq && wasm.EqualValType(elem, wasm.EqRef))
			if index == 0 {
				// The frontend reserves max(1024,2*min) for observable no-max growth.
				s.table64 = t.Limits.Addr64
				s.ownedTableGrowth = importedTables && m.ImportedTableCount() != 0 || t.Limits.HasMax || t.Limits.Min <= MaxOwnedTableEntries/2
			}
		}
	}
	if tableErr != nil {
		return s, tableErr
	}
	maxImports := 1
	if production {
		maxImports = MaxProductionImports
	} // 256 globals and one memory
	maxMemories := 1
	if s.indexedMemories {
		maxMemories = MaxProductionMemories
	}
	if len(m.Imports) > maxImports || len(m.Memories) > maxMemories {
		return s, unsupported("import/memory count bound")
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
			s.memoryCount++
			if s.memoryCount > maxMemories {
				return s, unsupported("multiple memories")
			}
			t := im.Type.MemType()
			s.memories = append(s.memories, t)
			if err := admitMemoryType(t, production, publicExports); err != nil {
				return s, err
			}
			if s.memory == nil {
				s.memory = &memorySpec{min: uint32(t.Limits.Min), max: t.Limits.Max, hasMax: t.Limits.HasMax, imported: true, module: im.Module, name: im.Name}
			}
		case wasm.ExternGlobal:
			if !production {
				return s, unsupported("isolated global import")
			}
			g := im.Type.GlobalType()
			reference := publicExports && importedTables && admitImportedFunctionGlobal(m, g)
			if !scalarType(g.Type, true) && !reference {
				return s, unsupported("nonscalar global import")
			}
			s.globals = append(s.globals, globalSpec{wide: reference || scalarWide(g.Type), mutable: g.Mutable, typ: g.Type})
		default:
			return s, unsupported("nonmemory/nonglobal import")
		}
	}
	for _, t := range m.Memories {
		s.memories = append(s.memories, t)
		s.memoryCount++
		if s.memoryCount > maxMemories {
			return s, unsupported("multiple memories")
		}
		if err := admitMemoryType(t, production, publicExports); err != nil {
			return s, err
		}
		if s.memory == nil {
			s.memory = &memorySpec{min: uint32(t.Limits.Min), max: t.Limits.Max, hasMax: t.Limits.HasMax}
		}
	}
	if len(s.globals)+len(m.Globals) > 256 || len(m.Data) > native.MaxDataSegments {
		return s, unsupported("state count bound")
	}
	importedGlobals := len(s.globals)
	stateOnlyExternrefs := admitStateOnlyExternrefGlobals(m, production, publicExports)
	for _, g := range m.Globals {
		if gcExtern && g.Type.Type.Kind() == wasm.ValRef {
			// The exact classifier pins both conversion initializers to null.
			s.globals = append(s.globals, globalSpec{wide: true, typ: g.Type.Type})
			continue
		}
		if (gcStructPacked || gcStructBasic || gcStructNumericGlobals || gcArrayFixedOfficial || gcArrayPackedData || gcArrayReference || gcArrayInitElem || gcArrayInitData || gcArrayFill || gcArrayCopy) && g.Type.Type.Kind() == wasm.ValRef {
			s.globals = append(s.globals, globalSpec{wide: true, mutable: g.Type.Mutable, typ: g.Type.Type})
			continue
		}
		if i31Core && (isI31CoreRef(g.Type.Type) || wasm.EqualValType(g.Type.Type, wasm.I31Ref) || wasm.EqualValType(g.Type.Type, wasm.AnyRef)) {
			v, err := i31CoreGlobalValue(g.Init)
			if err != nil {
				return s, err
			}
			s.globals = append(s.globals, globalSpec{wide: true, mutable: g.Type.Mutable, value: v, typ: g.Type.Type})
			continue
		}
		if wasm.EqualValType(g.Type.Type, wasm.ExternRef) {
			if !stateOnlyExternrefs {
				return s, unsupported("externref global requires the exact state-only null profile")
			}
			s.globals = append(s.globals, globalSpec{wide: true, typ: g.Type.Type})
			continue
		}
		if wasm.EqualValType(g.Type.Type, wasm.FuncRef) {
			if !publicExports || !importedTables {
				return s, unsupported("reference global requires retained public table")
			}
			if err := admitLocalFunctionGlobal(g, m, len(s.globals), s.localMutableReferences); err != nil {
				return s, err
			}
			s.globals = append(s.globals, globalSpec{wide: true, mutable: g.Type.Mutable, typ: g.Type.Type})
			continue
		}
		w := scalarWide(g.Type.Type)
		if !scalarType(g.Type.Type, production) {
			return s, unsupported("noninteger global")
		}
		available := s.globals[:importedGlobals]
		if publicExports {
			available = s.globals
		}
		v, err := admittedPublicScalarConst(g.Init, g.Type.Type, production, publicExports, available)
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
		if s.memory == nil || d.Mode.Kind != wasm.DataActive || uint64(d.Mode.Mem) >= uint64(s.memoryCount) || !s.indexedMemories && d.Mode.Mem != 0 {
			return s, unsupported("indexed/unsupported active data")
		}
		available := s.globals[:importedGlobals]
		if publicExports {
			available = s.globals
		}
		off, err := admittedPublicScalarConst(d.Mode.Offset, wasm.MemoryAddrType(s.memories[d.Mode.Mem]), production, publicExports, available)
		if err != nil {
			return s, err
		}
		if d.Mode.Mem == 0 && !s.memories[0].Limits.Addr64 {
			s.data = append(s.data, native.MemoryInitializer{Offset: uint32(off), Bytes: append([]byte(nil), d.Init...)})
		}
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
			if uint64(ex.Index.Index) < uint64(len(s.globals)) && wasm.EqualValType(s.globals[ex.Index.Index].typ, wasm.FuncRef) {
				if err := admitReferenceGlobalExport(m, ex.Index.Index); err != nil {
					return s, err
				}
			}
			s.globalExports[ex.Name] = int(ex.Index.Index)
		case wasm.ExternTable:
			if !production || !publicExports || !s.ownedTable || uint64(ex.Index.Index) >= uint64(len(s.tableAddr64)) {
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
		fn := functionSpec{params: make([]bool, len(ft.Params)), results: make([]bool, len(ft.Results)), signature: ft}
		if (s.ownedTable || s.importReferences) && !((gcTypeSubtypingDeclarations || gcRefEq || gcRefTestTable || gcRefTestConcrete || gcRefTestAbstract || gcStructGet || gcStructMutation || gcStructNull || gcStructPacked || gcStructBasic || gcStructNumericGlobals || gcArrayLocal || gcArrayUniform || gcArrayFixedLocal || gcArrayFixedOfficial || gcArrayPackedData || gcArrayReference || gcArrayInitElem || gcArrayInitData || gcArrayFill || gcArrayCopy) && ft.Kind != wasm.CompFunc) {
			var ok bool
			fn.key, ok = m.StructuralTypeKeyChecked(uint32(i))
			if !ok {
				return s, unsupported("bounded table signature identity")
			}
		}
		for k, t := range ft.Params {
			fn.params[k] = scalarWide(t) || i31Core && (isI31CoreRef(t) || (gcRefTestAbstract || gcBrOnCastNullability || gcStructPacked || gcStructBasic || gcArrayFixedOfficial || gcArrayPackedData || gcArrayReference || gcArrayInitElem || gcArrayInitData || gcArrayFill || gcArrayCopy) && t.Kind() == wasm.ValRef)
		}
		for k, t := range ft.Results {
			fn.results[k] = scalarWide(t) || i31Core && (isI31CoreRef(t) || (gcRefTestAbstract || gcBrOnCastNullability || gcStructPacked || gcStructBasic || gcArrayFixedOfficial || gcArrayPackedData || gcArrayReference || gcArrayInitElem || gcArrayInitData || gcArrayFill || gcArrayCopy) && t.Kind() == wasm.ValRef)
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
	f.memoryAccessAt(0, offset, size, wide, signed, store)
}
func (f *integerEmitter) memoryAccessAt(index uint32, offset uint64, size int, wide, signed, store bool) {
	f.memoryAccessTypedAt(index, offset, size, wide, signed, store, false)
}
func (f *integerEmitter) memoryAccessTypedAt(index uint32, offset uint64, size int, wide, signed, store, address64 bool) {
	if size != 1 && size != 2 && size != 4 && size != 8 {
		f.err = fmt.Errorf("rv64: invalid scalar memory footprint")
		return
	}
	f.swarMemoryAddressTypedAt(index, offset, uint8(size), address64)
	if f.err != nil || f.a.Err() != nil {
		return
	}
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
func (f *integerEmitter) memorySizeGrowTypedAt(index uint32, grow, address64 bool) {
	if f.err != nil || f.a.Err() != nil {
		return
	}
	if address64 && !f.mode.production {
		f.err = unsupported("private memory64 growth")
		return
	}
	if f.mode.production {
		f.productionMemorySizeGrowTypedAt(index, grow, address64)
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
