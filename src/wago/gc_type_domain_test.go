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
