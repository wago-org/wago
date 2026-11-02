//go:build !(linux || (darwin && arm64)) || riscv64 || tinygo || wago_target_tinygo

package wago

import (
	"encoding/binary"
	"fmt"
	"math/bits"
)

func compiledConversionStoreHooks(*Compiled) bool { return false }

// Opaque identities require publisher-independent receipts. Targets without
// that adapter accept only null and authenticated ordinary externrefs. RV64
// uses its separate compact publication protocol, not host opaque identities.
func unsupportedConversionWord(store *referenceStore, word uint64) error {
	if word == 0 {
		return nil
	}
	if store != nil {
		store.mu.Lock()
		defer store.mu.Unlock()
		raw := bits.RotateLeft64(word, -17) ^ store.externKey
		index, generation := uint32(raw), uint32(raw>>32)
		if store.externKey != 0 && index != 0 && uint64(index) <= uint64(len(store.externrefs)) {
			slot := &store.externrefs[index-1]
			// Compact converted public tokens also occupy this directory.
			// Inspect their ownership wrapper before public-value unwrapping.
			_, converted := slot.value.(interface{ externrefPublicValue() any })
			if slot.generation == generation && slot.value != releasedExternrefValue && slot.nextFree != externrefInternalSlot && !converted {
				return nil
			}
		}
	}
	return fmt.Errorf("opaque converted publication is unsupported on this target")
}
func (in *Instance) publishConversionStoreInvocation(_ gcConversionInvocation, _ *Instance, _ []byte, _, word uint64, publish func() error) error {
	if err := unsupportedConversionWord(in.refStore, word); err != nil {
		return err
	}
	return publish()
}
func conversionImportedTableCount(s *gcRefTestTableState) int {
	if s != nil && s.Conversion != nil && s.Conversion.core != nil {
		return s.Conversion.core.owner.c.tableImportCount()
	}
	return 0
}
func conversionBoundaryWord(*Instance, uint64, bool) (uint64, bool) { return 0, false }
func setConversionGlobalPublic(access *Instance, g *Global, value Value) (bool, error) {
	if compiledArchitecture == 4 || g == nil || g.Type != ValExternRef || value.typ != ValExternRef {
		return false, nil
	}
	var store *referenceStore
	if g.owner != nil {
		store = g.owner.store
	} else if access != nil {
		store = access.refStore
	}
	// Staged conversions have their own opaque public-word namespace. They are
	// deliberately absent from the ordinary externref directory; letting that
	// miss fall through would let the legacy global setter publish them without
	// a physical-container receipt. The two public APIs resolve conversions via
	// the access instance and the physical owner respectively.
	var physical *Instance
	if g.owner != nil {
		physical = g.owner.instance
	}
	for _, owner := range [2]*Instance{access, physical} {
		if value.bits == 0 || owner == nil {
			continue
		}
		if conversion := owner.existingGCExternConversionState(); conversion != nil {
			conversion.mu.Lock()
			converted := false
			if !conversion.closed {
				for _, entry := range conversion.entries {
					if entry.kind == gcExternConversionData && entry.publicExternWord == value.bits {
						converted = true
						break
					}
				}
			}
			conversion.mu.Unlock()
			if converted {
				return true, fmt.Errorf("opaque converted publication is unsupported on this target")
			}
		}
	}
	// Public ingress authenticates tokens before classifying their payload.
	// Leave invalid and cross-store words to the ordinary global validation,
	// which preserves its invalid-token error and never publishes them.
	if value.bits != 0 {
		if store == nil {
			return false, nil
		}
		if _, ok := store.resolveExternref(value.bits); !ok {
			return false, nil
		}
	}
	if err := unsupportedConversionWord(store, value.bits); err != nil {
		return true, err
	}
	return false, nil
}
func (in *Instance) publishConversionRangeInvocation(_ gcConversionInvocation, _ *Instance, _ []byte, _, _ uint64, words []uint64, publish func()) error {
	for _, word := range words {
		if err := unsupportedConversionWord(in.refStore, word); err != nil {
			return err
		}
	}
	publish()
	return nil
}

const conversionInitializationEnabled = compiledArchitecture != 4

func (in *Instance) initializeConversionReceipts(staged []conversionInitialTable) error {
	if compiledArchitecture == 4 {
		return nil
	} // compact RV64 owns its initialization protocol
	for i, g := range in.globalCells {
		if i >= len(in.c.GlobalImports) && in.c.Globals[i].Type == ValExternRef {
			if err := unsupportedConversionWord(in.refStore, readGlobalObject(g, ValExternRef)); err != nil {
				return err
			}
		}
	}
	if in.c != nil {
		for i := in.c.tableImportCount(); i < in.c.tableCount(); i++ {
			if in.c.tableElementType(i) != ValExternRef {
				continue
			}
			storage := in.tableDescriptor(i)
			for j := uint32(0); j < binary.LittleEndian.Uint32(storage); j++ {
				if err := unsupportedConversionWord(in.refStore, binary.LittleEndian.Uint64(storage[8+uint64(j)*8:])); err != nil {
					return err
				}
			}
		}
	}
	for _, row := range staged {
		for j, touched := range row.touched {
			if touched {
				if err := unsupportedConversionWord(in.refStore, binary.LittleEndian.Uint64(row.staged[8+j*8:])); err != nil {
					return err
				}
			}
		}
	}
	for _, row := range staged {
		for j, touched := range row.touched {
			if touched {
				copy(row.original[8+j*8:16+j*8], row.staged[8+j*8:16+j*8])
			}
		}
	}
	return nil
}
func setConversionTablePublic(t *Table, index uint64, value Value) error {
	if t == nil || t.owner == nil || value.typ != ValExternRef {
		return fmt.Errorf("invalid extern table value")
	}
	owner := t.instanceOwner()
	if owner != nil {
		if err := owner.beginInvocation(); err != nil {
			return err
		}
		defer owner.endInvocation()
	}
	if owner != nil {
		unlock := owner.lockInstanceNativeStateForHostAccess()
		defer unlock()
	} else {
		nativeExecutionMu.Lock()
		defer nativeExecutionMu.Unlock()
	}
	t.owner.mu.Lock()
	defer t.owner.mu.Unlock()
	if t.owner.closed || t.owner.elementType != ValExternRef || len(t.desc) < 8 || index >= uint64(binary.LittleEndian.Uint32(t.desc)) || index >= uint64((len(t.desc)-8)/8) {
		return fmt.Errorf("extern table metadata or index is invalid")
	}
	if value.bits == 0 && t.owner.hasValueType && !t.owner.valueType.Ref.Nullable {
		return fmt.Errorf("table requires non-null reference")
	}
	if err := unsupportedConversionWord(t.owner.store, value.bits); err != nil {
		return err
	}
	if err := unsupportedConversionWord(t.owner.store, binary.LittleEndian.Uint64(t.desc[8+index*8:])); err != nil {
		return err
	}
	binary.LittleEndian.PutUint64(t.desc[8+index*8:], value.bits)
	return nil
}
