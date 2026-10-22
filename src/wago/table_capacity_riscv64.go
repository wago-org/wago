//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"fmt"
	"slices"
	"unsafe"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
	"github.com/wago-org/wago/src/core/compiler/frontend"
)

type rv64TableCapacities struct{ values []uint32 }

func (c *Compiled) rv64ExecutionTableCapacities() []uint32 {
	if indexes := c.loadCompileIndexes(); indexes != nil && indexes.railshotTail.tableCapacities != nil {
		return indexes.railshotTail.tableCapacities.values
	}
	return nil
}

func (c *Compiled) setRV64ExecutionTableCapacities(values []uint32) {
	if memo := c.loadValidateMemo(); memo != nil && memo.executionView() != nil {
		return
	}
	if len(values) == 0 {
		c.ensureCompileIndexes().railshotTail.tableCapacities = nil
		return
	}
	c.ensureCompileIndexes().railshotTail.tableCapacities = &rv64TableCapacities{values: slices.Clone(values)}
}

func railshotStoreTableCapacities(c *Compiled, shapes []frontend.TableRuntimeShape) error {
	if len(shapes) != c.tableCount() || len(shapes) > railshot.MaxFunctions {
		return fmt.Errorf("RV64 table reservation directory count")
	}
	different := false
	for index, shape := range shapes {
		if shape.Capacity < 0 || uint64(shape.Capacity) > uint64(^uint32(0)) {
			return fmt.Errorf("RV64 table %d reservation exceeds native storage representation", index)
		}
		if _, imported := c.tableImportAt(index); !imported {
			definition := c.tableDef(index)
			different = different || shape.Capacity != c.tableRuntimeCapacity(index) || definition.HasMax && definition.Max > uint64(maxInt()) && !c.stagedInertOversizedTable64(index)
		}
	}
	if !different {
		return nil
	}
	values := make([]uint32, len(shapes))
	for index, shape := range shapes {
		if _, imported := c.tableImportAt(index); !imported {
			values[index] = uint32(shape.Capacity)
		}
	}
	// values is freshly owned; avoid a second source allocation.
	c.ensureCompileIndexes().railshotTail.tableCapacities = &rv64TableCapacities{values: values}
	return railshotValidateTableCapacities(c)
}

func railshotValidateTableCapacities(c *Compiled) error {
	values := c.rv64ExecutionTableCapacities()
	if len(values) == 0 {
		return nil
	}
	if len(values) != c.tableCount() || len(values) > railshot.MaxFunctions {
		return fmt.Errorf("RV64 table reservation directory count")
	}
	for index, value := range values {
		if _, imported := c.tableImportAt(index); imported {
			if value != 0 {
				return fmt.Errorf("RV64 imported table %d cannot own a reservation", index)
			}
			continue
		}
		definition := c.tableDef(index)
		if definition.Size < 0 || uint64(value) < uint64(definition.Size) || definition.HasMax && uint64(value) > definition.Max {
			return fmt.Errorf("RV64 table %d reservation differs from valid storage limits", index)
		}
	}
	return nil
}

func railshotTableReservedCapacity(c *Compiled, index int) (int, bool) {
	values := c.rv64ExecutionTableCapacities()
	if index >= 0 && index < len(values) {
		return int(values[index]), true
	}
	return 0, false
}

func railshotTableCapacitySnapshotBytes(c *Compiled) uintptr {
	if c.loadCompileIndexes() == nil {
		return 0
	}
	bytes := unsafe.Sizeof((*rv64TableCapacities)(nil))
	if values := c.rv64ExecutionTableCapacities(); len(values) != 0 {
		bytes += unsafe.Sizeof(rv64TableCapacities{}) + uintptr(len(values))*unsafe.Sizeof(uint32(0))
	}
	return bytes
}
