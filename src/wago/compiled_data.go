package wago

import (
	"crypto/sha256"
	"encoding/binary"
)

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

// rv64RootedActiveDataDigest authenticates the exact compiler-produced data
// image used by the fresh generic rooted-GC profile. Active segments admit only
// direct memory-0 literal offsets; passive slots preserve original data-index
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
		if data.MemoryIndex != 0 || data.Offset.HasGlobal || data.Offset.Global != 0 || len(data.Offset.Expr) != 0 || uint64(len(data.Bytes)) > remaining {
			return [32]byte{}, false
		}
		remaining -= uint64(len(data.Bytes))
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

// rv64RootedMemoryExportDigest authenticates the exact public memory namespace
// used by the fresh generic rooted-GC profile. Names are sorted so the receipt
// is independent of Go map iteration order; this profile exposes only memory 0.
func rv64RootedMemoryExportDigest(c *Compiled) ([32]byte, bool) {
	if c == nil || c.memoryDir == nil || !c.memoryDir.exactExports || len(c.memoryDir.exports) > 1024 || len(c.Exports)+len(c.memoryDir.exports) > 1024 {
		return [32]byte{}, false
	}
	h := sha256.New()
	var word [8]byte
	binary.LittleEndian.PutUint32(word[:4], uint32(len(c.memoryDir.exports)))
	_, _ = h.Write(word[:4])
	remaining := uint64(1 << 20)
	for _, name := range sortedKeys(c.memoryDir.exports) {
		index := c.memoryDir.exports[name]
		if index != 0 || len(c.memoryDir.defs) != 1 || uint64(len(name)) > remaining {
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

// rv64RootedGlobalDigest authenticates literal local scalar cells and their
// exact public namespace. Generic rooted code addresses globals by index, so
// type, width, mutability and initial bits must remain compiler-owned facts.
func rv64RootedGlobalDigest(c *Compiled) ([32]byte, bool) {
	if c == nil || len(c.GlobalImports) != 0 || len(c.Globals) > 256 || len(c.GlobalExports) > 1024 ||
		len(c.Exports)+len(c.GlobalExports)+len(c.memoryExportMap()) > 1024 {
		return [32]byte{}, false
	}
	h := sha256.New()
	var word [8]byte
	binary.LittleEndian.PutUint32(word[:4], uint32(len(c.Globals)))
	_, _ = h.Write(word[:4])
	for i := range c.Globals {
		global := &c.Globals[i]
		if global.Type != ValI32 && global.Type != ValI64 && global.Type != ValF32 && global.Type != ValF64 ||
			!global.HasValueType || global.HasInitGlobal || global.InitGlobal != 0 || global.HasInitFunc || global.InitFunc != 0 ||
			len(global.InitExpr) != 0 || global.V128 != (V128{}) || (global.Type == ValI32 || global.Type == ValF32) && global.Bits>>32 != 0 {
			return [32]byte{}, false
		}
		exact, err := c.globalExactType(i)
		plain, ok := valueTypeDescriptorFromValType(global.Type)
		if err != nil || !ok || exact != plain {
			return [32]byte{}, false
		}
		flags := byte(1)
		if global.Mutable {
			flags |= 2
		}
		_, _ = h.Write([]byte{byte(global.Type), flags})
		binary.LittleEndian.PutUint32(word[:4], global.ValueTypeIndex)
		_, _ = h.Write(word[:4])
		binary.LittleEndian.PutUint64(word[:], global.Bits)
		_, _ = h.Write(word[:])
	}
	binary.LittleEndian.PutUint32(word[:4], uint32(len(c.GlobalExports)))
	_, _ = h.Write(word[:4])
	remaining := uint64(1 << 20)
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

// rv64RootedTableDigest authenticates the first bounded private table boundary
// for generic rooted-GC code. Only one local table32 and exact nullable
// funcref literals are admitted here; public tables and general constant
// expressions remain separate ownership boundaries.
func rv64RootedTableDigest(c *Compiled) ([32]byte, bool) {
	if c == nil || len(c.extraTables) != 0 || c.tableImport != "" || len(c.tableExports) != 0 || len(c.Elems) > 256 || len(c.passiveElems) > 256 {
		return [32]byte{}, false
	}
	h := sha256.New()
	var word [8]byte
	if c.tableCount() == 0 {
		if c.HasTable || c.TableSize != 0 || c.TableMax != 0 || c.TableHasMax || c.TableAddr64 || c.TableType != 0 || c.TableHasValueType || c.TableValueTypeIndex != 0 ||
			c.HasTableInitFunc || c.TableInitFunc != 0 || len(c.Elems) != 0 || len(c.passiveElems) != 0 {
			return [32]byte{}, false
		}
		_, _ = h.Write([]byte{0})
		var digest [32]byte
		copy(digest[:], h.Sum(nil))
		return digest, true
	}
	if c.tableCount() != 1 || !c.HasTable || !c.hasTableExportMetadata || c.TableAddr64 || c.TableType != ValFuncRef || !c.TableHasValueType || !c.TableHasMax ||
		c.TableSize < 0 || c.TableSize > 256 || c.TableMax < uint64(c.TableSize) || c.TableMax > 256 ||
		uint64(c.TableValueTypeIndex) >= uint64(len(c.ValueTypes)) || len(c.FuncTypeID) != len(c.Funcs) {
		return [32]byte{}, false
	}
	needsDescriptors := false
	if !c.HasTableInitFunc && c.TableInitFunc != 0 || c.HasTableInitFunc && (uint64(c.TableInitFunc) >= uint64(len(c.Funcs)) || railshotNonCallableFunction(c, int(c.TableInitFunc))) {
		return [32]byte{}, false
	}
	needsDescriptors = c.HasTableInitFunc
	plain, _ := valueTypeDescriptorFromValType(ValFuncRef)
	exact, err := c.tableExactType(0)
	if err != nil || exact != plain || c.ValueTypes[c.TableValueTypeIndex] != plain {
		return [32]byte{}, false
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
	writeValues := func(values []RefInit) bool {
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
			if uint64(value.FuncIndex) >= uint64(len(c.Funcs)) {
				return false
			}
			needsDescriptors = true
			_, _ = h.Write([]byte{1})
			binary.LittleEndian.PutUint32(word[:4], value.FuncIndex)
			_, _ = h.Write(word[:4])
		}
		return true
	}
	_, _ = h.Write([]byte{1})
	binary.LittleEndian.PutUint64(word[:], uint64(c.TableSize))
	_, _ = h.Write(word[:])
	binary.LittleEndian.PutUint64(word[:], c.TableMax)
	_, _ = h.Write(word[:])
	binary.LittleEndian.PutUint32(word[:4], c.TableValueTypeIndex)
	_, _ = h.Write(word[:4])
	if c.HasTableInitFunc {
		_, _ = h.Write([]byte{1})
		binary.LittleEndian.PutUint32(word[:4], c.TableInitFunc)
		_, _ = h.Write(word[:4])
	} else {
		_, _ = h.Write([]byte{0})
	}
	binary.LittleEndian.PutUint32(word[:4], uint32(len(c.Elems)))
	_, _ = h.Write(word[:4])
	remaining := 256
	for i := range c.Elems {
		elem := &c.Elems[i]
		if elem.Mode != ElemModeActive || elem.TableIndex != 0 ||
			elem.Offset.HasGlobal || elem.Offset.Global != 0 || len(elem.Offset.Expr) != 0 || len(elem.Values) > remaining {
			return [32]byte{}, false
		}
		remaining -= len(elem.Values)
		if !writeElemType(elem) {
			return [32]byte{}, false
		}
		binary.LittleEndian.PutUint32(word[:4], elem.Offset.Base)
		_, _ = h.Write(word[:4])
		binary.LittleEndian.PutUint32(word[:4], uint32(len(elem.Values)))
		_, _ = h.Write(word[:4])
		if !writeValues(elem.Values) {
			return [32]byte{}, false
		}
	}
	binary.LittleEndian.PutUint32(word[:4], uint32(len(c.passiveElems)))
	_, _ = h.Write(word[:4])
	for i := range c.passiveElems {
		elem := &c.passiveElems[i]
		if elem.TableIndex != 0 ||
			elem.Offset.Base != 0 || elem.Offset.HasGlobal || elem.Offset.Global != 0 || len(elem.Offset.Expr) != 0 ||
			elem.Mode != ElemModeActive && elem.Mode != ElemModePassive && elem.Mode != ElemModeDeclarative || len(elem.Values) > remaining {
			return [32]byte{}, false
		}
		if elem.Mode != ElemModePassive && len(elem.Values) != 0 {
			return [32]byte{}, false
		}
		remaining -= len(elem.Values)
		if !writeElemType(elem) {
			return [32]byte{}, false
		}
		_, _ = h.Write([]byte{byte(elem.Mode)})
		binary.LittleEndian.PutUint32(word[:4], uint32(len(elem.Values)))
		_, _ = h.Write(word[:4])
		if !writeValues(elem.Values) {
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
