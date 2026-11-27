// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package wago

import (
	"errors"
	"fmt"
)

func (c *Compiled) hasTableDeclarationMetadata() bool {
	return c.HasTable || c.TableSize != 0 || c.TableMax != 0 || c.TableHasMax ||
		c.TableAddr64 || c.stagedTable64 || c.HasTableInitFunc || c.tableImport != "" ||
		c.tableImportMin != 0 || c.tableImportMax != 0 || c.tableImportHasMax ||
		len(c.extraTables) != 0 || len(c.tableExports) != 0 || len(c.Elems) != 0 ||
		len(c.rv64ExecutionTableCapacities()) != 0
}

func (c *Compiled) validateTableDeclarations(required CoreFeatures) error {
	if c.TableSize < 0 {
		return fmt.Errorf("compiled metadata invalid: negative TableSize %d", c.TableSize)
	}
	if c.TableMax != 0 && c.TableMax < uint64(c.TableSize) {
		return fmt.Errorf("compiled metadata invalid: TableMax %d < TableSize %d", c.TableMax, c.TableSize)
	}
	if c.TableAddr64 && !required.IsEnabled(CoreFeatureTable64) {
		return errors.New("compiled metadata invalid: table 0 uses 64-bit indexes without table64 feature")
	}
	if err := railshotValidateTableCapacities(c); err != nil {
		return err
	}
	_, reservedTable0 := railshotTableReservedCapacity(c, 0)
	if c.TableMax > uint64(maxInt()) && !c.stagedInertOversizedTable64(0) && !reservedTable0 {
		return fmt.Errorf("compiled metadata invalid: table 0 maximum %d overflows executable capacity", c.TableMax)
	}
	if len(c.extraTables) > 0 && !c.HasTable {
		return fmt.Errorf("compiled metadata invalid: %d extra table(s) without table 0", len(c.extraTables))
	}
	if c.HasTable && c.TableType != 0 && c.TableType != ValFuncRef && c.TableType != ValExternRef && c.TableType != ValExnRef && c.TableType != ValI31Ref && c.TableType != ValAnyRef {
		return fmt.Errorf("compiled metadata invalid: table 0 element type %s is unsupported", c.TableType)
	}
	for i, table := range c.extraTables {
		if table.Size < 0 {
			return fmt.Errorf("compiled metadata invalid: negative table %d size", i+1)
		}
		if table.Max != 0 && table.Max < uint64(table.Size) {
			return fmt.Errorf("compiled metadata invalid: table %d maximum %d < size %d", i+1, table.Max, table.Size)
		}
		if table.Type != 0 && table.Type != ValFuncRef && table.Type != ValExternRef && table.Type != ValI31Ref && table.Type != ValAnyRef {
			return fmt.Errorf("compiled metadata invalid: table %d element type %s is unsupported", i+1, table.Type)
		}
		if table.Addr64 && !required.IsEnabled(CoreFeatureTable64) {
			return fmt.Errorf("compiled metadata invalid: table %d uses 64-bit indexes without table64 feature", i+1)
		}
		_, reserved := railshotTableReservedCapacity(c, i+1)
		if table.Max > uint64(maxInt()) && !reserved {
			return fmt.Errorf("compiled metadata invalid: table %d maximum %d overflows executable capacity", i+1, table.Max)
		}
	}
	if !c.HasTable && c.TableSize != 0 {
		return fmt.Errorf("compiled metadata invalid: TableSize %d without table", c.TableSize)
	}
	if !c.HasTable && c.TableMax != 0 {
		return fmt.Errorf("compiled metadata invalid: TableMax %d without table", c.TableMax)
	}
	if !c.HasTable && c.TableAddr64 {
		return errors.New("compiled metadata invalid: table64 address form without table")
	}
	if !c.HasTable && c.tableImport != "" {
		return fmt.Errorf("compiled metadata invalid: table import %q without table", c.tableImport)
	}
	if c.tableImport == "" {
		if c.tableImportMin != 0 || c.tableImportMax != 0 || c.tableImportHasMax {
			return errors.New("compiled metadata invalid: table import limits without table import")
		}
		if c.TableHasMax && !c.HasTable {
			return errors.New("compiled metadata invalid: table maximum without table")
		}
	} else {
		if c.TableHasMax {
			return errors.New("compiled metadata invalid: local table maximum flag on imported table 0")
		}
		if c.TableSize != 0 || c.TableMax != 0 {
			return errors.New("compiled metadata invalid: local table limits present on imported table")
		}
		if c.tableImportMin < 0 || c.tableImportMax < 0 {
			return errors.New("compiled metadata invalid: negative imported table limit")
		}
		if !c.tableImportHasMax && c.tableImportMax != 0 {
			return errors.New("compiled metadata invalid: imported table max without max flag")
		}
		if c.tableImportHasMax && c.tableImportMax < c.tableImportMin {
			return fmt.Errorf("compiled metadata invalid: imported table max %d < min %d", c.tableImportMax, c.tableImportMin)
		}
	}
	seenLocalTable := false
	for i, table := range c.extraTables {
		index := i + 1
		if table.ImportKey == "" {
			seenLocalTable = true
			if table.ImportHasMax {
				return fmt.Errorf("compiled metadata invalid: table %d import max flag without import key", index)
			}
			continue
		}
		if c.tableImport == "" {
			return fmt.Errorf("compiled metadata invalid: imported table %d without imported table 0", index)
		}
		if seenLocalTable {
			return fmt.Errorf("compiled metadata invalid: imported table %d follows a local table", index)
		}
		if table.HasInitFunc {
			return fmt.Errorf("compiled metadata invalid: initializer on imported table %d", index)
		}
		if table.HasMax {
			return fmt.Errorf("compiled metadata invalid: local max flag on imported table %d", index)
		}
		if !table.ImportHasMax && table.Max != 0 {
			return fmt.Errorf("compiled metadata invalid: imported table %d max without max flag", index)
		}
		if table.ImportHasMax && table.Max < uint64(table.Size) {
			return fmt.Errorf("compiled metadata invalid: imported table %d max %d < min %d", index, table.Max, table.Size)
		}
	}
	if len(c.Elems) > 0 && !c.HasTable {
		return fmt.Errorf("compiled metadata invalid: %d element segment(s) without table", len(c.Elems))
	}
	return nil
}

func (c *Compiled) validateTableBindings(totalFuncs int) error {
	if c.HasTableInitFunc {
		if !c.HasTable {
			return errors.New("compiled metadata invalid: table initializer without table")
		}
		if c.tableImport != "" {
			return errors.New("compiled metadata invalid: table initializer on imported table")
		}
		if uint64(c.TableInitFunc) >= uint64(totalFuncs) {
			return fmt.Errorf("compiled metadata invalid: table initializer function index %d out of range", c.TableInitFunc)
		}
		actual, actualErr := c.functionRefExactType(c.TableInitFunc)
		required, requiredErr := c.tableExactType(0)
		if actualErr != nil || requiredErr != nil || !valueTypeSubtype(actual, c.Types, required, c.Types) {
			return errors.New("compiled metadata invalid: table 0 initializer function type mismatch")
		}
	}
	for i, table := range c.extraTables {
		if !table.HasInitFunc {
			continue
		}
		if uint64(table.InitFunc) >= uint64(totalFuncs) {
			return fmt.Errorf("compiled metadata invalid: table %d initializer function index %d out of range", i+1, table.InitFunc)
		}
		actual, actualErr := c.functionRefExactType(table.InitFunc)
		required, requiredErr := c.tableExactType(i + 1)
		if actualErr != nil || requiredErr != nil || !valueTypeSubtype(actual, c.Types, required, c.Types) {
			return fmt.Errorf("compiled metadata invalid: table %d initializer function type mismatch", i+1)
		}
	}
	if len(c.tableExports) != 0 && !c.hasTableExportMetadata {
		return errors.New("compiled metadata invalid: table exports without exact export metadata marker")
	}
	for name, tableIndex := range c.tableExports {
		if tableIndex < 0 || tableIndex >= c.tableCount() {
			return fmt.Errorf("compiled metadata invalid: table export %q index %d out of range", name, tableIndex)
		}
	}
	return nil
}
