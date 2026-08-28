package wago

import (
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/runtime/gc"
)

func TestGCTypeMappingRemapsCanonicalTypesToRuntimeDomain(t *testing.T) {
	mapping := &gcTypeMapping{
		localToDomain: []gc.TypeID{0, 1, 1, 2, 2, 3, 4, 5, 6},
		domainToLocal: []uint32{0, 1, 3, 5, 6, 7, 8},
	}
	got, err := mapping.canonicalTypes([]gc.TypeID{0, 1, 1, 3, 3, 5, 6, 7, 8})
	if err != nil {
		t.Fatal(err)
	}
	if want := "[0 1 2 3 4 5 6]"; fmt.Sprint(got) != want {
		t.Fatalf("canonical Runtime-domain types = %v, want %s", got, want)
	}
}

func TestGCTypeMappingRejectsConflictingCanonicalTypes(t *testing.T) {
	mapping := &gcTypeMapping{
		localToDomain: []gc.TypeID{0, 1, 1},
		domainToLocal: []uint32{0, 1},
	}
	if _, err := mapping.canonicalTypes([]gc.TypeID{0, 0, 1}); err == nil {
		t.Fatal("conflicting canonical representatives succeeded")
	}
}

func TestPreferredGCCollectorIgnoresReferenceFreeFunctionImports(t *testing.T) {
	store := &referenceStore{}
	collector := new(gc.Collector)
	producer := &Instance{gc: collector, refStore: store}
	imports := Imports{"env.call": &InstanceExport{inst: producer}}

	scalar := &Compiled{
		Imports:        []string{"env.call"},
		importFuncSigs: []FuncSig{{Results: []ValType{ValI64, ValI64, ValF32, ValF32, ValV128, ValI32}}},
	}
	got, err := preferredGCCollectorFromImports(scalar, imports, store)
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Fatalf("scalar-only import selected collector %p; want an independent domain", got)
	}

	reference := &Compiled{
		Imports:        []string{"env.call"},
		importFuncSigs: []FuncSig{{Results: []ValType{ValAnyRef}}},
	}
	got, err = preferredGCCollectorFromImports(reference, imports, store)
	if err != nil {
		t.Fatal(err)
	}
	if got != collector {
		t.Fatalf("collector-reference import selected collector %p; want %p", got, collector)
	}
}
