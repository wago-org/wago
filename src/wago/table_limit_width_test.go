package wago

import "testing"

func TestDeclaredTableLimitMetadataIsUint64(t *testing.T) {
	const large = uint64(1)<<63 + 17
	spec := ImportSpec{Kind: ImportTable, Min: large - 1, Max: large, HasMax: true, Addr64: true}
	if spec.Min != large-1 || spec.Max != large {
		t.Fatalf("ImportSpec limits = %d..%d, want %d..%d", spec.Min, spec.Max, large-1, large)
	}
	def := tableImportDef{Min: large - 1, Max: large, HasMax: true, Addr64: true}
	if def.Min != spec.Min || def.Max != spec.Max {
		t.Fatalf("internal exact limits = %d..%d, want %d..%d", def.Min, def.Max, spec.Min, spec.Max)
	}
}
