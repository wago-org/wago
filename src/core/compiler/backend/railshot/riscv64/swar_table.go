//go:build linux && !tinygo

package riscv64

import "github.com/wago-org/wago/src/core/compiler/wasm"

func swarOwnedTableType(table *wasm.TableType) error {
	if table == nil {
		return nil
	}
	if table.Limits.Addr64 || table.Limits.Min > MaxOwnedTableEntries ||
		(table.Limits.HasMax && (table.Limits.Max < table.Limits.Min || table.Limits.Max > MaxOwnedTableEntries)) ||
		!wasm.EqualValType(wasm.RefVal(table.Ref), wasm.FuncRef) {
		return unsupported("SWAR private nullable funcref table32 declaration/bound")
	}
	return nil
}
