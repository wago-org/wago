package wago

import (
	"encoding/binary"
	"fmt"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"unsafe"
)

// These helpers also run in providers compiled before a conversion domain exists.
// Their operands remain opaque words; they never pass through compact-ref casts.
const gcConversionTableSet uint32 = 53
const gcConversionGlobalSet uint32 = 54

// Direct test/construction dispatch has no authenticated native activation.
func (in *Instance) dispatchConversionStore(helper uint32, args, results []uint64) bool {
	return in.dispatchConversionStoreInvocation(nil, 0, helper, args, results)
}

type gcConversionInvocation struct {
	root *Instance
	id   invocationID
}

func (in *Instance) dispatchConversionStoreInvocation(root *Instance, id invocationID, helper uint32, args, results []uint64) bool {
	authority := gcConversionInvocation{root: root, id: id}
	if helper >= 55 && helper <= 58 {
		in.dispatchConversionTableBulk(authority, helper, args, results)
		return true
	}
	if helper != gcConversionTableSet && helper != gcConversionGlobalSet {
		return false
	}
	var owner *Instance
	var storage []byte
	var index, word uint64
	if helper == gcConversionTableSet {
		if len(args) != 3 {
			panic(gcHelperFailuref("extern table set arity"))
		}
		table := args[2]
		index, word = args[0], args[1]
		storage, owner = in.conversionTableStorage(table)
		if len(storage) < 8 || index >= uint64(binary.LittleEndian.Uint32(storage)) {
			panic(gcHelperTrap(coreruntime.TrapTableOutOfBounds))
		}
	} else {
		if len(args) != 2 || args[1] >= uint64(len(in.globalCells)) {
			panic(gcHelperFailuref("extern global set arity/index"))
		}
		i := int(args[1])
		word = args[0]
		if in.c.Globals[i].Type != ValExternRef || !in.c.Globals[i].Mutable {
			panic(gcHelperFailuref("extern global metadata"))
		}
		g := in.globalCells[i]
		if g == nil || len(g.cell) < 8 {
			panic(gcHelperFailuref("extern global storage unavailable"))
		}
		storage, owner = g.cell, in
		if i < len(in.c.GlobalImports) {
			if g.owner == nil {
				panic(gcHelperFailuref("extern global owner unavailable"))
			}
			// A nil Instance names a real host cell, never the publishing instance.
			owner = g.instanceOwner()
		}
	}
	publish := func() error {
		offset := uint64(0)
		if helper == gcConversionTableSet {
			offset = 8 + index*8
		}
		if offset+8 > uint64(len(storage)) {
			return fmt.Errorf("extern storage extent")
		}
		binary.LittleEndian.PutUint64(storage[offset:], word)
		return nil
	}
	if err := in.publishConversionStoreInvocation(authority, owner, storage, index, word, publish); err != nil {
		panic(gcHelperFailure(err))
	}
	return true
}

func (in *Instance) conversionTableStorage(table uint64) ([]byte, *Instance) {
	if table >= uint64(in.c.tableCount()) || in.c.tableElementType(int(table)) != ValExternRef {
		panic(gcHelperFailuref("extern table metadata"))
	}
	owner := in
	var desc []byte
	if table < uint64(in.c.tableImportCount()) {
		t, ok := in.imports.table(in.c.tableImportBindingKey(int(table)))
		if !ok || t == nil || t.owner == nil {
			panic(gcHelperFailuref("extern table import"))
		}
		// Keep physical storage and identity together for both single and bulk
		// stores. The adapter authenticates host authority in the current store.
		desc, owner = t.desc, t.instanceOwner()
	} else {
		desc = in.tableDescriptor(int(table))
	}
	if len(desc) < 8 {
		panic(gcHelperFailuref("extern table descriptor"))
	}
	length, capacity := uint64(binary.LittleEndian.Uint32(desc)), uint64(binary.LittleEndian.Uint32(desc[4:]))
	if length > capacity || capacity > uint64((len(desc)-8)/8) {
		panic(gcHelperFailuref("extern table descriptor extent"))
	}
	return desc, owner
}
func (in *Instance) dispatchConversionTableBulk(authority gcConversionInvocation, helper uint32, args, results []uint64) {
	arity := 4
	if helper == 56 || helper == 58 {
		arity = 5
	}
	if helper == 57 {
		arity = 3
	}
	if len(args) != arity {
		panic(gcHelperFailuref("extern bulk arity"))
	}
	ti := args[3%arity]
	if helper == 57 {
		ti = args[2]
	}
	desc, owner := in.conversionTableStorage(ti)
	length, capacity := uint64(binary.LittleEndian.Uint32(desc)), uint64(binary.LittleEndian.Uint32(desc[4:]))
	norm := func(word uint64, table uint64) uint64 {
		if !in.c.tableDef(int(table)).Addr64 {
			return uint64(uint32(word))
		}
		return word
	}
	start, count := norm(args[0], ti), norm(args[2], ti)
	var source []byte
	var sourceStart uint64
	var fill uint64
	if helper == 55 {
		fill = args[1]
	}
	if helper == 56 {
		source, _ = in.conversionTableStorage(args[4])
		sourceStart = norm(args[1], args[4])
		if !in.c.tableDef(int(args[4])).Addr64 {
			count = uint64(uint32(count))
		}
		sl := uint64(binary.LittleEndian.Uint32(source))
		if sourceStart > sl || count > sl-sourceStart {
			panic(gcHelperTrap(coreruntime.TrapTableOutOfBounds))
		}
		source = source[8:]
	}
	if helper == 58 {
		sourceStart, count = uint64(uint32(args[1])), uint64(uint32(args[2]))
		segment, _ := in.gcRootedTableElementDescriptor(uint32(args[4]))
		sl := uint64(binary.LittleEndian.Uint32(segment[8:]))
		if sourceStart > sl || count > sl-sourceStart {
			panic(gcHelperTrap(coreruntime.TrapTableOutOfBounds))
		}
		if sl != 0 {
			source = unsafe.Slice((*byte)(offHeapPtr(uintptr(binary.LittleEndian.Uint64(segment)))), int(sl)*8)
		}
	}
	limit := length
	if helper == 57 {
		if len(results) != 1 {
			panic(gcHelperFailuref("extern grow result arity"))
		}
		fill, start, count = args[0], length, norm(args[1], ti)
		if count > capacity-length {
			results[0] = ^uint64(0)
			if !in.c.tableDef(int(ti)).Addr64 {
				results[0] = uint64(^uint32(0))
			}
			return
		}
		limit = length + count
	}
	if start > limit || count > limit-start {
		panic(gcHelperTrap(coreruntime.TrapTableOutOfBounds))
	}
	// Snapshot the complete source first, giving memmove semantics for aliases.
	words := make([]uint64, int(count))
	for i := range words {
		words[i] = fill
		if source != nil {
			words[i] = binary.LittleEndian.Uint64(source[(sourceStart+uint64(i))*8:])
		}
	}
	publish := func() {
		for i, word := range words {
			binary.LittleEndian.PutUint64(desc[8+(start+uint64(i))*8:], word)
		}
		if helper == 57 {
			binary.LittleEndian.PutUint32(desc, uint32(limit))
			results[0] = length
		}
	}
	if err := in.publishConversionRangeInvocation(authority, owner, desc, start, limit, words, publish); err != nil {
		panic(gcHelperFailure(err))
	}
}

func compiledExternStoreHelperSlots(c *Compiled) int {
	if c == nil || compiledArchitecture == 4 || len(c.Funcs) == 0 {
		return 0
	}
	for i := 0; i < c.tableCount(); i++ {
		if c.tableElementType(i) == ValExternRef {
			return 5
		}
	}
	for _, g := range c.Globals {
		if g.Type == ValExternRef && g.Mutable {
			return 2
		}
	}
	return 0
}

type conversionInitialTable struct {
	original, staged []byte
	touched          []bool
	owner            *Instance
}

func stageConversionInitialTable(rows *[]conversionInitialTable, t *Table, start uint64, count int) []byte {
	for _, row := range *rows {
		if &row.original[0] == &t.desc[0] {
			for j := 0; j < count; j++ {
				row.touched[int(start)+j] = true
			}
			return row.staged
		}
	}
	row := conversionInitialTable{original: t.desc, staged: append([]byte(nil), t.desc...), owner: t.instanceOwner(), touched: make([]bool, binary.LittleEndian.Uint32(t.desc))}
	for j := 0; j < count; j++ {
		row.touched[int(start)+j] = true
	}
	*rows = append(*rows, row)
	return row.staged
}

// SetValue replaces one externref entry. A null ValueExternRef clears its exact
// physical receipt. The adapter resolves the canonical module or host authority.
func (t *Table) SetValue(index uint64, value Value) error {
	return setConversionTablePublic(t, index, value)
}

// Unauthenticated adapters remain available for unsupported-platform controls.
func (in *Instance) publishConversionStore(owner *Instance, storage []byte, index, word uint64, publish func() error) error {
	return in.publishConversionStoreInvocation(gcConversionInvocation{}, owner, storage, index, word, publish)
}
func (in *Instance) publishConversionRange(owner *Instance, storage []byte, start, limit uint64, words []uint64, publish func()) error {
	return in.publishConversionRangeInvocation(gcConversionInvocation{}, owner, storage, start, limit, words, publish)
}
