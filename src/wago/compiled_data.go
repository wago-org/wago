package wago

import (
	"crypto/sha256"
	"encoding/binary"

	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

const rv64RootedMaxHostFunctionImports = 256
const rv64RootedMaxMemories = 4

// compactActiveData is an immutable execution-only representation. Public
// DataInit metadata remains unchanged and independently mutable. Common constant
// offsets need neither a global index nor an expression slice in every record.
type compactActiveData struct {
	records []compactDataRecord
	bytes   []byte
}

type compactDataRecord struct {
	memoryIndex uint32
	offset      uint32
	start       uint32
	end         uint32
}

const minCompactActiveData = 1024

func (c *Compiled) activeDataCount() int {
	if c.compactData != nil {
		return len(c.compactData.records)
	}
	return len(c.Data)
}

func (c *Compiled) activeDataAt(index int) DataInit {
	if c.compactData == nil {
		return c.Data[index]
	}
	d := c.compactData.records[index]
	return DataInit{MemoryIndex: d.memoryIndex, Offset: OffsetInit{Base: d.offset}, Bytes: c.compactData.bytes[d.start:d.end:d.end]}
}

// rv64RootedImportDigest authenticates the bounded numeric host boundary used
// by freshly compiled generic rooted-GC modules. The exact module/name split is
// part of the receipt: concatenated import keys alone cannot distinguish names
// containing dots. Imported signatures remain reference-free until collector
// ownership can cross the public host ABI.
func rv64RootedImportDigest(c *Compiled) ([32]byte, bool) {
	if c == nil || c.NumImports < 0 || c.NumImports > rv64RootedMaxHostFunctionImports || len(c.Imports) != c.NumImports || len(c.importFuncSigs) != c.NumImports ||
		c.dynamicImports != (c.NumImports != 0) || len(c.FuncTypeID) != c.NumImports+len(c.Funcs) {
		return [32]byte{}, false
	}
	functionEnds, _, _, _, exact := c.importModuleEndSections()
	if !exact || len(functionEnds) != c.NumImports {
		return [32]byte{}, false
	}
	h := sha256.New()
	var word [8]byte
	binary.LittleEndian.PutUint32(word[:4], uint32(c.NumImports))
	_, _ = h.Write(word[:4])
	remaining := uint64(1 << 20)
	for i := range c.Imports {
		key, moduleEnd := c.Imports[i], functionEnds[i]
		if moduleEnd == 0 || validateImportModuleEnd(key, moduleEnd) != nil || uint64(len(key)) > remaining {
			return [32]byte{}, false
		}
		remaining -= uint64(len(key))
		binary.LittleEndian.PutUint64(word[:], uint64(len(key)))
		_, _ = h.Write(word[:])
		_, _ = h.Write([]byte(key))
		binary.LittleEndian.PutUint64(word[:], moduleEnd)
		_, _ = h.Write(word[:])

		sig := &c.importFuncSigs[i]
		if !sig.HasTypeIndex || sig.unsafeCrossTail || uint64(sig.TypeIndex) >= uint64(len(c.Types)) || len(sig.Params) > coreruntime.MaxHostArity || len(sig.Results) > coreruntime.MaxHostArity {
			return [32]byte{}, false
		}
		typ := &c.Types[sig.TypeIndex]
		if typ.Kind != CompositeTypeFunction || len(sig.Params) != len(typ.Params) || len(sig.Results) != len(typ.Results) {
			return [32]byte{}, false
		}
		binary.LittleEndian.PutUint32(word[:4], sig.TypeIndex)
		_, _ = h.Write(word[:4])
		binary.LittleEndian.PutUint64(word[:], c.FuncTypeID[i])
		_, _ = h.Write(word[:])
		for pair, values := range [][]ValType{sig.Params, sig.Results} {
			binary.LittleEndian.PutUint32(word[:4], uint32(len(values)))
			_, _ = h.Write(word[:4])
			exactValues := typ.Params
			if pair != 0 {
				exactValues = typ.Results
			}
			for j, value := range values {
				if value != ValI32 && value != ValI64 && value != ValF32 && value != ValF64 {
					return [32]byte{}, false
				}
				abi, ok := exactValues[j].ABIType(c.Types)
				if !ok || abi != value {
					return [32]byte{}, false
				}
				_, _ = h.Write([]byte{byte(value)})
			}
		}
	}
	var digest [32]byte
	copy(digest[:], h.Sum(nil))
	return digest, true
}

// rv64RootedActiveDataDigest authenticates the exact compiler-produced data
// image used by the fresh generic rooted-GC profile. Active segments admit
// direct indexed-memory literal offsets; passive slots preserve original data-index
// order so memory.init/data.drop address the authenticated per-instance state.
func rv64RootedActiveDataDigest(c *Compiled) ([32]byte, bool) {
	if c == nil || c.compactData != nil || len(c.Data) > 256 || len(c.PassiveData) > 256 {
		return [32]byte{}, false
	}
	h := sha256.New()
	var word [8]byte
	remaining := uint64(1 << 20)
	binary.LittleEndian.PutUint32(word[:4], uint32(len(c.Data)))
	_, _ = h.Write(word[:4])
	for i := range c.Data {
		data := &c.Data[i]
		if uint64(data.MemoryIndex) >= uint64(c.memoryCount()) || data.Offset.HasGlobal || data.Offset.Global != 0 || len(data.Offset.Expr) != 0 || uint64(len(data.Bytes)) > remaining {
			return [32]byte{}, false
		}
		remaining -= uint64(len(data.Bytes))
		binary.LittleEndian.PutUint32(word[:4], data.MemoryIndex)
		_, _ = h.Write(word[:4])
		binary.LittleEndian.PutUint32(word[:4], data.Offset.Base)
		_, _ = h.Write(word[:4])
		binary.LittleEndian.PutUint64(word[:], uint64(len(data.Bytes)))
		_, _ = h.Write(word[:])
		_, _ = h.Write(data.Bytes)
	}
	binary.LittleEndian.PutUint32(word[:4], uint32(len(c.PassiveData)))
	_, _ = h.Write(word[:4])
	for i := range c.PassiveData {
		data := &c.PassiveData[i]
		if uint64(len(data.Bytes)) > remaining {
			return [32]byte{}, false
		}
		remaining -= uint64(len(data.Bytes))
		binary.LittleEndian.PutUint64(word[:], uint64(len(data.Bytes)))
		_, _ = h.Write(word[:])
		_, _ = h.Write(data.Bytes)
	}
	var digest [32]byte
	copy(digest[:], h.Sum(nil))
	return digest, true
}

// rv64RootedMemoryExportDigest authenticates the bounded memory directory and
// exact public namespace used by the fresh generic rooted-GC profile. Names are
// sorted so the receipt is independent of Go map iteration order.
func rv64RootedMemoryExportDigest(c *Compiled) ([32]byte, bool) {
	if c == nil || c.memoryDir == nil || len(c.memoryDir.defs) > rv64RootedMaxMemories || !c.memoryDir.exactExports || len(c.memoryDir.exports) > 1024 || len(c.Exports)+len(c.memoryDir.exports) > 1024 {
		return [32]byte{}, false
	}
	h := sha256.New()
	var word [8]byte
	_, _, memoryEnds, _, exact := c.importModuleEndSections()
	if !exact {
		return [32]byte{}, false
	}
	remaining := uint64(1 << 20)
	imports := 0
	binary.LittleEndian.PutUint32(word[:4], uint32(len(c.memoryDir.defs)))
	_, _ = h.Write(word[:4])
	for i := range c.memoryDir.defs {
		def := &c.memoryDir.defs[i]
		if def.Shared || def.Addr64 || !def.HasMax || def.Min > def.Max || def.Max > 256 {
			return [32]byte{}, false
		}
		flags := byte(1)
		if def.ImportKey != "" {
			if imports != i || imports >= len(memoryEnds) || validateImportModuleEnd(def.ImportKey, memoryEnds[imports]) != nil || uint64(len(def.ImportKey)) > remaining {
				return [32]byte{}, false
			}
			remaining -= uint64(len(def.ImportKey))
			flags |= 2
			imports++
		}
		_, _ = h.Write([]byte{flags})
		binary.LittleEndian.PutUint64(word[:], def.Min)
		_, _ = h.Write(word[:])
		binary.LittleEndian.PutUint64(word[:], def.Max)
		_, _ = h.Write(word[:])
		binary.LittleEndian.PutUint64(word[:], uint64(len(def.ImportKey)))
		_, _ = h.Write(word[:])
		_, _ = h.Write([]byte(def.ImportKey))
		if def.ImportKey != "" {
			binary.LittleEndian.PutUint64(word[:], memoryEnds[imports-1])
			_, _ = h.Write(word[:])
		}
	}
	if imports != len(memoryEnds) {
		return [32]byte{}, false
	}
	binary.LittleEndian.PutUint32(word[:4], uint32(len(c.memoryDir.exports)))
	_, _ = h.Write(word[:4])
	for _, name := range sortedKeys(c.memoryDir.exports) {
		index := c.memoryDir.exports[name]
		if index < 0 || index >= len(c.memoryDir.defs) || uint64(len(name)) > remaining {
			return [32]byte{}, false
		}
		remaining -= uint64(len(name))
		binary.LittleEndian.PutUint64(word[:], uint64(len(name)))
		_, _ = h.Write(word[:])
		_, _ = h.Write([]byte(name))
		binary.LittleEndian.PutUint32(word[:4], uint32(index))
		_, _ = h.Write(word[:4])
	}
	var digest [32]byte
	copy(digest[:], h.Sum(nil))
	return digest, true
}

// rv64RootedGlobalDigest authenticates numeric imported cells, literal local
// scalar cells, nullable local collector-reference cells, and their exact public
// namespace. Generic rooted code addresses globals by index, so ownership,
// exact type, width, mutability, and initial bits must remain compiler-owned
// facts.
func rv64RootedGlobalDigest(c *Compiled) ([32]byte, bool) {
	if c == nil || len(c.GlobalImports) > len(c.Globals) || len(c.Globals) > 256 || len(c.GlobalExports) > 1024 ||
		len(c.Exports)+len(c.GlobalExports)+len(c.memoryExportMap()) > 1024 {
		return [32]byte{}, false
	}
	h := sha256.New()
	var word [8]byte
	binary.LittleEndian.PutUint32(word[:4], uint32(len(c.GlobalImports)))
	_, _ = h.Write(word[:4])
	binary.LittleEndian.PutUint32(word[:4], uint32(len(c.Globals)))
	_, _ = h.Write(word[:4])
	remaining := uint64(1 << 20)
	for i := range c.Globals {
		global := &c.Globals[i]
		collectorRef := isGCRefValType(global.Type)
		if global.Type != ValI32 && global.Type != ValI64 && global.Type != ValF32 && global.Type != ValF64 && !collectorRef ||
			!global.HasValueType || global.HasInitFunc || global.InitFunc != 0 ||
			global.V128 != (V128{}) || (global.Type == ValI32 || global.Type == ValF32) && global.Bits>>32 != 0 ||
			collectorRef && (i < len(c.GlobalImports) || global.Bits != 0 || global.HasInitGlobal || global.InitGlobal != 0 || len(global.InitExpr) != 0) {
			return [32]byte{}, false
		}
		if len(global.InitExpr) != 0 {
			if i < len(c.GlobalImports) || global.HasInitGlobal || global.InitGlobal != 0 ||
				(global.Type != ValI32 && global.Type != ValI64) || len(global.InitExpr) > 128 || uint64(len(global.InitExpr)) > remaining {
				return [32]byte{}, false
			}
			remaining -= uint64(len(global.InitExpr))
		} else if global.HasInitGlobal {
			if i < len(c.GlobalImports) || global.InitGlobal < 0 || global.InitGlobal >= i || global.Bits != 0 {
				return [32]byte{}, false
			}
			parent := &c.Globals[global.InitGlobal]
			if parent.Mutable || parent.Type != global.Type {
				return [32]byte{}, false
			}
		} else if global.InitGlobal != 0 {
			return [32]byte{}, false
		}
		exact, err := c.globalExactType(i)
		plain, ok := valueTypeDescriptorFromValType(global.Type)
		if err != nil || !ok || (!collectorRef && exact != plain) || collectorRef && exact.Kind != ValueTypeReference {
			return [32]byte{}, false
		}
		flags := byte(1)
		if global.Mutable {
			flags |= 2
		}
		if global.HasInitGlobal {
			flags |= 8
		}
		if len(global.InitExpr) != 0 {
			flags |= 16
		}
		if i < len(c.GlobalImports) {
			imp := &c.GlobalImports[i]
			if imp.Type != global.Type || imp.Mutable != global.Mutable || !imp.HasValueType ||
				imp.ValueTypeIndex != global.ValueTypeIndex || global.Bits != 0 || uint64(len(imp.Module))+uint64(len(imp.Name)) > remaining {
				return [32]byte{}, false
			}
			remaining -= uint64(len(imp.Module)) + uint64(len(imp.Name))
			flags |= 4
			binary.LittleEndian.PutUint64(word[:], uint64(len(imp.Module)))
			_, _ = h.Write(word[:])
			_, _ = h.Write([]byte(imp.Module))
			binary.LittleEndian.PutUint64(word[:], uint64(len(imp.Name)))
			_, _ = h.Write(word[:])
			_, _ = h.Write([]byte(imp.Name))
		}
		_, _ = h.Write([]byte{byte(global.Type), flags})
		binary.LittleEndian.PutUint32(word[:4], global.ValueTypeIndex)
		_, _ = h.Write(word[:4])
		if collectorRef {
			refFlags := byte(0)
			if exact.Ref.Nullable {
				refFlags |= 1
			}
			if exact.Ref.Exact {
				refFlags |= 2
			}
			if exact.Ref.Heap.Defined {
				refFlags |= 4
			}
			_, _ = h.Write([]byte{refFlags, byte(exact.Ref.Heap.Abstract)})
			binary.LittleEndian.PutUint32(word[:4], exact.Ref.Heap.TypeIndex)
			_, _ = h.Write(word[:4])
		}
		binary.LittleEndian.PutUint32(word[:4], uint32(global.InitGlobal))
		_, _ = h.Write(word[:4])
		binary.LittleEndian.PutUint64(word[:], uint64(len(global.InitExpr)))
		_, _ = h.Write(word[:])
		_, _ = h.Write(global.InitExpr)
		binary.LittleEndian.PutUint64(word[:], global.Bits)
		_, _ = h.Write(word[:])
	}
	binary.LittleEndian.PutUint32(word[:4], uint32(len(c.GlobalExports)))
	_, _ = h.Write(word[:4])
	for _, name := range sortedKeys(c.GlobalExports) {
		index := c.GlobalExports[name]
		if index < 0 || index >= len(c.Globals) || uint64(len(name)) > remaining {
			return [32]byte{}, false
		}
		remaining -= uint64(len(name))
		binary.LittleEndian.PutUint64(word[:], uint64(len(name)))
		_, _ = h.Write(word[:])
		_, _ = h.Write([]byte(name))
		binary.LittleEndian.PutUint32(word[:4], uint32(index))
		_, _ = h.Write(word[:4])
	}
	var digest [32]byte
	copy(digest[:], h.Sum(nil))
	return digest, true
}

// rv64RootedTableDigest authenticates the bounded table directory for generic
// rooted-GC code, including exact imported identities and the public table
// namespace. Callable rows remain retained canonical descriptors; general
// reference constant expressions remain a separate ownership boundary.
func rv64RootedTableDigest(c *Compiled) ([32]byte, bool) {
	if c == nil || len(c.tableExports) > 256 || len(c.Elems) > 256 || len(c.passiveElems) > 256 {
		return [32]byte{}, false
	}
	h := sha256.New()
	var word [8]byte
	remainingNames := uint64(1 << 20)
	if c.tableCount() == 0 {
		if c.HasTable || len(c.extraTables) != 0 || c.TableSize != 0 || c.TableMax != 0 || c.TableHasMax || c.TableAddr64 || c.TableType != 0 || c.TableHasValueType || c.TableValueTypeIndex != 0 ||
			c.HasTableInitFunc || c.TableInitFunc != 0 || len(c.Elems) != 0 || len(c.passiveElems) != 0 {
			return [32]byte{}, false
		}
		_, _ = h.Write([]byte{0})
		var digest [32]byte
		copy(digest[:], h.Sum(nil))
		return digest, true
	}
	if c.tableCount() < 1 || c.tableCount() > 2 || !c.HasTable || !c.hasTableExportMetadata || len(c.FuncTypeID) != c.NumImports+len(c.Funcs) {
		return [32]byte{}, false
	}
	importCount := c.tableImportCount()
	_, tableModuleEnds, _, _, exactImportNames := c.importModuleEndSections()
	if !exactImportNames || len(tableModuleEnds) != importCount {
		return [32]byte{}, false
	}
	publicTables := make([]bool, c.tableCount())
	for i := 0; i < importCount; i++ {
		publicTables[i] = true
	}
	for _, index := range c.tableExports {
		if index < 0 || index >= c.tableCount() {
			return [32]byte{}, false
		}
		publicTables[index] = true
	}
	hasPrivateTable := false
	for _, public := range publicTables {
		hasPrivateTable = hasPrivateTable || !public
	}
	needsDescriptors := false
	plain, _ := valueTypeDescriptorFromValType(ValFuncRef)
	rootedAggregate := func(exact ValueTypeDescriptor) bool {
		if exact.Kind != ValueTypeReference || !exact.Ref.Nullable || exact.Ref.Exact || !exact.Ref.Heap.Defined ||
			exact.Ref.Heap.Abstract != 0 || uint64(exact.Ref.Heap.TypeIndex) >= uint64(len(c.Types)) {
			return false
		}
		kind := c.Types[exact.Ref.Heap.TypeIndex].Kind
		return kind == CompositeTypeStruct || kind == CompositeTypeArray
	}
	writeElemType := func(elem *ElemInit) bool {
		if elem.RefType != 0 && elem.RefType != ValFuncRef {
			return false
		}
		if !elem.HasValueType {
			if elem.ValueTypeIndex != 0 {
				return false
			}
			_, _ = h.Write([]byte{0})
			return true
		}
		if uint64(elem.ValueTypeIndex) >= uint64(len(c.ValueTypes)) || c.ValueTypes[elem.ValueTypeIndex] != plain {
			return false
		}
		_, _ = h.Write([]byte{1})
		binary.LittleEndian.PutUint32(word[:4], elem.ValueTypeIndex)
		_, _ = h.Write(word[:4])
		return true
	}
	writeValues := func(values []RefInit, allowImported bool) bool {
		for _, value := range values {
			if value.HasGlobal || value.GlobalIndex != 0 || value.I31Wrap || value.RepeatPrevious || len(value.Expr) != 0 {
				return false
			}
			if value.Null {
				if value.FuncIndex != 0 {
					return false
				}
				_, _ = h.Write([]byte{0})
				continue
			}
			if (!allowImported && uint64(value.FuncIndex) < uint64(c.NumImports)) || uint64(value.FuncIndex) >= uint64(c.NumImports+len(c.Funcs)) {
				return false
			}
			needsDescriptors = true
			_, _ = h.Write([]byte{1})
			binary.LittleEndian.PutUint32(word[:4], value.FuncIndex)
			_, _ = h.Write(word[:4])
		}
		return true
	}
	_, _ = h.Write([]byte{byte(c.tableCount())})
	for tableIndex := 0; tableIndex < c.tableCount(); tableIndex++ {
		def := c.tableDef(tableIndex)
		exact, err := c.tableExactType(tableIndex)
		functionTable := exact == plain
		aggregateTable := rootedAggregate(exact)
		if err != nil || !functionTable && !aggregateTable || def.Addr64 || !def.HasValueType ||
			uint64(def.ValueTypeIndex) >= uint64(len(c.ValueTypes)) || c.ValueTypes[def.ValueTypeIndex] != exact ||
			functionTable && def.Type != ValFuncRef || aggregateTable && (def.Type != ValAnyRef || publicTables[tableIndex]) {
			return [32]byte{}, false
		}
		if imported, ok := c.tableImportAt(tableIndex); ok {
			if !functionTable || tableIndex >= importCount || imported.Key == "" || !imported.HasMax || imported.Addr64 || imported.Type != ValFuncRef || !imported.HasValueType ||
				imported.Min > imported.Max || imported.Max > 256 || imported.ValueTypeIndex != def.ValueTypeIndex || tableModuleEnds[tableIndex] == 0 ||
				validateImportModuleEnd(imported.Key, tableModuleEnds[tableIndex]) != nil || uint64(len(imported.Key)) > remainingNames || def.HasInitFunc || def.InitFunc != 0 {
				return [32]byte{}, false
			}
			remainingNames -= uint64(len(imported.Key))
			_, _ = h.Write([]byte{1})
			binary.LittleEndian.PutUint64(word[:], uint64(len(imported.Key)))
			_, _ = h.Write(word[:])
			_, _ = h.Write([]byte(imported.Key))
			binary.LittleEndian.PutUint64(word[:], tableModuleEnds[tableIndex])
			_, _ = h.Write(word[:])
			binary.LittleEndian.PutUint64(word[:], imported.Min)
			_, _ = h.Write(word[:])
			binary.LittleEndian.PutUint64(word[:], imported.Max)
			_, _ = h.Write(word[:])
			binary.LittleEndian.PutUint32(word[:4], imported.ValueTypeIndex)
			_, _ = h.Write(word[:4])
			continue
		}
		if tableIndex < importCount || def.ImportKey != "" || def.ImportHasMax || !def.HasMax || def.Size < 0 || def.Size > 256 || def.Max < uint64(def.Size) || def.Max > 256 ||
			aggregateTable && (def.HasInitFunc || def.InitFunc != 0) ||
			!def.HasInitFunc && def.InitFunc != 0 || def.HasInitFunc && ((!publicTables[tableIndex] && uint64(def.InitFunc) < uint64(c.NumImports)) || uint64(def.InitFunc) >= uint64(c.NumImports+len(c.Funcs)) || railshotNonCallableFunction(c, int(def.InitFunc))) {
			return [32]byte{}, false
		}
		_, _ = h.Write([]byte{0})
		needsDescriptors = needsDescriptors || def.HasInitFunc
		binary.LittleEndian.PutUint64(word[:], uint64(def.Size))
		_, _ = h.Write(word[:])
		binary.LittleEndian.PutUint64(word[:], def.Max)
		_, _ = h.Write(word[:])
		binary.LittleEndian.PutUint32(word[:4], def.ValueTypeIndex)
		_, _ = h.Write(word[:4])
		if aggregateTable {
			refFlags := byte(0)
			if exact.Ref.Nullable {
				refFlags |= 1
			}
			if exact.Ref.Exact {
				refFlags |= 2
			}
			if exact.Ref.Heap.Defined {
				refFlags |= 4
			}
			_, _ = h.Write([]byte{refFlags, byte(exact.Ref.Heap.Abstract)})
			binary.LittleEndian.PutUint32(word[:4], exact.Ref.Heap.TypeIndex)
			_, _ = h.Write(word[:4])
		}
		if def.HasInitFunc {
			_, _ = h.Write([]byte{1})
			binary.LittleEndian.PutUint32(word[:4], def.InitFunc)
			_, _ = h.Write(word[:4])
		} else {
			_, _ = h.Write([]byte{0})
		}
	}
	binary.LittleEndian.PutUint32(word[:4], uint32(len(c.tableExports)))
	_, _ = h.Write(word[:4])
	for _, name := range sortedKeys(c.tableExports) {
		index := c.tableExports[name]
		if index < 0 || index >= c.tableCount() || uint64(len(name)) > remainingNames {
			return [32]byte{}, false
		}
		remainingNames -= uint64(len(name))
		binary.LittleEndian.PutUint64(word[:], uint64(len(name)))
		_, _ = h.Write(word[:])
		_, _ = h.Write([]byte(name))
		binary.LittleEndian.PutUint32(word[:4], uint32(index))
		_, _ = h.Write(word[:4])
	}
	binary.LittleEndian.PutUint32(word[:4], uint32(len(c.Elems)))
	_, _ = h.Write(word[:4])
	remaining := 256
	for i := range c.Elems {
		elem := &c.Elems[i]
		if elem.Mode != ElemModeActive || uint64(elem.TableIndex) >= uint64(c.tableCount()) ||
			elem.Offset.HasGlobal || elem.Offset.Global != 0 || len(elem.Offset.Expr) != 0 || len(elem.Values) > remaining {
			return [32]byte{}, false
		}
		exact, err := c.tableExactType(int(elem.TableIndex))
		if err != nil || exact != plain {
			return [32]byte{}, false
		}
		remaining -= len(elem.Values)
		binary.LittleEndian.PutUint32(word[:4], elem.TableIndex)
		_, _ = h.Write(word[:4])
		if !writeElemType(elem) {
			return [32]byte{}, false
		}
		binary.LittleEndian.PutUint32(word[:4], elem.Offset.Base)
		_, _ = h.Write(word[:4])
		binary.LittleEndian.PutUint32(word[:4], uint32(len(elem.Values)))
		_, _ = h.Write(word[:4])
		if !writeValues(elem.Values, publicTables[elem.TableIndex]) {
			return [32]byte{}, false
		}
	}
	binary.LittleEndian.PutUint32(word[:4], uint32(len(c.passiveElems)))
	_, _ = h.Write(word[:4])
	for i := range c.passiveElems {
		elem := &c.passiveElems[i]
		if elem.Mode == ElemModeActive && uint64(elem.TableIndex) >= uint64(c.tableCount()) || elem.Mode != ElemModeActive && elem.TableIndex != 0 ||
			elem.Offset.Base != 0 || elem.Offset.HasGlobal || elem.Offset.Global != 0 || len(elem.Offset.Expr) != 0 ||
			elem.Mode != ElemModeActive && elem.Mode != ElemModePassive && elem.Mode != ElemModeDeclarative || len(elem.Values) > remaining {
			return [32]byte{}, false
		}
		if elem.Mode != ElemModePassive && len(elem.Values) != 0 {
			return [32]byte{}, false
		}
		remaining -= len(elem.Values)
		binary.LittleEndian.PutUint32(word[:4], elem.TableIndex)
		_, _ = h.Write(word[:4])
		if !writeElemType(elem) {
			return [32]byte{}, false
		}
		_, _ = h.Write([]byte{byte(elem.Mode)})
		binary.LittleEndian.PutUint32(word[:4], uint32(len(elem.Values)))
		_, _ = h.Write(word[:4])
		allowImported := !hasPrivateTable
		if elem.Mode == ElemModeActive {
			allowImported = publicTables[elem.TableIndex]
		}
		if !writeValues(elem.Values, allowImported) {
			return [32]byte{}, false
		}
	}
	if needsDescriptors && !c.needsFuncRefDescs() {
		return [32]byte{}, false
	}
	var digest [32]byte
	copy(digest[:], h.Sum(nil))
	return digest, true
}

// compactActiveDataSize also guards the uint32 byte-range representation. Rare
// offset forms and nil-vs-empty metadata retain the ordinary exact deep copy.
func compactActiveDataSize(c *Compiled) (int, bool) {
	if c.compactData != nil {
		return len(c.compactData.bytes), true
	}
	if len(c.Data) < minCompactActiveData {
		return 0, false
	}
	var size uint64
	for _, d := range c.Data {
		if d.Offset.HasGlobal || d.Offset.Global != 0 || d.Offset.Expr != nil || d.Bytes == nil {
			return 0, false
		}
		size += uint64(len(d.Bytes))
		if size > uint64(^uint32(0)) || size > uint64(maxInt()) {
			return 0, false
		}
	}
	return int(size), true
}

func cloneCompactActiveData(out *compactActiveData, c *Compiled, payloadBytes int) {
	*out = compactActiveData{records: make([]compactDataRecord, c.activeDataCount()), bytes: make([]byte, payloadBytes)}
	next := 0
	for i := range out.records {
		d := c.activeDataAt(i)
		end := next + len(d.Bytes)
		copy(out.bytes[next:end], d.Bytes)
		out.records[i] = compactDataRecord{memoryIndex: d.MemoryIndex, offset: d.Offset.Base, start: uint32(next), end: uint32(end)}
		next = end
	}
}
