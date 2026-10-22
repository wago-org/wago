//go:build linux && !tinygo

package riscv64

import "github.com/wago-org/wago/src/core/compiler/wasm"

const (
	branchHintGroupBytes = 32
	branchHintEntryBytes = 8
)

// admitPublicBranchHints reconciles raw and decoded advisory metadata before
// lowering. Hints deliberately do not select code layout in the initial RV64
// profile, but hand-built modules still receive the decoder's exact validation
// and share the aggregate decoded-metadata budget.
func admitPublicBranchHints(m *wasm.Module, public bool, remaining *int) error {
	customs := 0
	var raw []byte
	for i := range m.Customs {
		if m.Customs[i].Name == "metadata.code.branch_hint" {
			customs++
			raw = m.Customs[i].Data
		}
	}
	if !public {
		if m.BranchHints != nil || customs != 0 {
			return unsupported("branch hints require public production")
		}
		return nil
	}
	if customs > 1 || len(m.BranchHints) > MaxFunctions {
		return unsupported("branch hint section/group bound")
	}
	consume := func(count, width int) bool {
		if count < 0 || width < 0 || width != 0 && count > *remaining/width {
			return false
		}
		*remaining -= count * width
		return true
	}
	if !consume(len(m.BranchHints), branchHintGroupBytes) {
		return unsupported("branch hint metadata byte bound")
	}
	total := 0
	for i := range m.BranchHints {
		group := &m.BranchHints[i]
		if i != 0 && group.FuncIndex <= m.BranchHints[i-1].FuncIndex || len(group.Hints) > MaxModuleBytes/branchHintEntryBytes-total {
			return unsupported("branch hint ordering/count bound")
		}
		total += len(group.Hints)
		if !consume(len(group.Hints), branchHintEntryBytes) {
			return unsupported("branch hint metadata byte bound")
		}
		for j := 1; j < len(group.Hints); j++ {
			if group.Hints[j].Offset <= group.Hints[j-1].Offset {
				return unsupported("branch hint ordering/count bound")
			}
		}
	}
	if customs == 1 {
		limit := uint64(max(*remaining, 1))
		decoded, err := wasm.DecodeBranchHintMetadata(raw, wasm.DecodeLimits{MaxMetadataBytes: limit})
		if err != nil || !equalBranchHints(decoded, m.BranchHints) {
			return unsupported("branch hint raw/decoded metadata")
		}
	}
	if err := wasm.ValidateBranchHints(m); err != nil {
		return unsupported("branch hint target")
	}
	return nil
}

func equalBranchHints(a, b []wasm.FuncBranchHints) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].FuncIndex != b[i].FuncIndex || len(a[i].Hints) != len(b[i].Hints) {
			return false
		}
		for j := range a[i].Hints {
			if a[i].Hints[j] != b[i].Hints[j] {
				return false
			}
		}
	}
	return true
}
