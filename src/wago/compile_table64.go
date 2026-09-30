package wago

import (
	"fmt"
	goruntime "runtime"

	"github.com/wago-org/wago/src/core/compiler/frontend"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// Keep table64 admission separate from ordinary compilation. This cold path
// carries many shape checks and error exits that otherwise enlarge its frame.
//
//go:noinline
func validateCompileTable64(m *wasm.Module) error {
	if !supportsCompleteCore3Backend(goruntime.GOOS, goruntime.GOARCH) {
		return fmt.Errorf("compile: unsupported table table64 staged execution on %s/%s", goruntime.GOOS, goruntime.GOARCH)
	}
	twoLocal := m.TableCount() == 2 && m.ImportedTableCount() == 0 && len(m.Tables) == 2
	twoLocalDeclaration := stagedTwoLocalNoMaxTable64DeclarationShape(m)
	importedLocalDeclaration := stagedImportedLocalNoMaxTable64DeclarationShape(m)
	threeLocalTableInit64 := m.TableCount() == 3 && m.ImportedTableCount() == 0 && len(m.Tables) == 3
	soleExternrefGrow := m.TableCount() == 1 && stagedSoleExternrefGrowShape(m)
	fourLocalExternrefSizeGrow := m.TableCount() == 4 && stagedFourLocalExternrefSizeGrowShape(m)
	if m.TableCount() != 1 && !twoLocal && !importedLocalDeclaration && !threeLocalTableInit64 && !fourLocalExternrefSizeGrow {
		return fmt.Errorf("compile: staged table64 requires exactly one local/imported table or an exact bounded multi-table slice")
	}
	if m.TableCount() == 1 && m.ImportedTableCount() != 0 && len(m.Tables) != 0 {
		return fmt.Errorf("compile: staged table64 rejects mixed imported/local table shapes")
	}
	if twoLocal && !twoLocalDeclaration {
		if err := stagedTwoLocalTableShape(m); err != nil {
			return fmt.Errorf("compile: staged table64 %w", err)
		}
	}
	if threeLocalTableInit64 {
		if err := stagedThreeLocalTableInit64Shape(m); err != nil {
			return fmt.Errorf("compile: staged table64 %w", err)
		}
	}
	if soleExternrefGrow || fourLocalExternrefSizeGrow {
		for i := range m.Tables {
			if m.Tables[i].Init != nil {
				return fmt.Errorf("compile: staged table64 table %d initializer expression is outside the exact local externref size/grow slice", i)
			}
		}
		if len(m.Elements) != 0 {
			return fmt.Errorf("compile: staged table64 element segments are outside the exact local externref size/grow slice")
		}
		allowed := func(k wasm.InstrKind) bool {
			if fourLocalExternrefSizeGrow {
				return k == wasm.InstrTableSize || k == wasm.InstrTableGrow
			}
			return k == wasm.InstrTableGet || k == wasm.InstrTableSet || k == wasm.InstrTableSize || k == wasm.InstrTableGrow
		}
		if err := stagedExactTableOperationShape(m, "exact local externref size/grow slice", allowed); err != nil {
			return fmt.Errorf("compile: staged table64 %w", err)
		}
	}
	externrefLocal := (twoLocal && (stagedTwoLocalExternrefReadWriteShape(m) || stagedTwoLocalExternrefFillShape(m))) || soleExternrefGrow || fourLocalExternrefSizeGrow
	inertOversized := stagedInertOversizedTable64Shape(m)
	for tableIndex := 0; tableIndex < m.TableCount(); tableIndex++ {
		tt, ok := m.TableType(uint32(tableIndex))
		if !ok {
			return fmt.Errorf("compile: staged table64 table %d type is unavailable", tableIndex)
		}
		if !wasm.EqualValType(wasm.RefVal(tt.Ref), wasm.FuncRef) && !(externrefLocal && wasm.EqualValType(wasm.RefVal(tt.Ref), wasm.ExternRef)) {
			return fmt.Errorf("compile: staged table64 requires funcref table %d outside an exact local externref slice", tableIndex)
		}
		if tt.Limits.Min > frontend.StagedTable64Max() || (tt.Limits.HasMax && tt.Limits.Max > frontend.StagedTable64Max() && !inertOversized) {
			return fmt.Errorf("compile: staged table64 table %d requires an executable runtime bound no greater than %d entries", tableIndex, frontend.StagedTable64Max())
		}
	}
	for i := range m.Elements {
		e := &m.Elements[i]
		if e.Mode.Kind == wasm.ElemActive && (int(e.Mode.Table) < 0 || int(e.Mode.Table) >= m.TableCount()) {
			return fmt.Errorf("compile: staged table64 active element segment targets unavailable table %d", e.Mode.Table)
		}
		if !twoLocal && !importedLocalDeclaration && !threeLocalTableInit64 && e.Mode.Kind == wasm.ElemActive && e.Mode.Table != 0 {
			return fmt.Errorf("compile: staged table64 active element segment targets table %d, want the sole table 0", e.Mode.Table)
		}
		if !twoLocal && !importedLocalDeclaration && !threeLocalTableInit64 && m.ImportedTableCount() != 0 && e.Mode.Kind != wasm.ElemActive {
			return fmt.Errorf("compile: imported table64 passive/declarative lifecycle remains outside the sole-local staged boundary")
		}
	}
	return nil
}
